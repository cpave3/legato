package server

import (
	"net/http"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func repositoryServer(t *testing.T) *Server {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "repositories.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	srv := New(nil, nil, nil, ":0")
	srv.SetRepositoryPresetService(service.NewRepositoryPresetService(db, []service.RepositoryPreset{{Name: "Legato", Repo: "cpave3/legato", BaseBranch: "main"}}, t.TempDir()))
	return srv
}

func TestRepositoriesCanBeAddedListedAndRemoved(t *testing.T) {
	srv := repositoryServer(t)

	var added service.RepositoryPreset
	if status := serveJSON(t, srv, http.MethodPost, "/api/repositories", `{"name":"Widget","repo":"acme/widget","base_branch":"develop"}`, &added); status != http.StatusCreated || added.ID == 0 {
		t.Fatalf("add: %d %+v", status, added)
	}
	var listed struct {
		Repositories []service.RepositoryPreset `json:"repositories"`
	}
	if status := serveJSON(t, srv, http.MethodGet, "/api/repositories", "", &listed); status != http.StatusOK || len(listed.Repositories) != 2 || !listed.Repositories[0].Configured || listed.Repositories[1].Name != "Widget" {
		t.Fatalf("list: %d %+v", status, listed)
	}

	// The dialogs and the workflow form see it at once.
	var settings struct {
		Repositories []struct{ Name, Repo string } `json:"repositories"`
	}
	serveJSON(t, srv, http.MethodGet, "/api/settings", "", &settings)
	if len(settings.Repositories) != 2 || settings.Repositories[1].Repo != "acme/widget" {
		t.Fatalf("settings repositories %+v", settings.Repositories)
	}

	if status := serveJSON(t, srv, http.MethodDelete, "/api/repositories/"+strconv.FormatInt(added.ID, 10), "", nil); status != http.StatusNoContent {
		t.Fatalf("delete: %d", status)
	}
	if status := serveJSON(t, srv, http.MethodDelete, "/api/repositories/"+strconv.FormatInt(added.ID, 10), "", nil); status != http.StatusNotFound {
		t.Fatalf("delete again: %d", status)
	}
}

func TestARepositoryThatCannotBeAddedSaysWhy(t *testing.T) {
	srv := repositoryServer(t)
	var failure struct{ Error string }
	if status := serveJSON(t, srv, http.MethodPost, "/api/repositories", `{"name":"Legato","repo":"cpave3/other"}`, &failure); status != http.StatusBadRequest || failure.Error == "" {
		t.Fatalf("a name of config.yaml: %d %+v", status, failure)
	}
}
