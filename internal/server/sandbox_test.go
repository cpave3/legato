package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

func TestASpawnCanAskToRunWithoutTheSandbox(t *testing.T) {
	agents := &spawnRecordingAgents{}
	srv, _ := directoryServer(t, agents, "work")
	if status := serveJSON(t, srv, http.MethodPost, "/api/agents/spawn", `{"title":"t","sandbox":false}`, nil); status != http.StatusCreated {
		t.Fatalf("status = %d", status)
	}
	if status := serveJSON(t, srv, http.MethodPost, "/api/agents/spawn", `{"title":"t"}`, nil); status != http.StatusCreated {
		t.Fatalf("status = %d", status)
	}
	if len(agents.spawned) != 2 || !agents.spawned[0].Unsandboxed || agents.spawned[1].Unsandboxed {
		t.Fatalf("spawned = %+v", agents.spawned)
	}
}

func TestSettingsSayWhetherAgentsAreSandboxed(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	srv.SetSandboxEnabled(true)
	var settings struct {
		SandboxEnabled bool `json:"sandbox_enabled"`
	}
	if status := serveJSON(t, srv, http.MethodGet, "/api/settings", "", &settings); status != http.StatusOK || !settings.SandboxEnabled {
		t.Fatalf("status %d settings %+v", status, settings)
	}
}

func TestAddingAResourceToASandboxedAgentReloadsItWhenAsked(t *testing.T) {
	srv, _, attacher := resourceServer(t)
	agents := &restartAgentFake{}
	srv.agents = agents
	attacher.err = service.ErrSandboxFixed

	var failure struct{ Error string }
	if status := serveJSON(t, srv, http.MethodPost, "/api/agents/task-1/resources", `{"names":["Specs"]}`, &failure); status != http.StatusConflict || !strings.Contains(failure.Error, "reload") {
		t.Fatalf("without reload: %d %q", status, failure.Error)
	}
	if agents.restarted != "" {
		t.Fatal("restarted without being asked")
	}
	var answer struct {
		Reloaded bool `json:"reloaded"`
	}
	if status := serveJSON(t, srv, http.MethodPost, "/api/agents/task-1/resources", `{"names":["Specs"],"reload":true}`, &answer); status != http.StatusOK || !answer.Reloaded {
		t.Fatalf("with reload: %d %+v", status, answer)
	}
	if agents.restarted != "task-1" || len(agents.resources) != 1 || agents.resources[0].Name != "Specs" {
		t.Fatalf("restarted %q with %+v", agents.restarted, agents.resources)
	}
}
