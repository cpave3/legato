package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

type taskAgentFake struct {
	service.AgentService
	live             bool
	defaultAdapter   string
	chimeraSessionID string
	spawned          string
	opts             service.AgentSpawnOptions
}

func (f *taskAgentFake) CaptureOutput(context.Context, string) (string, error) {
	if f.live {
		return "running", nil
	}
	return "", errors.New("not running")
}
func (f *taskAgentFake) SpawnAgent(_ context.Context, taskID string, _ int, _ int, options ...service.AgentSpawnOptions) error {
	f.spawned = taskID
	if len(options) > 0 {
		f.opts = options[0]
	}
	return nil
}
func (f *taskAgentFake) GetTaskChimeraSessionID(context.Context, string) (string, error) {
	return f.chimeraSessionID, nil
}
func (f *taskAgentFake) DefaultAdapter() string { return f.defaultAdapter }

func TestTaskAgentEndpointCreatesTaskBoundAgent(t *testing.T) {
	agents := &taskAgentFake{}
	board := &mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}
	server := New(board, agents, nil, ":0")
	server.workDir = "/default"
	request := httptest.NewRequest(http.MethodPost, "/api/tasks/T-1/agent", strings.NewReader(`{"agent_kind":"chimera","working_dir":"/repo"}`))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if agents.spawned != "T-1" || agents.opts.AgentKind != "chimera" || agents.opts.WorkingDir != "/repo" {
		t.Fatalf("spawned=%q opts=%+v", agents.spawned, agents.opts)
	}
	if agents.opts.ChimeraSessionID != "" || agents.opts.ChimeraSessionExists != "" {
		t.Fatalf("new Chimera agent received resume options: %+v", agents.opts)
	}
}

func TestTaskAgentEndpointResumesPersistedDefaultChimeraSession(t *testing.T) {
	agents := &taskAgentFake{defaultAdapter: "chimera", chimeraSessionID: "persisted-session"}
	board := &mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}
	server := New(board, agents, nil, ":0")
	request := httptest.NewRequest(http.MethodPost, "/api/tasks/T-1/agent", strings.NewReader(`{"working_dir":"/repo"}`))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if agents.opts.ChimeraSessionID != "persisted-session" || agents.opts.ChimeraSessionExists != "resume" {
		t.Fatalf("resume options=%+v", agents.opts)
	}
}

func TestTaskAgentEndpointResumesPersistedChimeraSession(t *testing.T) {
	agents := &taskAgentFake{chimeraSessionID: "persisted-session"}
	board := &mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}
	server := New(board, agents, nil, ":0")
	request := httptest.NewRequest(http.MethodPost, "/api/tasks/T-1/agent", strings.NewReader(`{"agent_kind":"chimera","working_dir":"/repo"}`))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if agents.opts.ChimeraSessionID != "persisted-session" || agents.opts.ChimeraSessionExists != "resume" {
		t.Fatalf("resume options=%+v", agents.opts)
	}
}

func TestTaskAgentEndpointResumesLiveAgentWithoutSpawning(t *testing.T) {
	agents := &taskAgentFake{live: true}
	board := &mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}
	server := New(board, agents, nil, ":0")
	request := httptest.NewRequest(http.MethodPost, "/api/tasks/T-1/agent", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if agents.spawned != "" {
		t.Fatalf("spawned=%q", agents.spawned)
	}
}
