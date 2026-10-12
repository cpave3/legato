package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// AgentUIImageRoute is where the web server gives out the image files agents
// showed: AgentUIImageRoute + "<task-id>/<file>". The address of an image is
// the SHA-256 of its content, so only someone who already holds the image can
// name it. That is what lets the route work without the person's token, which
// the sandboxed frame an agent's HTML is drawn in must never have.
const AgentUIImageRoute = "/agent-ui-images/"

const (
	// AgentUIMaxImageBytes bounds one image file an agent may show.
	AgentUIMaxImageBytes = 25 << 20
	// agentUIMaxImages bounds the image files of one item.
	agentUIMaxImages = 60
	// A task keeps the images it used most recently; older copies are
	// removed, so a long session cannot fill the disk.
	agentUIImagesKept = 100
)

// The kinds of image an agent may show, by the type read from a file's own
// first bytes, and the extension its copy gets. SVG is left out on purpose:
// it is a document, and one opened from Legato's own address could run
// script there. An agent writes SVG inline in its HTML instead.
var agentUIImageExtensions = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
	"image/bmp":  ".bmp",
}

var (
	agentUIImageName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	agentUIImageFile = regexp.MustCompile(`^[0-9a-f]{64}\.(png|jpg|gif|webp|bmp)$`)
	// How content refers to one of its images: legato-image:<name>.
	agentUIImageReference = regexp.MustCompile(`legato-image:([A-Za-z0-9][A-Za-z0-9_-]*)`)
)

// AgentUIImageInput names an image file an agent wants to use in what it
// shows.
type AgentUIImageInput struct {
	Name string `json:"name" jsonschema:"a name of your own for the image (letters, digits, dashes, underscores); write legato-image:<name> where its address goes"`
	Path string `json:"path" jsonschema:"the image file on disk, as an absolute path: PNG, JPEG, GIF, WebP or BMP"`
}

// AgentUIImage is an image an item uses: the name its content refers to the
// image by, and where the web server gives out Legato's copy of the file.
type AgentUIImage struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// storedAgentUIImage is how a revision's images are kept in the store.
type storedAgentUIImage struct {
	Name string `json:"name"`
	File string `json:"file"`
}

// AgentUIImageFile is Legato's copy of an image an agent showed, open for
// reading.
type AgentUIImageFile struct {
	*os.File
	// MediaType is the type read from the file's own content.
	MediaType string
	ModTime   time.Time
}

// keepImages copies the image files of one request into the task's folder
// and returns them in the store's encoding. Nothing of the request is stored
// when one of its files cannot be shown.
func (s *AgentUIService) keepImages(taskID string, images []AgentUIImageInput) (string, error) {
	if len(images) == 0 {
		return "", nil
	}
	if s.imageDir == "" {
		return "", errors.New("this Legato has no folder to keep images in, so it cannot show image files")
	}
	if !ValidPastedImageTaskID(taskID) {
		return "", fmt.Errorf("%w: task ID %q cannot name a folder of images", ErrAgentUIInvalid, taskID)
	}
	dir := filepath.Join(s.imageDir, taskID)
	// What an agent shows may be private: the folder and its files are the
	// user's only.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("the images could not be stored: %w", err)
	}
	stored := make([]storedAgentUIImage, 0, len(images))
	var added []string
	for _, image := range images {
		file, isNew, err := keepImage(dir, image.Path)
		if err != nil {
			// The copies this request made go with it. One that was there
			// before belongs to something already shown.
			for _, name := range added {
				_ = os.Remove(filepath.Join(dir, name))
			}
			return "", fmt.Errorf("%w: image %q: %s", ErrAgentUIInvalid, image.Name, err)
		}
		if isNew {
			added = append(added, file)
		}
		stored = append(stored, storedAgentUIImage{Name: image.Name, File: file})
	}
	pruneAgentUIFiles(dir, agentUIImageFile, agentUIImagesKept)
	encoded, err := json.Marshal(stored)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// keepImage copies one image file into dir under the SHA-256 of its content
// and returns the copy's name, and whether the copy is new. A file whose
// content is already there is not copied again, only marked as used now.
func keepImage(dir, path string) (name string, isNew bool, err error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", false, fmt.Errorf("%s is not a path Legato can read", path)
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		return "", false, fmt.Errorf("there is no file at %s", absolute)
	}
	if info.Size() > AgentUIMaxImageBytes {
		return "", false, fmt.Errorf("%s is larger than %d MB", absolute, AgentUIMaxImageBytes>>20)
	}
	// Bounded again: the file may have grown since it was measured.
	source, err := os.Open(absolute)
	if err != nil {
		return "", false, fmt.Errorf("%s cannot be read", absolute)
	}
	content, err := io.ReadAll(io.LimitReader(source, AgentUIMaxImageBytes+1))
	source.Close()
	if err != nil {
		return "", false, fmt.Errorf("%s cannot be read", absolute)
	}
	if len(content) > AgentUIMaxImageBytes {
		return "", false, fmt.Errorf("%s is larger than %d MB", absolute, AgentUIMaxImageBytes>>20)
	}
	name, isNew, err = keepImageContent(dir, content)
	if errors.Is(err, errNotAnImage) {
		return "", false, fmt.Errorf("%s is not an image Legato can show (PNG, JPEG, GIF, WebP or BMP)", absolute)
	}
	return name, isNew, err
}

var errNotAnImage = errors.New("not an image Legato can show")

// keepImageContent stores an image's content in dir under its SHA-256 and
// returns the copy's name, and whether the copy is new. It answers
// errNotAnImage for content that is not one of the kinds Legato shows.
func keepImageContent(dir string, content []byte) (name string, isNew bool, err error) {
	// What a file is called does not count: the content decides.
	extension, ok := agentUIImageExtensions[http.DetectContentType(content)]
	if !ok {
		return "", false, errNotAnImage
	}
	sum := sha256.Sum256(content)
	name = hex.EncodeToString(sum[:]) + extension
	kept := filepath.Join(dir, name)
	if _, err := os.Stat(kept); err == nil {
		now := time.Now()
		_ = os.Chtimes(kept, now, now)
		return name, false, nil
	}
	// Written beside its final name and moved there, so that a reader never
	// finds half a file under a name that promises its content.
	incoming, err := os.CreateTemp(dir, ".incoming-*")
	if err != nil {
		return "", false, fmt.Errorf("the image could not be stored: %s", err)
	}
	_, writeErr := incoming.Write(content)
	closeErr := incoming.Close()
	if writeErr == nil {
		writeErr = closeErr
	}
	if writeErr == nil {
		writeErr = os.Rename(incoming.Name(), kept)
	}
	if writeErr != nil {
		_ = os.Remove(incoming.Name())
		return "", false, fmt.Errorf("the image could not be stored: %s", writeErr)
	}
	return name, true, nil
}

// pruneAgentUIFiles removes the files of a task's folder used longest ago,
// past the number it keeps. Only files whose names match are counted.
func pruneAgentUIFiles(dir string, names *regexp.Regexp, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type kept struct {
		name string
		used time.Time
	}
	var files []kept
	for _, entry := range entries {
		if !names.MatchString(entry.Name()) {
			continue
		}
		if info, err := entry.Info(); err == nil && info.Mode().IsRegular() {
			files = append(files, kept{entry.Name(), info.ModTime()})
		}
	}
	sort.Slice(files, func(a, b int) bool { return files[a].used.Before(files[b].used) })
	for index := 0; index < len(files)-keep; index++ {
		_ = os.Remove(filepath.Join(dir, files[index].name))
	}
}

// agentUIOrphanGrace is how long a task's folder of images is left alone
// after it last changed. Images are copied before the item that uses them is
// stored, and another process may be between the two.
const agentUIOrphanGrace = time.Hour

// RemoveOrphanImages removes the images of every task that has nothing shown
// any more: its task was deleted, or the person removed what it showed.
// Nothing calls for it at the moment an item goes, so a server runs it when
// it starts.
func (s *AgentUIService) RemoveOrphanImages(ctx context.Context) error {
	return s.removeOrphanFiles(ctx, s.imageDir)
}

// removeOrphanFiles removes each task's folder under root when that task has
// nothing shown any more.
func (s *AgentUIService) removeOrphanFiles(ctx context.Context, root string) error {
	if root == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !entry.IsDir() || !ValidPastedImageTaskID(entry.Name()) || time.Since(info.ModTime()) < agentUIOrphanGrace {
			continue
		}
		items, err := s.store.ListAgentUIItems(ctx, entry.Name())
		if err != nil {
			return err
		}
		if len(items) == 0 {
			_ = os.RemoveAll(filepath.Join(root, entry.Name()))
		}
	}
	return nil
}

// OpenImage opens Legato's copy of an image a task's agent showed. It
// answers store.ErrNotFound for anything that is not such a copy.
func (s *AgentUIService) OpenImage(taskID, file string) (*AgentUIImageFile, error) {
	if s.imageDir == "" || !ValidPastedImageTaskID(taskID) || !agentUIImageFile.MatchString(file) {
		return nil, store.ErrNotFound
	}
	return openImageCopy(filepath.Join(s.imageDir, taskID, file))
}

// openImageCopy opens a kept copy of an image for reading. It answers
// store.ErrNotFound for anything that is not a regular file holding an image
// Legato shows.
func openImageCopy(path string) (*AgentUIImageFile, error) {
	opened, err := os.Open(path)
	if err != nil {
		return nil, store.ErrNotFound
	}
	info, err := opened.Stat()
	if err != nil || !info.Mode().IsRegular() {
		opened.Close()
		return nil, store.ErrNotFound
	}
	// Read from the content again, not from the name: only an image leaves
	// through this door, whatever came to lie in the folder.
	head := make([]byte, 512)
	n, err := io.ReadFull(opened, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		opened.Close()
		return nil, store.ErrNotFound
	}
	mediaType := http.DetectContentType(head[:n])
	if _, ok := agentUIImageExtensions[mediaType]; !ok {
		opened.Close()
		return nil, store.ErrNotFound
	}
	if _, err := opened.Seek(0, io.SeekStart); err != nil {
		opened.Close()
		return nil, store.ErrNotFound
	}
	return &AgentUIImageFile{File: opened, MediaType: mediaType, ModTime: info.ModTime()}, nil
}

// storedImages reads an item's images and says where each is served. An
// image whose copy is gone is left out: a client told of no image says so,
// and one told of an image that is not there draws a broken picture.
func (s *AgentUIService) storedImages(item *store.AgentUIItem) []AgentUIImage {
	images := agentUIImagesFromStore(item.TaskID, item.Images)
	kept := images[:0]
	for _, image := range images {
		if _, err := os.Stat(filepath.Join(s.imageDir, item.TaskID, filepath.Base(image.URL))); err == nil {
			kept = append(kept, image)
		}
	}
	return kept
}

// agentUIImagesFromStore reads a revision's images and says where each is
// served.
func agentUIImagesFromStore(taskID, encoded string) []AgentUIImage {
	if encoded == "" {
		return nil
	}
	var stored []storedAgentUIImage
	if err := json.Unmarshal([]byte(encoded), &stored); err != nil {
		return nil
	}
	images := make([]AgentUIImage, 0, len(stored))
	for _, image := range stored {
		images = append(images, AgentUIImage{Name: image.Name, URL: AgentUIImageRoute + taskID + "/" + image.File})
	}
	return images
}

// validateAgentUIImages checks the image files an agent listed for its HTML
// or Markdown, and that the content does not refer to one it did not list.
func validateAgentUIImages(content string, images []AgentUIImageInput) error {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrAgentUIInvalid, fmt.Sprintf(format, args...))
	}
	if len(images) > agentUIMaxImages {
		return invalid("at most %d images fit in one item", agentUIMaxImages)
	}
	names := make(map[string]bool, len(images))
	for i, image := range images {
		if !agentUIImageName.MatchString(image.Name) {
			return invalid("images[%d]: name %q must be 1-64 letters, digits, dashes or underscores", i, image.Name)
		}
		if names[image.Name] {
			return invalid("images[%d]: the name %q is used twice", i, image.Name)
		}
		names[image.Name] = true
		if strings.TrimSpace(image.Path) == "" {
			return invalid("image %q has no path", image.Name)
		}
	}
	// An image the content names and the request does not list would be drawn
	// as a broken picture. Said now, the agent can correct the call.
	for _, reference := range agentUIImageReference.FindAllStringSubmatch(content, -1) {
		if !names[reference[1]] {
			return invalid("the content refers to %s, but images lists no image named %q", reference[0], reference[1])
		}
	}
	return nil
}
