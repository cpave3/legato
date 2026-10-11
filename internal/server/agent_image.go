package server

import (
	"crypto/rand"
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

	"github.com/cpave3/legato/internal/engine/chimera"
)

// agentImageMaxBytes bounds an image the web UI may ask for.
const agentImageMaxBytes = 25 << 20

// agentImageTypes are the kinds of file the image endpoint gives out, as
// read from a file's own first bytes. SVG is left out on purpose: it is a
// document, and one opened from Legato's own address could run script there.
var agentImageTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
	"image/bmp":  true,
}

// The extension a stored image gets, by the type read from its content.
var agentImageExtensions = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
	"image/bmp":  ".bmp",
}

// pastedImageMaxBytes bounds one image the person pastes for an agent, and
// pastedImagesKept how many of them a task keeps.
const (
	pastedImageMaxBytes = 10 << 20
	pastedImagesKept    = 20
)

// A task's ID becomes a folder name.
var pastedImageTaskID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// pastedImageDir is where a task's pasted images are kept: under the
// system's temporary directory, because that is a place every agent tool
// lets its agent read (Chimera's read tool takes the working directory or
// /tmp and nothing else). It is not the worktree, where the files would show
// up as changes.
func pastedImageDir(taskID string) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("legato-%d", os.Getuid()), "pasted-images", taskID)
}

// agentImageHandler is the two halves of images in a conversation: GET
// serves an image file an agent read, so the web UI can show it, and POST
// stores an image the person pasted, so an agent can read it.
func (s *Server) agentImageHandler() http.HandlerFunc {
	read, store := s.readAgentImage, s.storePastedImage
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			read(w, r)
		case http.MethodPost:
			store(w, r)
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

// storePastedImage keeps the image in the request's body as a file and
// answers with its path. An agent tool takes a message as text, so the
// message names the file and the agent reads it with its own tools: that
// works for every agent, whatever it can be sent directly.
func (s *Server) storePastedImage(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("task_id")
	if !pastedImageTaskID.MatchString(taskID) {
		s.writeError(w, http.StatusBadRequest, "the task ID is not valid")
		return
	}
	content, err := io.ReadAll(http.MaxBytesReader(w, r.Body, pastedImageMaxBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			s.writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("the image is larger than %d MB", pastedImageMaxBytes>>20))
			return
		}
		s.writeError(w, http.StatusBadRequest, "the image could not be read")
		return
	}
	// What the browser calls it does not count: the content decides.
	extension, ok := agentImageExtensions[http.DetectContentType(content)]
	if !ok {
		s.writeError(w, http.StatusUnsupportedMediaType, "only PNG, JPEG, GIF, WebP and BMP images can be pasted")
		return
	}

	dir := pastedImageDir(taskID)
	// A pasted image may be private: the folder and the file are the user's only.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		s.writeError(w, http.StatusInternalServerError, "the image could not be stored: "+err.Error())
		return
	}
	suffix := make([]byte, 3)
	if _, err := rand.Read(suffix); err != nil {
		s.writeError(w, http.StatusInternalServerError, "the image could not be stored: "+err.Error())
		return
	}
	path := filepath.Join(dir, fmt.Sprintf("pasted-%s-%s%s", time.Now().UTC().Format("20060102-150405.000"), hex.EncodeToString(suffix), extension))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		s.writeError(w, http.StatusInternalServerError, "the image could not be stored: "+err.Error())
		return
	}
	prunePastedImages(dir)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"path": path})
}

// prunePastedImages removes a task's oldest pasted images past the number it
// keeps. The temporary directory is often memory, and a long session must
// not fill it.
func prunePastedImages(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type stored struct {
		name    string
		written time.Time
	}
	var images []stored
	for _, entry := range entries {
		if info, err := entry.Info(); err == nil && info.Mode().IsRegular() {
			images = append(images, stored{entry.Name(), info.ModTime()})
		}
	}
	// By when each was written: two pastes can share the millisecond in
	// their names.
	sort.Slice(images, func(a, b int) bool { return images[a].written.Before(images[b].written) })
	for index := 0; index < len(images)-pastedImagesKept; index++ {
		_ = os.Remove(filepath.Join(dir, images[index].name))
	}
}

// readAgentImage serves an image file an agent read. The file is on the
// machine Legato runs on, which is not always the one the browser is on, so
// Legato reads it.
//
// Any path may be asked for: an agent's screenshots are seldom inside its
// worktree. What limits the door is what it gives out. Only a file whose
// content is an image leaves through it, whatever the file is called.
func (s *Server) readAgentImage(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		s.writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	if !filepath.IsAbs(path) {
		dir, err := s.agentSessionDirectory(r, r.PathValue("task_id"))
		if err != nil {
			s.writeError(w, http.StatusNotFound, "the session's working directory is not known, so a relative path cannot be found")
			return
		}
		path = filepath.Join(dir, path)
	}

	file, err := os.Open(path)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "no such file")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		s.writeError(w, http.StatusNotFound, "no such file")
		return
	}
	if info.Size() > agentImageMaxBytes {
		s.writeError(w, http.StatusRequestEntityTooLarge, "the image is too large to show")
		return
	}

	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		s.writeError(w, http.StatusNotFound, "the file cannot be read")
		return
	}
	contentType := http.DetectContentType(head[:n])
	if !agentImageTypes[contentType] {
		s.writeError(w, http.StatusUnsupportedMediaType, "the file is not an image Legato can show")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		s.writeError(w, http.StatusNotFound, "the file cannot be read")
		return
	}

	header := w.Header()
	header.Set("Content-Type", contentType)
	// The type is the one read from the content, and the browser must not
	// guess another. A screenshot is often written again under the same
	// name, so no copy is kept.
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Cache-Control", "no-store")
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.WriteHeader(http.StatusOK)
	// Bounded again: the file may have grown since it was measured.
	_, _ = io.Copy(w, io.LimitReader(file, agentImageMaxBytes))
}

// agentSessionDirectory is the working directory of a session that has a
// native endpoint, as the endpoint itself reports it.
func (s *Server) agentSessionDirectory(r *http.Request, taskID string) (string, error) {
	if s.agents == nil {
		return "", errors.New("agent service not available")
	}
	session, err := s.agents.GetAgentSession(r.Context(), taskID)
	if err != nil {
		return "", err
	}
	if session == nil || session.ChimeraEndpoint == nil || session.ChimeraSessionID == nil {
		return "", errors.New("the session has no native endpoint")
	}
	snapshot, err := chimera.NewClient(*session.ChimeraEndpoint).GetSession(r.Context(), *session.ChimeraSessionID)
	if err != nil {
		return "", err
	}
	if snapshot.CWD == "" {
		return "", errors.New("the session reports no working directory")
	}
	return snapshot.CWD, nil
}
