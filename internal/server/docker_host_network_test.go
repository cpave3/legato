package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

func TestASpawnCanAskForTheHostNetworkForItsContainers(t *testing.T) {
	agents := &spawnRecordingAgents{}
	srv, _ := directoryServer(t, agents, "work")
	if status := serveJSON(t, srv, http.MethodPost, "/api/agents/spawn", `{"title":"t","docker_host_network":true}`, nil); status != http.StatusCreated {
		t.Fatalf("status = %d", status)
	}
	if status := serveJSON(t, srv, http.MethodPost, "/api/agents/spawn", `{"title":"t"}`, nil); status != http.StatusCreated {
		t.Fatalf("status = %d", status)
	}
	if len(agents.spawned) != 2 || !agents.spawned[0].DockerHostNetwork || agents.spawned[1].DockerHostNetwork {
		t.Fatalf("spawned = %+v", agents.spawned)
	}

	task := &taskAgentFake{}
	if rec := postTaskAgent(t, task, `{"docker_host_network":true}`); rec.Code != http.StatusCreated || !task.opts.DockerHostNetwork {
		t.Fatalf("task agent: %d %s, opts %+v", rec.Code, rec.Body, task.opts)
	}
}

// Where the service refuses it, the caller hears why, as a bad request.
func TestAHostNetworkTheServiceRefusesIsABadRequest(t *testing.T) {
	refused := fmt.Errorf("%w is not allowed for the Reeve", service.ErrDockerHostNetwork)
	task := &taskAgentFake{spawnErr: refused}
	if rec := postTaskAgent(t, task, `{"docker_host_network":true}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "host network") {
		t.Fatalf("task agent: %d %s", rec.Code, rec.Body)
	}
}

// A running agent is not changed by a spawn: it would go on without what
// was asked. Reloading it changes it.
func TestAskingARunningAgentForTheHostNetworkSaysToReloadIt(t *testing.T) {
	task := &promptTaskAgents{taskAgentFake: taskAgentFake{live: true}}
	if rec := postTaskAgent(t, task, `{"docker_host_network":true}`); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "restart") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

type hostNetworkRestartFake struct {
	restartAgentFake
	set *bool
}

func (f *hostNetworkRestartFake) RestartAgentSettingDockerHostNetwork(_ context.Context, taskID string, resources []service.Resource, on bool) error {
	if f.err != nil {
		return f.err
	}
	f.restarted, f.resources, f.set = taskID, resources, &on
	return nil
}

func TestRestartingCanTurnTheHostNetworkOnOrOff(t *testing.T) {
	for _, on := range []bool{true, false} {
		agents := &hostNetworkRestartFake{restartAgentFake: restartAgentFake{attached: []string{"Vault"}}}
		srv := restartServer(&agents.restartAgentFake)
		srv.agents = agents
		var answer struct {
			Restarted         bool `json:"restarted"`
			DockerHostNetwork bool `json:"docker_host_network"`
		}
		status := serveJSON(t, srv, http.MethodPost, "/api/agents/T-1/restart", fmt.Sprintf(`{"docker_host_network": %v}`, on), &answer)
		if status != http.StatusOK || agents.set == nil || *agents.set != on || !answer.Restarted || answer.DockerHostNetwork != on {
			t.Fatalf("on=%v: status %d, set %v, answer %+v", on, status, agents.set, answer)
		}
		if len(agents.resources) != 1 || agents.resources[0].Name != "Vault" {
			t.Fatalf("resources = %+v, want Vault kept", agents.resources)
		}
	}

	agents := &hostNetworkRestartFake{}
	srv := restartServer(&agents.restartAgentFake)
	srv.agents = agents
	if status := serveJSON(t, srv, http.MethodPost, "/api/agents/T-1/restart", `{"docker_host_network": true, "switch_sandbox": true}`, nil); status != http.StatusBadRequest || agents.restarted != "" {
		t.Fatalf("both at once: %d, restarted %q", status, agents.restarted)
	}
	agents.err = fmt.Errorf("%w is not allowed outside Legato's sandbox", service.ErrDockerHostNetwork)
	if status := serveJSON(t, srv, http.MethodPost, "/api/agents/T-1/restart", `{"docker_host_network": true}`, nil); status != http.StatusBadRequest {
		t.Fatalf("refused by the service: %d", status)
	}
}

func TestAgentsAreListedWithTheHostNetwork(t *testing.T) {
	if r := agentResponse(service.AgentSession{TaskID: "T-1", Sandboxed: true, DockerHostNetwork: true}); !r.DockerHostNetwork {
		t.Fatalf("response = %+v", r)
	}
}

// The spawn dialog offers the host network only where it can be given.
func TestSettingsSayWhetherSandboxedAgentsHaveDocker(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	srv.SetSandboxEnabled(true)
	srv.SetSandboxDockerEnabled(true)
	var settings struct {
		SandboxDockerEnabled bool `json:"sandbox_docker_enabled"`
	}
	if status := serveJSON(t, srv, http.MethodGet, "/api/settings", "", &settings); status != http.StatusOK || !settings.SandboxDockerEnabled {
		t.Fatalf("status %d settings %+v", status, settings)
	}
}
