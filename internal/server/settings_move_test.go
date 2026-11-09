package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func TestPinnedSettingsMoveToTheDatabaseAndLegatoRestarts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte("icons: nerdfonts\nagents:\n  reviews: false\n"), 0o600)
	t.Setenv("LEGATO_CONFIG", path)
	db, _ := store.New(filepath.Join(dir, "legato.db"))
	t.Cleanup(func() { db.Close() })
	settings := service.NewSettingsService(db, nil)
	srv := New(nil, nil, nil, ":0")
	srv.SetAuthToken("secret")
	restarted := make(chan struct{}, 1)
	srv.SetSettingsMover(&service.SettingsMover{Settings: settings, Rollback: &service.RestartRollback{Store: db, Settings: settings}}, func() { restarted <- struct{}{} })

	rec := callUpdates(srv, http.MethodGet, "/api/config/move", "")
	var movable service.SettingsMove
	if json.Unmarshal(rec.Body.Bytes(), &movable) != nil || strings.Join(movable.Settings, ",") != "agents.reviews,icons" {
		t.Fatalf("GET: %d %s", rec.Code, rec.Body)
	}
	rec = callUpdates(srv, http.MethodPost, "/api/config/move", `{"settings": ["icons"]}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST: %d %s", rec.Code, rec.Body)
	}
	select {
	case <-restarted:
	case <-time.After(time.Second):
		t.Fatal("no restart")
	}
	if file, _ := os.ReadFile(path); strings.Contains(string(file), "icons") {
		t.Fatalf("config.yaml still has icons: %s", file)
	}
	// A second move waits for the first restart.
	if rec := callUpdates(srv, http.MethodPost, "/api/config/move", `{"settings": ["agents.reviews"]}`); rec.Code != http.StatusConflict {
		t.Fatalf("second POST: %d %s", rec.Code, rec.Body)
	}
}
