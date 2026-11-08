package server

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

type restartAgentFake struct {
	service.AgentService
	attached  []string
	restarted string
	resources []service.Resource
	err       error
	// switched is set by a restart that switched the sandbox; sandboxed is
	// where the agent then runs.
	switched  bool
	sandboxed bool
}

func (f *restartAgentFake) AttachedResources(context.Context, string) ([]string, error) {
	return f.attached, nil
}

func (f *restartAgentFake) RestartAgent(_ context.Context, taskID string, resources []service.Resource) error {
	if f.err != nil {
		return f.err
	}
	f.restarted, f.resources = taskID, resources
	return nil
}

func (f *restartAgentFake) RestartAgentSwitchingSandbox(_ context.Context, taskID string, resources []service.Resource) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	f.restarted, f.resources, f.switched = taskID, resources, true
	return f.sandboxed, nil
}

func restartServer(agents *restartAgentFake) *Server {
	srv := New(nil, agents, nil, ":0")
	srv.SetResourceService(service.NewResourceService(nil, []service.Resource{
		{Name: "Vault", Paths: []service.ResourcePath{{Path: "/home/me/Vault", Access: "write"}}},
	}, "/home/me"))
	return srv
}

func TestRestartingAnAgentKeepsTheResourcesItHas(t *testing.T) {
	// One the agent was given has been removed since; the restart goes on without it.
	agents := &restartAgentFake{attached: []string{"Vault", "Removed since"}}

	status := serveJSON(t, restartServer(agents), http.MethodPost, "/api/agents/T-1/restart", "", nil)

	if status != http.StatusOK || agents.restarted != "T-1" {
		t.Fatalf("status = %d, restarted %q", status, agents.restarted)
	}
	if len(agents.resources) != 1 || agents.resources[0].Name != "Vault" {
		t.Fatalf("resources = %+v, want Vault only", agents.resources)
	}
}

func TestAnAgentThatCannotBeRestartedIsAConflict(t *testing.T) {
	agents := &restartAgentFake{err: fmt.Errorf("%w: a swarm runs this agent", service.ErrAgentNotRestartable)}

	var failure struct{ Error string }
	status := serveJSON(t, restartServer(agents), http.MethodPost, "/api/agents/T-1/restart", "", &failure)

	if status != http.StatusConflict || failure.Error == "" {
		t.Fatalf("status = %d, body %+v; want 409 with the reason", status, failure)
	}
}

func TestRestartingCanSwitchAnAgentOutOfTheSandboxOrBackIn(t *testing.T) {
	agents := &restartAgentFake{attached: []string{"Vault"}, sandboxed: false}

	var answer struct {
		Restarted bool `json:"restarted"`
		Sandboxed bool `json:"sandboxed"`
	}
	status := serveJSON(t, restartServer(agents), http.MethodPost, "/api/agents/T-1/restart", `{"switch_sandbox": true}`, &answer)

	if status != http.StatusOK || !agents.switched || agents.restarted != "T-1" || !answer.Restarted || answer.Sandboxed {
		t.Fatalf("status = %d, switched = %v, answer %+v; want the agent switched, now out of the sandbox", status, agents.switched, answer)
	}
	if len(agents.resources) != 1 || agents.resources[0].Name != "Vault" {
		t.Fatalf("resources = %+v, want Vault kept", agents.resources)
	}

	// A plain restart leaves it where it was.
	agents = &restartAgentFake{}
	if status := serveJSON(t, restartServer(agents), http.MethodPost, "/api/agents/T-1/restart", "", nil); status != http.StatusOK || agents.switched {
		t.Fatalf("plain restart: status = %d, switched = %v", status, agents.switched)
	}
}
