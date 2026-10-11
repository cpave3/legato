package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// pngBytes is a real, small PNG image.
func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	img.Set(1, 1, color.RGBA{R: 200, G: 30, B: 30, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func writeFile(t *testing.T, path string, content []byte) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func getAgentImage(t *testing.T, server *Server, taskID, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/agents/"+taskID+"/image?path="+url.QueryEscape(path), nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

// An agent read an image file; the person opens it in the web UI. The file
// is on the machine Legato runs on, so Legato reads it for the browser.
func TestAgentImageServesAnImageFileFromItsPath(t *testing.T) {
	content := pngBytes(t)
	path := writeFile(t, filepath.Join(t.TempDir(), "shot.png"), content)
	server := New(nil, &promptAgentService{}, nil, ":0")

	response := getAgentImage(t, server, "task-1", path)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type=%q", got)
	}
	if !bytes.Equal(response.Body.Bytes(), content) {
		t.Errorf("the body is not the file: %d bytes, want %d", response.Body.Len(), len(content))
	}
	// The browser must take the type as given, and must not keep a copy: a
	// screenshot is often written again under the same name.
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options=%q", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control=%q", got)
	}
}

// What a file is called does not make it an image: only its content does.
// Anything else on the disk stays unreadable through this door.
func TestAgentImageRefusesAFileThatIsNotAnImage(t *testing.T) {
	dir := t.TempDir()
	cases := map[string][]byte{
		"secret.png": []byte("api_key = sk-not-an-image\n"),
		"notes.txt":  []byte("plain text"),
		// An SVG opened from Legato's own address could run script there.
		"drawing.svg": []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"page.png":    []byte("<!doctype html><html><body><script>alert(1)</script></body></html>"),
	}
	server := New(nil, &promptAgentService{}, nil, ":0")
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			response := getAgentImage(t, server, "task-1", writeFile(t, filepath.Join(dir, name), content))
			if response.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), string(content)) {
				t.Errorf("the refusal carries the file's content: %s", response.Body.String())
			}
		})
	}
}

func TestAgentImageSaysWhenThereIsNoSuchFile(t *testing.T) {
	dir := t.TempDir()
	server := New(nil, &promptAgentService{}, nil, ":0")

	for name, path := range map[string]string{"missing file": filepath.Join(dir, "gone.png"), "a directory": dir} {
		t.Run(name, func(t *testing.T) {
			if response := getAgentImage(t, server, "task-1", path); response.Code != http.StatusNotFound {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	if response := getAgentImage(t, server, "task-1", ""); response.Code != http.StatusBadRequest {
		t.Fatalf("no path: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAgentImageRefusesAFileOverTheSizeLimit(t *testing.T) {
	content := append(pngBytes(t), make([]byte, agentImageMaxBytes)...)
	path := writeFile(t, filepath.Join(t.TempDir(), "huge.png"), content)
	server := New(nil, &promptAgentService{}, nil, ":0")

	if response := getAgentImage(t, server, "task-1", path); response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

// Chimera's read tool takes a path relative to the session's working
// directory, which the session's own endpoint reports.
func TestAgentImageResolvesARelativePathInTheSessionsDirectory(t *testing.T) {
	dir := t.TempDir()
	content := pngBytes(t)
	writeFile(t, filepath.Join(dir, "docs", "shot.png"), content)
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sessions/session-1" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{"id":"session-1","status":"idle","cwd":%q}`, dir)
	}))
	t.Cleanup(native.Close)
	sessionID, endpoint := "session-1", native.URL
	agents := &chimeraSnapshotAgentFake{session: &store.AgentSession{ID: 3, TaskID: "task-1", ChimeraSessionID: &sessionID, ChimeraEndpoint: &endpoint}}
	server := New(nil, agents, nil, ":0")

	response := getAgentImage(t, server, "task-1", "docs/shot.png")

	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), content) {
		t.Fatalf("status=%d, %d bytes: %s", response.Code, response.Body.Len(), response.Header().Get("Content-Type"))
	}
}

// With no session to ask, a relative path has no meaning.
func TestAgentImageRefusesARelativePathWithoutASessionDirectory(t *testing.T) {
	server := New(nil, &promptAgentService{}, nil, ":0")

	response := getAgentImage(t, server, "task-1", "docs/shot.png")

	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAgentImageAnswersGetAndPostOnly(t *testing.T) {
	server := New(nil, &promptAgentService{}, nil, ":0")
	request := httptest.NewRequest(http.MethodDelete, "/api/agents/task-1/image?path=/tmp/x.png", nil)
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", response.Code)
	}
}

// The door reads from the whole disk, so it is behind the token like the
// rest of the API.
func TestAgentImageNeedsTheToken(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "shot.png"), pngBytes(t))
	server := New(nil, &promptAgentService{}, nil, ":0")
	server.SetAuthToken("secret-token")

	if response := getAgentImage(t, server, "task-1", path); response.Code != http.StatusUnauthorized {
		t.Fatalf("without a token: status=%d", response.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/agents/task-1/image?path="+url.QueryEscape(path), nil)
	request.Header.Set("Authorization", "Bearer secret-token")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("with the token: status=%d body=%s", response.Code, response.Body.String())
	}
}

func postAgentImage(t *testing.T, server *Server, taskID string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/agents/"+taskID+"/image", bytes.NewReader(content))
	request.Header.Set("Content-Type", "image/png")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func pastedPath(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Path == "" {
		t.Fatalf("no path in %s (%v)", response.Body.String(), err)
	}
	return body.Path
}

// The person pastes an image into the message box. An agent tool takes text,
// not images, so Legato keeps the image as a file and the message names its
// path: every agent can read a file.
func TestAgentImageStoresAPastedImageAndReturnsItsPath(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	content := pngBytes(t)
	server := New(nil, &promptAgentService{}, nil, ":0")

	path := pastedPath(t, postAgentImage(t, server, "task-1", content))

	// Under the system's temporary directory, which every agent tool lets its
	// agent read, in a folder of the task's own.
	if !strings.HasPrefix(path, temp+string(filepath.Separator)) || filepath.Base(filepath.Dir(path)) != "task-1" {
		t.Errorf("path=%q is not in the task's folder under %q", path, temp)
	}
	if filepath.Ext(path) != ".png" {
		t.Errorf("path=%q does not end in the image's own extension", path)
	}
	stored, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(stored, content) {
		t.Fatalf("stored %d bytes, want %d (%v)", len(stored), len(content), err)
	}
	// A pasted image may be private: the file and its folder are the user's only.
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("file mode=%v", info.Mode().Perm())
	}
	if info, _ := os.Stat(filepath.Dir(path)); info.Mode().Perm() != 0o700 {
		t.Errorf("folder mode=%v", info.Mode().Perm())
	}
	// The person can look at it again from the conversation.
	if response := getAgentImage(t, server, "task-1", path); response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), content) {
		t.Errorf("reading it back: status=%d, %d bytes", response.Code, response.Body.Len())
	}
}

func TestAgentImageRefusesAPasteThatIsNotAnImage(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	server := New(nil, &promptAgentService{}, nil, ":0")

	for name, content := range map[string][]byte{
		"text":  []byte("#!/bin/sh\nrm -rf ~\n"),
		"svg":   []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"empty": {},
	} {
		t.Run(name, func(t *testing.T) {
			if response := postAgentImage(t, server, "task-1", content); response.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	if entries, _ := os.ReadDir(temp); len(entries) != 0 {
		t.Errorf("a refused paste left files behind: %v", entries)
	}
}

func TestAgentImageRefusesAPasteOverTheSizeLimit(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	server := New(nil, &promptAgentService{}, nil, ":0")
	content := append(pngBytes(t), make([]byte, pastedImageMaxBytes)...)

	if response := postAgentImage(t, server, "task-1", content); response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

// The task's ID becomes a folder name, so it must not be able to name
// another place.
func TestAgentImageRefusesATaskIDThatIsNotAName(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	server := New(nil, &promptAgentService{}, nil, ":0")

	for _, taskID := range []string{"..%2F..%2Foutside", "a%2Fb", "%2E%2E", "with%20space"} {
		if response := postAgentImage(t, server, taskID, pngBytes(t)); response.Code != http.StatusBadRequest {
			t.Errorf("task id %q: status=%d", taskID, response.Code)
		}
	}
	if entries, _ := os.ReadDir(temp); len(entries) != 0 {
		t.Errorf("a refused paste left files behind: %v", entries)
	}
}

// The temporary directory is often memory. A task keeps its newest images
// and lets the oldest go, so a long session cannot fill it.
func TestAgentImageKeepsOnlyATasksNewestPastes(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	server := New(nil, &promptAgentService{}, nil, ":0")
	var paths []string
	for range pastedImagesKept + 2 {
		paths = append(paths, pastedPath(t, postAgentImage(t, server, "task-1", pngBytes(t))))
	}
	other := pastedPath(t, postAgentImage(t, server, "task-2", pngBytes(t)))

	for index, path := range paths {
		_, err := os.Stat(path)
		if gone := index < 2; gone != os.IsNotExist(err) {
			t.Errorf("paste %d of %d: gone=%v, want %v", index+1, len(paths), os.IsNotExist(err), gone)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("another task's paste was removed: %v", err)
	}
}
