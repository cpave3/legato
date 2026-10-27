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
