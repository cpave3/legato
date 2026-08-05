package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type chimeraDiscoveryAgentFake struct {
	service.AgentService
	session     *store.AgentSession
	sessionErr  error
	discoverErr error
	discovered  string
}

func (f *chimeraDiscoveryAgentFake) GetAgentSession(context.Context, string) (*store.AgentSession, error) {
	return f.session, f.sessionErr
}

func (f *chimeraDiscoveryAgentFake) DiscoverChimera(_ context.Context, taskID string) error {
	f.discovered = taskID
	return f.discoverErr
}

func TestChimeraDiscoverEndpointUpgradesActiveChimeraAgent(t *testing.T) {
	agents := &chimeraDiscoveryAgentFake{session: &store.AgentSession{TaskID: "TASK-42", AgentKind: "chimera", Status: "running"}}
	server := New(nil, agents, nil, ":0")
	request := httptest.NewRequest(http.MethodPost, "/api/agents/TASK-42/chimera/discover", nil)
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if agents.discovered != "TASK-42" {
		t.Fatalf("discovered task = %q", agents.discovered)
	}
	if response.Body.String() != "{\"task_id\":\"TASK-42\",\"upgraded\":true}\n" {
		t.Fatalf("body=%s", response.Body.String())
	}
}

func TestChimeraDiscoverEndpointRejectsInvalidTargetsAndDiscoveryFailures(t *testing.T) {
	endpoint := "http://127.0.0.1:1234"
	tests := []struct {
		name       string
		agents     *chimeraDiscoveryAgentFake
		wantStatus int
	}{
		{name: "missing active agent", agents: &chimeraDiscoveryAgentFake{sessionErr: store.ErrNotFound}, wantStatus: http.StatusNotFound},
		{name: "non-Chimera agent", agents: &chimeraDiscoveryAgentFake{session: &store.AgentSession{AgentKind: "shell", Status: "running"}}, wantStatus: http.StatusConflict},
		{name: "inactive agent", agents: &chimeraDiscoveryAgentFake{session: &store.AgentSession{AgentKind: "chimera", Status: "dead"}}, wantStatus: http.StatusConflict},
		{name: "already native", agents: &chimeraDiscoveryAgentFake{session: &store.AgentSession{AgentKind: "chimera", Status: "running", ChimeraEndpoint: &endpoint}}, wantStatus: http.StatusConflict},
		{name: "no verified instance", agents: &chimeraDiscoveryAgentFake{session: &store.AgentSession{AgentKind: "chimera", Status: "running"}, discoverErr: service.ErrChimeraDiscoveryNotFound}, wantStatus: http.StatusNotFound},
		{name: "ambiguous instances", agents: &chimeraDiscoveryAgentFake{session: &store.AgentSession{AgentKind: "chimera", Status: "running"}, discoverErr: service.ErrChimeraDiscoveryAmbiguous}, wantStatus: http.StatusConflict},
		{name: "persistence failure", agents: &chimeraDiscoveryAgentFake{session: &store.AgentSession{AgentKind: "chimera", Status: "running"}, discoverErr: errors.New("database unavailable")}, wantStatus: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := New(nil, tt.agents, nil, ":0")
			request := httptest.NewRequest(http.MethodPost, "/api/agents/TASK-42/chimera/discover", nil)
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			if response.Code != tt.wantStatus {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
