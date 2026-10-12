package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/planvalidate"
	"github.com/cpave3/legato/internal/engine/store"
)

// Where the web server gives out the image files that the Markdown of a plan
// or of a review shows: the route + "<folder>/<file>". As with the images of
// what agents show, the address of an image is the SHA-256 of its content,
// so the routes need no token.
const (
	PlanImageRoute   = "/plan-images/"
	ReviewImageRoute = "/review-images/"
)

// documentMaxImages bounds the image files of one Markdown document.
const documentMaxImages = 60

// DocumentImage is an image a Markdown document shows: the path the Markdown
// names it by, and where the web server gives out Legato's copy of the file.
type DocumentImage struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// documentImages keeps the image files that the Markdown of plans, or of
// reviews, names: one folder for each plan or tour under root, each file
// under the hash of its content. Unlike the images of what agents show,
// these are not removed by count: they stay as long as their plan or tour.
type documentImages struct {
	root  string
	route string
}

var documentImageFolderName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// documentImageFolder is the folder name for a plan's or a tour's images.
// An ID is made from a task ID and a name the agent chose, so one that is
// not a plain name is stood in for by a hash of it.
func documentImageFolder(ownerID string) string {
	if documentImageFolderName.MatchString(ownerID) && !strings.Contains(ownerID, "..") {
		return ownerID
	}
	sum := sha256.Sum256([]byte(ownerID))
	return "x-" + hex.EncodeToString(sum[:16])
}

// A markdownImage is an image of a Markdown document that is a file on
// disk.
type markdownImage struct {
	// Name is the destination as the Markdown wrote it.
	Name string
	// Path is the file it names.
	Path string
	Line int
}

// isRemoteImage reports whether a Markdown image's destination is an
// address a browser fetches by itself, which is no file of Legato's.
func isRemoteImage(destination string) bool {
	lower := strings.ToLower(destination)
	for _, prefix := range []string{"http://", "https://", "data:", "//"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// findMarkdownImages lists the image files a Markdown document names, each
// once, and what is wrong with the ones Legato cannot show. resolve turns a
// destination into the file it names.
func findMarkdownImages(markdown string, resolve func(destination string) (string, error)) ([]markdownImage, []planvalidate.Diagnostic) {
	var images []markdownImage
	var problems []planvalidate.Diagnostic
	seen := map[string]bool{}
	for _, found := range planvalidate.Images([]byte(markdown)) {
		destination := found.Destination
		if destination == "" || isRemoteImage(destination) || seen[destination] {
			continue
		}
		seen[destination] = true
		problem := func(format string, args ...any) {
			problems = append(problems, planvalidate.Diagnostic{Line: found.Line, Message: fmt.Sprintf("image %s: %s", destination, fmt.Sprintf(format, args...))})
		}
		if scheme, _, ok := strings.Cut(destination, ":"); ok && !strings.ContainsAny(scheme, `/\.`) {
			problem("name an image file by its path, not by a %s: address", scheme)
			continue
		}
		file, err := resolve(destination)
		if err != nil {
			problem("%s", err)
			continue
		}
		// A destination may be written with its spaces escaped.
		if _, statErr := os.Stat(file); statErr != nil {
			if unescaped, unescapeErr := url.PathUnescape(destination); unescapeErr == nil && unescaped != destination {
				if other, resolveErr := resolve(unescaped); resolveErr == nil {
					if _, otherErr := os.Stat(other); otherErr == nil {
						file = other
					}
				}
			}
		}
		if err := checkImageFile(file); err != nil {
			problem("%s", err)
			continue
		}
		images = append(images, markdownImage{Name: destination, Path: file, Line: found.Line})
	}
	if len(images) > documentMaxImages {
		problems = append(problems, planvalidate.Diagnostic{Message: fmt.Sprintf("at most %d image files fit in one document; this one has %d", documentMaxImages, len(images))})
	}
	return images, problems
}

// checkImageFile says why a file cannot be shown as an image, without
// keeping anything of it.
func checkImageFile(path string) error {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("there is no file at %s", path)
	}
	if info.Size() > AgentUIMaxImageBytes {
		return fmt.Errorf("%s is larger than %d MB", path, AgentUIMaxImageBytes>>20)
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%s cannot be read", path)
	}
	defer file.Close()
	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s cannot be read", path)
	}
	// What a file is called does not count: the content decides.
	if _, ok := agentUIImageExtensions[http.DetectContentType(head[:n])]; !ok {
		return fmt.Errorf("%s is not an image Legato can show (PNG, JPEG, GIF, WebP or BMP)", path)
	}
	return nil
}

// imageProblemsError joins what is wrong with a document's images into one
// error that names each image's line in the file the agent wrote.
func imageProblemsError(file string, problems []planvalidate.Diagnostic) error {
	messages := make([]string, 0, len(problems))
	for _, problem := range problems {
		messages = append(messages, planvalidate.FormatDiagnostic(file, problem))
	}
	return errors.New(strings.Join(messages, "\n"))
}

// keep copies a document's image files into the owner's folder and returns
// them in the store's encoding, with the names of the copies it made, for a
// caller that must take them back when what follows fails.
func (d documentImages) keep(ownerID string, images []markdownImage) (encoded string, added []string, err error) {
	if len(images) == 0 {
		return "", nil, nil
	}
	if d.root == "" {
		return "", nil, errors.New("this Legato has no folder to keep images in, so it cannot show image files")
	}
	dir := filepath.Join(d.root, documentImageFolder(ownerID))
	// What a plan or a review shows may be private: the folder and its files
	// are the user's only.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, fmt.Errorf("the images could not be stored: %w", err)
	}
	stored := make([]storedAgentUIImage, 0, len(images))
	for _, image := range images {
		file, isNew, err := keepImage(dir, image.Path)
		if err != nil {
			d.discard(ownerID, added)
			return "", nil, fmt.Errorf("image %s: %s", image.Name, err)
		}
		if isNew {
			added = append(added, file)
		}
		stored = append(stored, storedAgentUIImage{Name: image.Name, File: file})
	}
	out, err := json.Marshal(stored)
	if err != nil {
		d.discard(ownerID, added)
		return "", nil, err
	}
	return string(out), added, nil
}

// discard removes copies that keep made for something that was then not
// stored. A copy that was there before belongs to something stored earlier,
// and is not among them.
func (d documentImages) discard(ownerID string, added []string) {
	if d.root == "" {
		return
	}
	dir := filepath.Join(d.root, documentImageFolder(ownerID))
	for _, name := range added {
		_ = os.Remove(filepath.Join(dir, name))
	}
	// Gone with its last file; a folder that still holds one stays.
	_ = os.Remove(dir)
}

// list reads a document's images and says where each is served. An image
// whose copy is gone is left out: a client told of no image shows the alt
// text, and one told of an image that is not there draws a broken picture.
func (d documentImages) list(ownerID, encoded string) []DocumentImage {
	if encoded == "" || d.root == "" {
		return nil
	}
	var stored []storedAgentUIImage
	if err := json.Unmarshal([]byte(encoded), &stored); err != nil {
		return nil
	}
	folder := documentImageFolder(ownerID)
	var images []DocumentImage
	for _, image := range stored {
		if !agentUIImageFile.MatchString(image.File) {
			continue
		}
		if _, err := os.Stat(filepath.Join(d.root, folder, image.File)); err != nil {
			continue
		}
		images = append(images, DocumentImage{Name: image.Name, URL: d.route + folder + "/" + image.File})
	}
	return images
}

// open opens one kept copy by its folder and file name, as an address
// names them. It answers store.ErrNotFound for anything that is not such a
// copy.
func (d documentImages) open(folder, file string) (*AgentUIImageFile, error) {
	if d.root == "" || !documentImageFolderName.MatchString(folder) || strings.Contains(folder, "..") || !agentUIImageFile.MatchString(file) {
		return nil, store.ErrNotFound
	}
	return openImageCopy(filepath.Join(d.root, folder, file))
}

// remove removes every image of a plan or a tour that is gone.
func (d documentImages) remove(ownerID string) {
	if d.root == "" {
		return
	}
	_ = os.RemoveAll(filepath.Join(d.root, documentImageFolder(ownerID)))
}

// documentImageGrace is how long a kept file is left alone after it was
// last used. Images are copied before what names them is stored, and
// another process may be between the two.
const documentImageGrace = time.Hour

// sweep removes what nothing names any more: the folder of an owner that is
// gone, and a file that none of an owner's documents names. named gives,
// for each owner that still exists, the store's encoding of each of its
// documents' images.
func (d documentImages) sweep(named map[string][]string) error {
	if d.root == "" {
		return nil
	}
	entries, err := os.ReadDir(d.root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	live := make(map[string]map[string]bool, len(named))
	for ownerID, encodings := range named {
		files := map[string]bool{}
		for _, encoded := range encodings {
			var stored []storedAgentUIImage
			if encoded == "" || json.Unmarshal([]byte(encoded), &stored) != nil {
				continue
			}
			for _, image := range stored {
				files[image.File] = true
			}
		}
		live[documentImageFolder(ownerID)] = files
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(d.root, entry.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		// Read before a file goes: removing one marks the folder as changed.
		folder, err := entry.Info()
		if err != nil {
			continue
		}
		remaining := 0
		for _, file := range files {
			info, err := file.Info()
			if err != nil {
				remaining++
				continue
			}
			if live[entry.Name()][file.Name()] || time.Since(info.ModTime()) < documentImageGrace {
				remaining++
				continue
			}
			_ = os.Remove(filepath.Join(dir, file.Name()))
		}
		if remaining == 0 && time.Since(folder.ModTime()) >= documentImageGrace {
			_ = os.Remove(dir)
		}
	}
	return nil
}
