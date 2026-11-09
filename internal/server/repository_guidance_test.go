package server

import (
	"net/http"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func repositoryGuidanceServer(t *testing.T) *Server {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "guidance.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	srv := New(nil, nil, nil, ":0")
	srv.SetRepositoryGuidanceService(service.NewRepositoryGuidanceService(db, []config.AgentGuidance{
		{Repo: "github.com/acme/web-app", Text: "From the config file."},
	}))
	return srv
}

type guidanceList struct {
	Guidance []service.RepositoryGuidance `json:"guidance"`
}

func TestRepositoryGuidanceCanBeSavedListedAndRemoved(t *testing.T) {
	srv := repositoryGuidanceServer(t)
	const path = "/api/settings/repository-guidance"

	var saved service.RepositoryGuidance
	if status := serveJSON(t, srv, http.MethodPut, path, `{"repo":"git@github.com:Acme/API.git","text":"Run the API checks."}`, &saved); status != http.StatusOK {
		t.Fatalf("save: %d %+v", status, saved)
	}
	if saved != (service.RepositoryGuidance{Repo: "github.com/acme/api", Text: "Run the API checks."}) {
		t.Fatalf("saved %+v", saved)
	}

	var listed guidanceList
	if status := serveJSON(t, srv, http.MethodGet, path, "", &listed); status != http.StatusOK {
		t.Fatalf("list: %d", status)
	}
	want := []service.RepositoryGuidance{
		{Repo: "github.com/acme/web-app", Text: "From the config file.", Configured: true},
		{Repo: "github.com/acme/api", Text: "Run the API checks."},
	}
	if !reflect.DeepEqual(listed.Guidance, want) {
		t.Fatalf("list %+v", listed.Guidance)
	}

	if status := serveJSON(t, srv, http.MethodDelete, path+"?repo=github.com/acme/api", "", nil); status != http.StatusNoContent {
		t.Fatalf("remove: %d", status)
	}
	if status := serveJSON(t, srv, http.MethodDelete, path+"?repo=github.com/acme/api", "", nil); status != http.StatusNotFound {
		t.Fatalf("remove again: %d", status)
	}
}

func TestRepositoryGuidanceThatCannotBeSavedSaysWhy(t *testing.T) {
	srv := repositoryGuidanceServer(t)
	const path = "/api/settings/repository-guidance"
	for name, tc := range map[string]struct {
		method, path, body string
		status             int
	}{
		"not a repository":       {http.MethodPut, path, `{"repo":"web-app","text":"x"}`, http.StatusBadRequest},
		"no text":                {http.MethodPut, path, `{"repo":"acme/api","text":" "}`, http.StatusBadRequest},
		"set in config.yaml":     {http.MethodPut, path, `{"repo":"acme/web-app","text":"x"}`, http.StatusConflict},
		"removing config.yaml's": {http.MethodDelete, path + "?repo=acme/web-app", "", http.StatusConflict},
		"removing with no repo":  {http.MethodDelete, path, "", http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			var failure struct{ Error string }
			if status := serveJSON(t, srv, tc.method, tc.path, tc.body, &failure); status != tc.status || failure.Error == "" {
				t.Fatalf("%d %+v, want %d with a reason", status, failure, tc.status)
			}
		})
	}
}

func TestRepositoryGuidanceWithoutTheServiceIsUnavailable(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	if status := serveJSON(t, srv, http.MethodGet, "/api/settings/repository-guidance", "", nil); status != http.StatusServiceUnavailable {
		t.Fatalf("status %d", status)
	}
}
