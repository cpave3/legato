package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// showTestImage has an agent show an artifact that uses one image file, and
// returns the item as the API reads it back.
func showTestImage(t *testing.T) (*Server, service.AgentUIItem, []byte) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "agent-ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	bus := events.New()
	ui := service.NewAgentUIService(db, bus, service.AgentUIOptions{ImageDir: filepath.Join(t.TempDir(), "agent-ui-images")})
	srv := NewWithSwarm(nil, nil, nil, "", nil, bus, "")
	srv.SetAgentUIService(ui)

	content := []byte("\x89PNG\r\n\x1a\nthe board")
	path := filepath.Join(t.TempDir(), "board.png")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	shown, err := ui.Show(context.Background(), service.AgentUIShowInput{
		TaskID: "task1", Placement: service.AgentUIPanel, Format: service.AgentUIFormatHTML,
		Title: "The board", Content: `<img src="legato-image:board">`,
		Images: []service.AgentUIImageInput{{Name: "board", Path: path}},
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

func TestAgentUIImageIsServedFromTheAddressTheItemNames(t *testing.T) {
	srv, item, content := showTestImage(t)
	if len(item.Images) != 1 || item.Images[0].Name != "board" || !strings.HasPrefix(item.Images[0].URL, "/agent-ui-images/task1/") {
		t.Fatalf("the item's images = %#v", item.Images)
	}
	// The frame an agent's HTML is drawn in has no token, and must have none:
	// the image is served without one.
	srv.SetAuthToken("secret")

	got := agentUIRequest(srv, http.MethodGet, item.Images[0].URL)
	if got.Code != http.StatusOK || got.Body.String() != string(content) {
		t.Fatalf("image status=%d body=%q", got.Code, got.Body.String())
	}
	header := got.Header()
	if header.Get("Content-Type") != "image/png" || header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("type headers = %v", header)
	}
	// The address is the content's own hash: what it names never changes.
	if cache := header.Get("Cache-Control"); !strings.Contains(cache, "immutable") || !strings.Contains(cache, "private") {
		t.Fatalf("Cache-Control = %q", cache)
	}
	// Opened by itself at Legato's address, the file gets no powers there.
	if policy := header.Get("Content-Security-Policy"); !strings.Contains(policy, "default-src 'none'") || !strings.Contains(policy, "sandbox") {
		t.Fatalf("Content-Security-Policy = %q", policy)
	}
}

func TestAgentUIImageRouteGivesOutNothingElse(t *testing.T) {
	srv, item, _ := showTestImage(t)
	file := filepath.Base(item.Images[0].URL)
	for name, path := range map[string]string{
		"another task":             "/agent-ui-images/task2/" + file,
		"a name that is no hash":   "/agent-ui-images/task1/board.png",
		"an unknown hash":          "/agent-ui-images/task1/" + strings.Repeat("0", 64) + ".png",
		"a path out of the folder": "/agent-ui-images/task1/..%2F..%2Fagent-ui.db",
		"the folder itself":        "/agent-ui-images/task1/",
	} {
		if got := agentUIRequest(srv, http.MethodGet, path); got.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", name, got.Code)
		}
	}
	if got := agentUIRequest(srv, http.MethodPost, item.Images[0].URL); got.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d, want 405", got.Code)
	}
	// The API beside it still wants the token.
	srv.SetAuthToken("secret")
	if got := agentUIRequest(srv, http.MethodGet, "/api/ui-items/"+item.ID); got.Code != http.StatusUnauthorized {
		t.Fatalf("the item without a token = %d", got.Code)
	}
}
