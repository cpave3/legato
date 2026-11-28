package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func managedHooksServer(t *testing.T) (*Server, string) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "hooks.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	dir := filepath.Join(t.TempDir(), "managed-hooks")
	srv := New(nil, nil, nil, ":0")
	srv.SetManagedHookService(service.NewManagedHookService(dir, db))
	return srv, dir
}

func dropManagedHook(t *testing.T, dir, name, manifest string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name, "HOOK.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

type managedHookList struct {
	Dir          string                `json:"dir"`
	Hooks        []service.ManagedHookListing `json:"hooks"`
	Repositories []any                 `json:"repositories"`
}

const codeevalManifest = "name: codeeval\ndescription: Net-debt gate.\nevents:\n  stop: {command: ./stop.sh}\n"

func TestManagedHooksAreListedAndTurnedOffAndOn(t *testing.T) {
	srv, dir := managedHooksServer(t)
	const path = "/api/settings/managed-hooks"

	var listed managedHookList
	if status := serveJSON(t, srv, http.MethodGet, path, "", &listed); status != http.StatusOK || listed.Dir != dir || len(listed.Hooks) != 0 || listed.Repositories == nil {
		t.Fatalf("empty list: %d %+v", status, listed)
	}

	// A folder dropped in shows at the next ask, a broken one with its error.
	dropManagedHook(t, dir, "codeeval", codeevalManifest)
	dropManagedHook(t, dir, "broken", "name: broken\ndescription: x\nevents:\n  stop: {command: ./a, match: [shell]}\n")
	if status := serveJSON(t, srv, http.MethodGet, path, "", &listed); status != http.StatusOK || len(listed.Hooks) != 2 {
		t.Fatalf("list: %d %+v", status, listed)
	}
	broken, gate := listed.Hooks[0], listed.Hooks[1]
	if broken.Name != "broken" || broken.Err == "" {
		t.Fatalf("broken = %+v, want its error", broken)
	}
	if gate.Name != "codeeval" || gate.Description != "Net-debt gate." || gate.Path != filepath.Join(dir, "codeeval") || !gate.Enabled || gate.Err != "" || len(gate.Events) != 1 {
		t.Fatalf("codeeval = %+v", gate)
	}

	var changed service.ManagedHookListing
	if status := serveJSON(t, srv, http.MethodPut, path+"/codeeval", `{"enabled":false}`, &changed); status != http.StatusOK || changed.Name != "codeeval" || changed.Enabled {
		t.Fatalf("turning codeeval off: %d %+v", status, changed)
	}
	serveJSON(t, srv, http.MethodGet, path, "", &listed)
	if listed.Hooks[1].Enabled {
		t.Fatalf("codeeval still on: %+v", listed.Hooks[1])
	}
	if status := serveJSON(t, srv, http.MethodPut, path+"/codeeval", `{"enabled":true}`, &changed); status != http.StatusOK || !changed.Enabled {
		t.Fatalf("turning codeeval on: %d %+v", status, changed)
	}

	// Changed on disk: the next list says so, without a restart.
	dropManagedHook(t, dir, "codeeval", "name: codeeval\ndescription: Changed.\nevents:\n  stop: {command: ./stop.sh}\n")
	serveJSON(t, srv, http.MethodGet, path, "", &listed)
	if listed.Hooks[1].Description != "Changed." {
		t.Fatalf("after a change on disk: %+v", listed.Hooks[1])
	}
}

func TestTurningAManagedHookOnOrOffIsRefusedWhenItCannotBe(t *testing.T) {
	srv, dir := managedHooksServer(t)
	dropManagedHook(t, dir, "codeeval", codeevalManifest)
	dropManagedHook(t, dir, "broken", "name: other\ndescription: x\nevents:\n  stop: {command: ./a}\n")
	for _, tc := range []struct {
		name, path, body string
		want             int
	}{
		{"no state", "/codeeval", `{}`, http.StatusBadRequest},
		{"not JSON", "/codeeval", `on`, http.StatusBadRequest},
		{"absent", "/missing", `{"enabled":false}`, http.StatusNotFound},
		{"errored", "/broken", `{"enabled":false}`, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body map[string]any
			if status := serveJSON(t, srv, http.MethodPut, "/api/settings/managed-hooks"+tc.path, tc.body, &body); status != tc.want {
				t.Fatalf("status = %d %v, want %d", status, body, tc.want)
			}
		})
	}

	unwired := New(nil, nil, nil, ":0")
	var body map[string]any
	if status := serveJSON(t, unwired, http.MethodGet, "/api/settings/managed-hooks", "", &body); status != http.StatusServiceUnavailable {
		t.Fatalf("unwired GET = %d", status)
	}
	if status := serveJSON(t, unwired, http.MethodPut, "/api/settings/managed-hooks/codeeval", `{"enabled":true}`, &body); status != http.StatusServiceUnavailable {
		t.Fatalf("unwired PUT = %d", status)
	}
}
