package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// showTestVideo has an agent show a video, and returns the item as the API
// reads it back.
func showTestVideo(t *testing.T) (*Server, service.AgentUIItem, []byte) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "agent-ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	bus := events.New()
	ui := service.NewAgentUIService(db, bus, service.AgentUIOptions{VideoDir: filepath.Join(t.TempDir(), "agent-ui-videos")})
	srv := NewWithSwarm(nil, nil, nil, "", nil, bus, "")
	srv.SetAgentUIService(ui)

	content := []byte("\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isommp41" + strings.Repeat("frame ", 200))
	path := filepath.Join(t.TempDir(), "tour.mp4")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	shown, err := ui.Show(context.Background(), service.AgentUIShowInput{
		TaskID: "task1", Placement: service.AgentUIPanel, Format: service.AgentUIFormatVideo, Title: "Tour",
		Content: `{"version":1,"durationMs":4000,"scenes":[{"id":"intro"}]}`, Video: path,
	})
	if err != nil {
		t.Fatal(err)
	}
	read := agentUIRequest(srv, http.MethodGet, "/api/ui-items/"+shown.ID)
	if read.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", read.Code, read.Body.String())
	}
	var item service.AgentUIItem
	if err := json.Unmarshal(read.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	return srv, item, content
}

func TestAgentUIVideoIsServedFromTheAddressTheItemNames(t *testing.T) {
	srv, item, content := showTestVideo(t)
	if item.Format != "video" || item.Video == nil || !strings.HasPrefix(item.Video.URL, "/agent-ui-videos/task1/") {
		t.Fatalf("the item = format %q, video %#v", item.Format, item.Video)
	}
	// A video element sends no token: the video is served without one.
	srv.SetAuthToken("secret")

	got := agentUIRequest(srv, http.MethodGet, item.Video.URL)
	if got.Code != http.StatusOK || got.Body.String() != string(content) {
		t.Fatalf("video status=%d, %d bytes", got.Code, got.Body.Len())
	}
	header := got.Header()
	if header.Get("Content-Type") != "video/mp4" || header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("type headers = %v", header)
	}
	if cache := header.Get("Cache-Control"); !strings.Contains(cache, "immutable") || !strings.Contains(cache, "private") {
		t.Fatalf("Cache-Control = %q", cache)
	}
	if policy := header.Get("Content-Security-Policy"); !strings.Contains(policy, "default-src 'none'") || !strings.Contains(policy, "sandbox") {
		t.Fatalf("Content-Security-Policy = %q", policy)
	}
}

func TestAgentUIVideoRouteAnswersARangeSoThePlayerCanSeek(t *testing.T) {
	srv, item, content := showTestVideo(t)
	req := httptest.NewRequest(http.MethodGet, item.Video.URL, nil)
	req.Header.Set("Range", "bytes=100-199")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != string(content[100:200]) {
		t.Fatalf("range status=%d, %d bytes", rec.Code, rec.Body.Len())
	}
	if got := rec.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Errorf("Accept-Ranges = %q", got)
	}
}

func TestAgentUIVideoRouteGivesOutNothingElse(t *testing.T) {
	srv, item, _ := showTestVideo(t)
	file := filepath.Base(item.Video.URL)
	for name, path := range map[string]string{
		"another task":             "/agent-ui-videos/task2/" + file,
		"a name that is no hash":   "/agent-ui-videos/task1/tour.mp4",
		"an unknown hash":          "/agent-ui-videos/task1/" + strings.Repeat("0", 64) + ".mp4",
		"a path out of the folder": "/agent-ui-videos/task1/..%2F..%2Fagent-ui.db",
		"the folder itself":        "/agent-ui-videos/task1/",
	} {
		if got := agentUIRequest(srv, http.MethodGet, path); got.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", name, got.Code)
		}
	}
	if got := agentUIRequest(srv, http.MethodPost, item.Video.URL); got.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d, want 405", got.Code)
	}
}
