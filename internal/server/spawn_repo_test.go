package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// An agent can be started in a fresh clone of a repository instead of a
// directory: Legato makes the clone for it.
func TestSpawnAgentInAClone(t *testing.T) {
	agents := &spawnRecordingAgents{}
	srv, _ := directoryServer(t, agents)
	if status := serveJSON(t, srv, http.MethodPost, "/api/agents/spawn", `{"title":"t","repo":"acme/widget","ref":"main","branch":"legato/fix"}`, nil); status != http.StatusCreated {
		t.Fatalf("status = %d", status)
	}
	if len(agents.spawned) != 1 || agents.spawned[0].Repo != "acme/widget" || agents.spawned[0].Ref != "main" || agents.spawned[0].Branch != "legato/fix" || agents.spawned[0].WorkingDir != "" {
		t.Fatalf("spawned = %+v", agents.spawned)
	}
	if status := serveJSON(t, srv, http.MethodPost, "/api/agents/spawn", `{"title":"t","repo":"acme/widget","working_dir":"/srv"}`, nil); status != http.StatusBadRequest {
		t.Fatalf("a repository and a directory: status = %d", status)
	}
}

func TestTaskAgentEndpointStartsTheAgentInAClone(t *testing.T) {
	agents := &taskAgentFake{}
	server := New(&mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}, agents, nil, ":0")
	server.workDir = "/default"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/tasks/T-1/agent", strings.NewReader(`{"repo":"acme/widget","branch":"legato/t-1"}`)))
	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	waitFor(t, server, func(l preparingList) bool { return len(l) == 0 })
	if agents.opts.Repo != "acme/widget" || agents.opts.Branch != "legato/t-1" || agents.opts.WorkingDir != "" {
		t.Fatalf("opts = %+v", agents.opts)
	}
}

func TestSettingsSayWhetherFreshClonesAreOffered(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	var settings struct {
		FreshClonesEnabled bool `json:"fresh_clones_enabled"`
	}
	if serveJSON(t, srv, http.MethodGet, "/api/settings", "", &settings); settings.FreshClonesEnabled {
		t.Fatal("fresh clones are offered by default")
	}
	srv.SetFreshClonesEnabled(true)
	if status := serveJSON(t, srv, http.MethodGet, "/api/settings", "", &settings); status != http.StatusOK || !settings.FreshClonesEnabled {
		t.Fatalf("status %d settings %+v", status, settings)
	}
}

// An agent can be started in a job clone another task made: the next
// branch of a stack, in the same folder.
func TestSpawnAgentInAJobClone(t *testing.T) {
	agents := &spawnRecordingAgents{}
	srv, _ := directoryServer(t, agents)
	if status := serveJSON(t, srv, http.MethodPost, "/api/agents/spawn", `{"title":"t","clone_id":7,"branch":"stack/two"}`, nil); status != http.StatusCreated {
		t.Fatalf("status = %d", status)
	}
	if len(agents.spawned) != 1 || agents.spawned[0].Clone != 7 || agents.spawned[0].Branch != "stack/two" || agents.spawned[0].WorkingDir != "" {
		t.Fatalf("spawned = %+v", agents.spawned)
	}
	if status := serveJSON(t, srv, http.MethodPost, "/api/agents/spawn", `{"title":"t","clone_id":7,"working_dir":"/srv"}`, nil); status != http.StatusBadRequest {
		t.Fatalf("a job clone and a directory: status = %d", status)
	}
}

func TestTaskAgentEndpointStartsTheAgentInAJobClone(t *testing.T) {
	agents := &taskAgentFake{}
	server := New(&mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}, agents, nil, ":0")
	server.workDir = "/default"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/tasks/T-1/agent", strings.NewReader(`{"clone_id":7}`)))
	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	waitFor(t, server, func(l preparingList) bool { return len(l) == 0 })
	if agents.opts.Clone != 7 || agents.opts.WorkingDir != "" {
		t.Fatalf("opts = %+v", agents.opts)
	}
}
