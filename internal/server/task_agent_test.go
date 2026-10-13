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
	nativeSessions   map[string]*service.TaskResumeSession
	spawnErr         error
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
	return f.spawnErr
}
func (f *taskAgentFake) GetTaskResumeSession(_ context.Context, _ string, kind string) (*service.TaskResumeSession, error) {
	if saved := f.nativeSessions[kind]; saved != nil {
		return saved, nil
	}
	if kind == "chimera" && f.chimeraSessionID != "" {
		return &service.TaskResumeSession{ID: f.chimeraSessionID}, nil
	}
	return nil, nil
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
	if agents.opts.SessionPolicy != "" {
		t.Fatalf("new Chimera agent received resume policy: %+v", agents.opts)
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
	if agents.opts.SessionPolicy != "resume" {
		t.Fatalf("resume options=%+v", agents.opts)
	}
	if agents.opts.ResumeSessionID != "persisted-session" {
		t.Fatalf("ResumeSessionID = %q, want persisted-session", agents.opts.ResumeSessionID)
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
	if agents.opts.SessionPolicy != "resume" {
		t.Fatalf("resume options=%+v", agents.opts)
	}
	if agents.opts.ResumeSessionID != "persisted-session" {
		t.Fatalf("ResumeSessionID = %q, want persisted-session", agents.opts.ResumeSessionID)
	}
}

func TestTaskAgentEndpointExplicitlyResumesPersistedChimeraSession(t *testing.T) {
	agents := &taskAgentFake{chimeraSessionID: "persisted-session"}
	board := &mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}
	server := New(board, agents, nil, ":0")
	request := httptest.NewRequest(http.MethodPost, "/api/tasks/T-1/agent", strings.NewReader(`{"agent_kind":"chimera","session_policy":"resume"}`))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if agents.opts.SessionPolicy != "resume" {
		t.Fatalf("resume options=%+v", agents.opts)
	}
}

func TestTaskAgentEndpointStartsNewPersistedChimeraSession(t *testing.T) {
	agents := &taskAgentFake{chimeraSessionID: "persisted-session"}
	board := &mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}
	server := New(board, agents, nil, ":0")
	request := httptest.NewRequest(http.MethodPost, "/api/tasks/T-1/agent", strings.NewReader(`{"agent_kind":"chimera","session_policy":"new"}`))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if agents.opts.SessionPolicy != "new" {
		t.Fatalf("new session options=%+v", agents.opts)
	}
}

func TestTaskAgentEndpointRejectsStaleChimeraSessionChoice(t *testing.T) {
	for _, policy := range []string{"resume", "new"} {
		t.Run(policy, func(t *testing.T) {
			agents := &taskAgentFake{}
			board := &mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}
			server := New(board, agents, nil, ":0")
			request := httptest.NewRequest(http.MethodPost, "/api/tasks/T-1/agent", strings.NewReader(`{"agent_kind":"chimera","session_policy":"`+policy+`"}`))
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			if response.Code != http.StatusConflict {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if agents.spawned != "" {
				t.Fatalf("spawned=%q", agents.spawned)
			}
		})
	}
}

func TestTaskAgentEndpointRejectsInvalidChimeraSessionPolicy(t *testing.T) {
	agents := &taskAgentFake{chimeraSessionID: "persisted-session"}
	board := &mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}
	server := New(board, agents, nil, ":0")
	request := httptest.NewRequest(http.MethodPost, "/api/tasks/T-1/agent", strings.NewReader(`{"agent_kind":"chimera","session_policy":"replace"}`))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if agents.spawned != "" {
		t.Fatalf("spawned=%q", agents.spawned)
	}
}

func TestTaskAgentEndpointRejectsChimeraSessionPolicyForOtherAdapter(t *testing.T) {
	agents := &taskAgentFake{defaultAdapter: "claude"}
	board := &mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}
	server := New(board, agents, nil, ":0")
	request := httptest.NewRequest(http.MethodPost, "/api/tasks/T-1/agent", strings.NewReader(`{"agent_kind":"claude","session_policy":"resume"}`))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if agents.spawned != "" {
		t.Fatalf("spawned=%q", agents.spawned)
	}
}

func TestTaskAgentEndpointResumesLiveAgentWithoutSpawning(t *testing.T) {
	agents := &taskAgentFake{live: true}
	board := &mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}
	server := New(board, agents, nil, ":0")
	request := httptest.NewRequest(http.MethodPost, "/api/tasks/T-1/agent", strings.NewReader(`{"session_policy":"invalid"}`))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if agents.spawned != "" {
		t.Fatalf("spawned=%q", agents.spawned)
	}
}
