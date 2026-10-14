package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// directoryServer is a server whose directory service has a home of its own,
// holding the named directories, and "Legato" as its one configured preset.
func directoryServer(t *testing.T, agents service.AgentService, dirs ...string) (*Server, string) {
	t.Helper()
	home := t.TempDir()
	for _, dir := range append(dirs, "Projects/legato") {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	db, err := store.New(filepath.Join(t.TempDir(), "directories.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	srv := NewWithSwarm(nil, agents, nil, ":0", nil, nil, filepath.Join(home, "Projects", "legato"))
	configured := []service.DirectoryPreset{{Name: "Legato", Path: "~/Projects/legato"}}
	srv.SetDirectoryService(service.NewDirectoryService(db, configured, home))
	return srv, home
}

func serveJSON(t *testing.T, srv *Server, method, target, body string, into any) int {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
	if into != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
			t.Fatalf("%s %s: %v in %q", method, target, err, rec.Body.String())
		}
	}
	return rec.Code
}

func TestDirectoriesEndpointSuggestsDirectoriesForATypedPath(t *testing.T) {
	srv, home := directoryServer(t, nil, "Development/autopilot/.git", "Development/automation")

	var found service.DirectorySuggestions
	status := serveJSON(t, srv, http.MethodGet, "/api/directories?q="+url.QueryEscape("~/Development/auto"), "", &found)

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if len(found.Items) != 2 || found.Total != 2 || found.Base != "~/Development/" || found.Prefix != "auto" {
		t.Fatalf("found = %+v", found)
	}
	first := found.Items[0]
	if first.Display != "~/Development/autopilot/" || !first.Git || first.Path != filepath.Join(home, "Development", "autopilot") {
		t.Fatalf("first = %+v", first)
	}
}

func TestDirectoriesEndpointKeepsToTheLimitItIsGiven(t *testing.T) {
	srv, _ := directoryServer(t, nil, "a1", "a2", "a3")

	var found service.DirectorySuggestions
	serveJSON(t, srv, http.MethodGet, "/api/directories?limit=2&q="+url.QueryEscape("~/a"), "", &found)

	if len(found.Items) != 2 || found.Total != 3 {
		t.Fatalf("found = %+v", found)
	}
}

func TestDirectoryPresetsEndpointListsPresetsAndTheDefaultDirectory(t *testing.T) {
	srv, home := directoryServer(t, nil)

	var listed directoryPresetsResponse
	status := serveJSON(t, srv, http.MethodGet, "/api/directories/presets", "", &listed)

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if len(listed.Presets) != 1 || listed.Presets[0].Name != "Legato" || listed.Presets[0].Display != "~/Projects/legato" {
		t.Fatalf("presets = %+v", listed.Presets)
	}
	if listed.Default.Path != filepath.Join(home, "Projects", "legato") || listed.Default.Display != "~/Projects/legato" {
		t.Fatalf("default = %+v", listed.Default)
	}
}

func TestDirectoryPresetsEndpointSavesADirectory(t *testing.T) {
	srv, home := directoryServer(t, nil, "work/app")

	var saved service.DirectoryPreset
	status := serveJSON(t, srv, http.MethodPost, "/api/directories/presets", `{"path":"~/work/app/"}`, &saved)

	if status != http.StatusCreated {
		t.Fatalf("status = %d", status)
	}
	if saved.Name != "app" || saved.Path != filepath.Join(home, "work", "app") || !saved.Saved {
		t.Fatalf("saved = %+v", saved)
	}
	var listed directoryPresetsResponse
	serveJSON(t, srv, http.MethodGet, "/api/directories/presets", "", &listed)
	if len(listed.Presets) != 2 || listed.Presets[1] != saved {
		t.Fatalf("presets = %+v", listed.Presets)
	}
}

func TestDirectoryPresetsEndpointRefusesWhatIsNotADirectory(t *testing.T) {
	srv, _ := directoryServer(t, nil)

	var refused map[string]string
	status := serveJSON(t, srv, http.MethodPost, "/api/directories/presets", `{"path":"~/nowhere"}`, &refused)

	if status != http.StatusBadRequest || refused["error"] != "~/nowhere is not a directory" {
		t.Fatalf("status = %d, body = %v", status, refused)
	}
}

func TestDirectoryEndpointsAnswerUnavailableWithoutADirectoryService(t *testing.T) {
	srv := New(nil, nil, nil, ":0")

	for _, target := range []string{"/api/directories?q=~", "/api/directories/presets"} {
		if status := serveJSON(t, srv, http.MethodGet, target, "", nil); status != http.StatusServiceUnavailable {
			t.Errorf("GET %s = %d", target, status)
		}
	}
}

type spawnRecordingAgents struct {
	promptAgentService
	spawned []service.AgentSpawnOptions
}

func (s *spawnRecordingAgents) SpawnEphemeralAgent(_ context.Context, _ string, _, _ int, opts ...service.AgentSpawnOptions) error {
	s.spawned = append(s.spawned, opts...)
	return nil
}

func TestSpawnAgentExpandsAWorkingDirectoryThatStartsWithATilde(t *testing.T) {
	agents := &spawnRecordingAgents{}
	srv, home := directoryServer(t, agents, "work/app")

	status := serveJSON(t, srv, http.MethodPost, "/api/agents/spawn", `{"title":"t","working_dir":"~/work/app/"}`, nil)

	if status != http.StatusCreated {
		t.Fatalf("status = %d", status)
	}
	if len(agents.spawned) != 1 || agents.spawned[0].WorkingDir != filepath.Join(home, "work", "app") {
		t.Fatalf("spawned = %+v", agents.spawned)
	}
}
