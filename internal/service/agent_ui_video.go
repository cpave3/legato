package service

import (
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
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// AgentUIFormatVideo is a narrated video with the manifest that describes
// it. The item's content is the manifest: JSON that says what is on screen
// and what is being said at every moment. Legato draws it with its own
// player, so nothing the agent wrote runs.
const AgentUIFormatVideo = "video"

// AgentUIVideoRoute is where the web server gives out the video files agents
// showed: AgentUIVideoRoute + "<task-id>/<file>". As with an image, the
// address is the SHA-256 of the content. A video element cannot send the
// person's token with its request, so the address is what stands in for it.
const AgentUIVideoRoute = "/agent-ui-videos/"

const (
	// AgentUIMaxVideoBytes bounds one video file an agent may show.
	AgentUIMaxVideoBytes = 1 << 30
	// A task keeps the videos it showed most recently. An item keeps 20
	// revisions, and a video is far larger than an image.
	agentUIVideosKept = 12
	// agentUIVideoManifestVersion is the manifest version this Legato reads.
	agentUIVideoManifestVersion = 1
	// agentUIVideoName and agentUIPosterName are the names a video item's
	// files are stored under: the video, and the picture shown for it
	// before it is played, when the video carries one.
	agentUIVideoName  = "video"
	agentUIPosterName = "poster"
)

// The kinds of video an agent may show, by the type read from a file's own
// first bytes, and the extension its copy gets.
var agentUIVideoExtensions = map[string]string{
	"video/mp4":  ".mp4",
	"video/webm": ".webm",
}

var agentUIVideoFile = regexp.MustCompile(`^[0-9a-f]{64}\.(mp4|webm)$`)

// AgentUIVideo is the video file of a video item: where the web server gives
// out Legato's copy of it.
type AgentUIVideo struct {
	URL string `json:"url"`
	// Poster is where the picture to show before the video is played is
	// given out. It is the cover picture the video file carries, and is
	// empty for a video that carries none.
	Poster string `json:"poster,omitempty"`
}

// AgentUIVideoFile is Legato's copy of a video an agent showed, open for
// reading.
type AgentUIVideoFile struct {
	*os.File
	// MediaType is the type read from the file's own content.
	MediaType string
	ModTime   time.Time
}

// agentUIVideoManifest is the part of a manifest Legato checks before it
// stores one. The web UI reads the rest.
type agentUIVideoManifest struct {
	Version    int `json:"version"`
	DurationMs int `json:"durationMs"`
	Scenes     []struct {
		ID string `json:"id"`
	} `json:"scenes"`
}

// validateAgentUIVideo checks a request to show a video, apart from the
// video file itself.
func validateAgentUIVideo(in AgentUIShowInput) error {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrAgentUIInvalid, fmt.Sprintf(format, args...))
	}
	if in.Video == "" {
		return invalid("a video item needs the path of its video file")
	}
	if len(in.Images) > 0 {
		return invalid("a video item takes no image files")
	}
	var manifest agentUIVideoManifest
	if err := json.Unmarshal([]byte(in.Content), &manifest); err != nil {
		return invalid("the manifest is not JSON: %v", err)
	}
	if manifest.Version != agentUIVideoManifestVersion {
		return invalid("the manifest is version %d; this Legato reads version %d", manifest.Version, agentUIVideoManifestVersion)
	}
	if manifest.DurationMs <= 0 || len(manifest.Scenes) == 0 {
		return invalid("the manifest describes no video: it needs durationMs and scenes")
	}
	return nil
}

// keepVideo copies a video file into the task's folder and returns it in the
// store's encoding.
func (s *AgentUIService) keepVideo(taskID, path string) (string, error) {
	if s.videoDir == "" {
		return "", errors.New("this Legato has no folder to keep videos in, so it cannot show a video")
	}
	if !ValidPastedImageTaskID(taskID) {
		return "", fmt.Errorf("%w: task ID %q cannot name a folder of videos", ErrAgentUIInvalid, taskID)
	}
	dir := filepath.Join(s.videoDir, taskID)
	// What an agent shows may be private: the folder and its files are the
	// user's only.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("the video could not be stored: %w", err)
	}
	file, err := keepVideoFile(dir, path)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrAgentUIInvalid, err)
	}
	pruneAgentUIFiles(dir, agentUIVideoFile, agentUIVideosKept)
	files := []storedAgentUIImage{{Name: agentUIVideoName, File: file}}
	// A video with no cover picture, or one Legato cannot keep, is shown
	// by its first frame: the poster is a courtesy, never a reason to turn
	// a video down.
	if poster := s.keepPoster(taskID, filepath.Join(dir, file)); poster != "" {
		files = append(files, storedAgentUIImage{Name: agentUIPosterName, File: poster})
	}
	encoded, err := json.Marshal(files)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// keepVideoFile copies one video file into dir under the SHA-256 of its
// content and returns the copy's name. A video is too large to hold in
// memory, so it is hashed while it is copied.
func keepVideoFile(dir, path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("%s is not a path Legato can read", path)
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("there is no file at %s", absolute)
	}
	if info.Size() > AgentUIMaxVideoBytes {
		return "", fmt.Errorf("%s is larger than %d MB", absolute, AgentUIMaxVideoBytes>>20)
	}
	source, err := os.Open(absolute)
	if err != nil {
		return "", fmt.Errorf("%s cannot be read", absolute)
	}
	defer source.Close()

	// What the file is called does not count: the content decides.
	head := make([]byte, 512)
	n, err := io.ReadFull(source, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("%s cannot be read", absolute)
	}
	extension, ok := agentUIVideoExtensions[http.DetectContentType(head[:n])]
	if !ok {
		return "", fmt.Errorf("%s is not a video Legato can show (MP4 or WebM)", absolute)
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("%s cannot be read", absolute)
	}

	// Written beside its final name and moved there, so that a reader never
	// finds half a file under a name that promises its content.
	incoming, err := os.CreateTemp(dir, ".incoming-*")
	if err != nil {
		return "", fmt.Errorf("the video could not be stored: %s", err)
	}
	defer os.Remove(incoming.Name())
	hash := sha256.New()
	// Bounded again: the file may have grown since it was measured.
	written, copyErr := io.Copy(io.MultiWriter(incoming, hash), io.LimitReader(source, AgentUIMaxVideoBytes+1))
	closeErr := incoming.Close()
	switch {
	case copyErr != nil:
		return "", fmt.Errorf("the video could not be stored: %s", copyErr)
	case closeErr != nil:
		return "", fmt.Errorf("the video could not be stored: %s", closeErr)
	case written > AgentUIMaxVideoBytes:
		return "", fmt.Errorf("%s is larger than %d MB", absolute, AgentUIMaxVideoBytes>>20)
	}
	name := hex.EncodeToString(hash.Sum(nil)) + extension
	kept := filepath.Join(dir, name)
	if _, err := os.Stat(kept); err == nil {
		now := time.Now()
		_ = os.Chtimes(kept, now, now)
		return name, nil
	}
	if err := os.Chmod(incoming.Name(), 0o600); err != nil {
		return "", fmt.Errorf("the video could not be stored: %s", err)
	}
	if err := os.Rename(incoming.Name(), kept); err != nil {
		return "", fmt.Errorf("the video could not be stored: %s", err)
	}
	return name, nil
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

// OpenVideo opens Legato's copy of a video a task's agent showed. It answers
// store.ErrNotFound for anything that is not such a copy.
func (s *AgentUIService) OpenVideo(taskID, file string) (*AgentUIVideoFile, error) {
	if s.videoDir == "" || !ValidPastedImageTaskID(taskID) || !agentUIVideoFile.MatchString(file) {
		return nil, store.ErrNotFound
	}
	opened, err := os.Open(filepath.Join(s.videoDir, taskID, file))
	if err != nil {
		return nil, store.ErrNotFound
	}
	info, err := opened.Stat()
	if err != nil || !info.Mode().IsRegular() {
		opened.Close()
		return nil, store.ErrNotFound
	}
	// Read from the content again, not from the name: only a video leaves
	// through this door, whatever came to lie in the folder.
	head := make([]byte, 512)
	n, err := io.ReadFull(opened, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		opened.Close()
		return nil, store.ErrNotFound
	}
	mediaType := http.DetectContentType(head[:n])
	if _, ok := agentUIVideoExtensions[mediaType]; !ok {
		opened.Close()
		return nil, store.ErrNotFound
	}
	if _, err := opened.Seek(0, io.SeekStart); err != nil {
		opened.Close()
		return nil, store.ErrNotFound
	}
	return &AgentUIVideoFile{File: opened, MediaType: mediaType, ModTime: info.ModTime()}, nil
}

// keepPoster reads the cover picture out of a stored video and keeps it with
// the task's images, where the image route gives it out. It returns the
// copy's name, or "" when there is no picture to keep.
func (s *AgentUIService) keepPoster(taskID, video string) string {
	if s.imageDir == "" {
		return ""
	}
	file, err := os.Open(video)
	if err != nil {
		return ""
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ""
	}
	cover := mp4Cover(file, info.Size())
	if cover == nil {
		return ""
	}
	dir := filepath.Join(s.imageDir, taskID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	name, _, err := keepImageContent(dir, cover)
	if err != nil {
		return ""
	}
	pruneAgentUIFiles(dir, agentUIImageFile, agentUIImagesKept)
	return name
}

// agentUIVideoFromStore reads a video item's files and says where each is
// served. It is nil for an item that names no video.
func agentUIVideoFromStore(taskID, encoded string) *AgentUIVideo {
	if encoded == "" {
		return nil
	}
	var stored []storedAgentUIImage
	if err := json.Unmarshal([]byte(encoded), &stored); err != nil {
		return nil
	}
	var video *AgentUIVideo
	poster := ""
	for _, file := range stored {
		switch {
		case file.Name == agentUIVideoName && agentUIVideoFile.MatchString(file.File):
			video = &AgentUIVideo{URL: AgentUIVideoRoute + taskID + "/" + file.File}
		case file.Name == agentUIPosterName && agentUIImageFile.MatchString(file.File):
			poster = AgentUIImageRoute + taskID + "/" + file.File
		}
	}
	if video != nil {
		video.Poster = poster
	}
	return video
}

// storedVideo is agentUIVideoFromStore for a client about to play the file:
// a video whose copy is gone is left out, so the client can say so.
func (s *AgentUIService) storedVideo(item *store.AgentUIItem) *AgentUIVideo {
	video := agentUIVideoFromStore(item.TaskID, item.Images)
	if video == nil {
		return nil
	}
	if _, err := os.Stat(filepath.Join(s.videoDir, item.TaskID, filepath.Base(video.URL))); err != nil {
		return nil
	}
	if video.Poster != "" {
		if _, err := os.Stat(filepath.Join(s.imageDir, item.TaskID, filepath.Base(video.Poster))); err != nil {
			video.Poster = ""
		}
	}
	return video
}
