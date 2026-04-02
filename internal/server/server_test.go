package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type mockBoardService struct {
	columns []service.Column
	cards   map[string][]service.Card
}

func (m *mockBoardService) ListColumns(_ context.Context) ([]service.Column, error) {
	return m.columns, nil
}

func (m *mockBoardService) ListCards(_ context.Context, column string) ([]service.Card, error) {
	return m.cards[column], nil
}

func (m *mockBoardService) GetCard(_ context.Context, _ string) (*service.CardDetail, error) {
	return nil, nil
}
func (m *mockBoardService) MoveCard(_ context.Context, _ string, _ string) error { return nil }
func (m *mockBoardService) ReorderCard(_ context.Context, _ string, _ int) error  { return nil }
func (m *mockBoardService) SearchCards(_ context.Context, _ string) ([]service.Card, error) {
	return nil, nil
}
func (m *mockBoardService) ExportCardContext(_ context.Context, _ string, _ service.ExportFormat) (string, error) {
	return "", nil
}
func (m *mockBoardService) DeleteTask(_ context.Context, _ string) error { return nil }
func (m *mockBoardService) CreateTask(_ context.Context, _, _, _, _ string, _ *int) (*service.Card, error) {
	return nil, nil
}
func (m *mockBoardService) UpdateTaskDescription(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockBoardService) UpdateTaskTitle(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockBoardService) ListCardsByWorkspace(_ context.Context, column string, _ store.WorkspaceView) ([]service.Card, error) {
	return m.ListCards(context.Background(), column)
}
func (m *mockBoardService) UpdateTaskWorkspace(_ context.Context, _ string, _ *int) error {
	return nil
}
func (m *mockBoardService) ListWorkspaces(_ context.Context) ([]service.Workspace, error) {
	return nil, nil
}
func (m *mockBoardService) ArchiveDoneCards(_ context.Context) (int, error) { return 0, nil }
func (m *mockBoardService) ArchiveTask(_ context.Context, _ string) error   { return nil }
func (m *mockBoardService) CountDoneCards(_ context.Context) (int, error)   { return 0, nil }

func TestHealthEndpointReturnsOK(t *testing.T) {
	svc := &mockBoardService{
		columns: []service.Column{
			{Name: "Backlog", SortOrder: 0},
			{Name: "Doing", SortOrder: 1},
		},
		cards: map[string][]service.Card{
			"Backlog": {
				{ID: "REX-1", Title: "First", Status: "Backlog"},
				{ID: "REX-2", Title: "Second", Status: "Backlog"},
			},
			"Doing": {
				{ID: "REX-3", Title: "In progress", Status: "Doing"},
			},
		},
	}

	srv := New(svc, nil, nil, ":0")
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Status != "ok" {
		t.Errorf("status = %q, want ok", resp.Status)
	}
	if len(resp.Columns) != 2 {
		t.Fatalf("columns = %d, want 2", len(resp.Columns))
	}
	if resp.Columns[0].Name != "Backlog" {
		t.Errorf("column[0] = %q, want Backlog", resp.Columns[0].Name)
	}
	if len(resp.Columns[0].Cards) != 2 {
		t.Errorf("backlog cards = %d, want 2", len(resp.Columns[0].Cards))
	}
	if resp.Columns[1].Name != "Doing" {
		t.Errorf("column[1] = %q, want Doing", resp.Columns[1].Name)
	}
}

func TestHealthEndpointEmptyBoard(t *testing.T) {
	svc := &mockBoardService{
		columns: []service.Column{
			{Name: "Backlog", SortOrder: 0},
		},
		cards: map[string][]service.Card{
			"Backlog": {},
		},
	}

	srv := New(svc, nil, nil, ":0")
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp HealthResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Status != "ok" {
		t.Errorf("status = %q, want ok", resp.Status)
	}
	if len(resp.Columns[0].Cards) != 0 {
		t.Errorf("should have 0 cards, got %d", len(resp.Columns[0].Cards))
	}
}

func TestAgentsEndpointNoService(t *testing.T) {
	svc := &mockBoardService{
		columns: []service.Column{},
		cards:   map[string][]service.Card{},
	}

	srv := New(svc, nil, nil, ":0")
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/api/agents", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp []AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("got %d agents, want 0", len(resp))
	}
}

func TestTasksEndpoint(t *testing.T) {
	svc := &mockBoardService{
		columns: []service.Column{
			{Name: "Backlog", SortOrder: 0},
			{Name: "Doing", SortOrder: 1},
		},
		cards: map[string][]service.Card{
			"Backlog": {
				{ID: "t1", Title: "Task one", Status: "Backlog"},
			},
			"Doing": {},
		},
	}

	srv := New(svc, nil, nil, ":0")
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/api/tasks", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp TasksResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp["Backlog"]) != 1 {
		t.Errorf("backlog cards = %d, want 1", len(resp["Backlog"]))
	}
	if len(resp["Doing"]) != 0 {
		t.Errorf("doing cards = %d, want 0", len(resp["Doing"]))
	}
}

func TestServerStartAndStop(t *testing.T) {
	svc := &mockBoardService{
		columns: []service.Column{},
		cards:   map[string][]service.Card{},
	}

	srv := New(svc, nil, nil, "127.0.0.1:0")
	go func() { _ = srv.Start() }()
	time.Sleep(50 * time.Millisecond)
	if err := srv.Stop(context.Background()); err != nil {
		t.Errorf("stop: %v", err)
	}
}

func TestFindIncompleteEscape(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  int // expected split index
	}{
		{"empty", []byte{}, 0},
		{"no escape", []byte("hello world"), 11},
		{"complete CSI", []byte("hello\x1b[31mred"), 13},
		{"bare ESC at end", []byte("hello\x1b"), 5},
		{"incomplete CSI", []byte("hello\x1b[38;2;100"), 5},
		{"ESC[ at end", []byte("hello\x1b["), 5},
		{"incomplete OSC", []byte("hello\x1b]0;title"), 5},
		{"complete OSC with BEL", []byte("hello\x1b]0;title\x07rest"), 19},
		{"two-byte escape complete", []byte("hello\x1b="), 7},
		{"complete CSI then text", []byte("\x1b[2Jhello"), 9},
		{"multiple CSI last incomplete", []byte("\x1b[2Jhello\x1b[38;2"), 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findIncompleteEscape(tt.input)
			if got != tt.want {
				t.Errorf("findIncompleteEscape(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// mockTmuxManager implements service.TmuxManager for server-level tests.
type mockTmuxManager struct {
	sentKeys []string
}

func (m *mockTmuxManager) Spawn(name, workDir string, width, height int, envVars ...string) error {
	return nil
}
func (m *mockTmuxManager) Kill(name string) error                       { return nil }
func (m *mockTmuxManager) Capture(name string) (string, error)          { return "", nil }
func (m *mockTmuxManager) CaptureWithEscapes(name string) (string, error) { return "", nil }
func (m *mockTmuxManager) Attach(name string) *exec.Cmd                 { return exec.Command("echo") }
func (m *mockTmuxManager) ListSessions() ([]string, error)              { return nil, nil }
func (m *mockTmuxManager) IsAlive(name string) (bool, error)            { return false, nil }
func (m *mockTmuxManager) SendKeys(name, keys string) error             { return nil }
func (m *mockTmuxManager) SendKey(name, key string) error {
	m.sentKeys = append(m.sentKeys, key)
	return nil
}
func (m *mockTmuxManager) PipeOutput(name string) (io.Reader, func(), error) {
	return strings.NewReader(""), func() {}, nil
}
func (m *mockTmuxManager) SetEnv(sessionName, key, value string) error    { return nil }
func (m *mockTmuxManager) SetOption(sessionName, key, value string) error { return nil }
func (m *mockTmuxManager) PaneCommands() (map[string]string, error)       { return nil, nil }

func TestHandleSendKeysNamedSequence(t *testing.T) {
	mock := &mockTmuxManager{}
	svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(svc, nil, mock, ":0")

	// handleSendKeys doesn't send responses on the success path,
	// so we only need a client with a valid context.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &wsClient{ctx: ctx, cancel: cancel}

	srv.handleSendKeys(client, WSMessage{
		AgentID: "test-agent",
		Keys:    "Down Down Enter",
	})

	want := []string{"Down", "Down", "Enter"}
	if len(mock.sentKeys) != len(want) {
		t.Fatalf("got %d SendKey calls, want %d", len(mock.sentKeys), len(want))
	}
	for i, k := range want {
		if mock.sentKeys[i] != k {
			t.Errorf("sentKeys[%d] = %q, want %q", i, mock.sentKeys[i], k)
		}
	}
}

func TestStreamSubscribeUnsubscribe(t *testing.T) {
	mock := &mockTmuxManager{}
	sm := newStreamManager(mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &wsClient{ctx: ctx, cancel: cancel}

	sm.subscribe("agent-1", client)

	sm.mu.Lock()
	_, exists := sm.streams["agent-1"]
	sm.mu.Unlock()
	if !exists {
		t.Fatal("stream should exist after subscribe")
	}

	sm.unsubscribe("agent-1", client)

	sm.mu.Lock()
	_, exists = sm.streams["agent-1"]
	sm.mu.Unlock()
	if exists {
		t.Fatal("stream should be removed after last client unsubscribes")
	}
}

// failPipeMock is a mockTmuxManager where PipeOutput always fails.
type failPipeMock struct {
	mockTmuxManager
}

func (m *failPipeMock) PipeOutput(name string) (io.Reader, func(), error) {
	return nil, nil, fmt.Errorf("pipe failed")
}

func TestStartPipeResetsOnFailure(t *testing.T) {
	mock := &failPipeMock{}
	sm := newStreamManager(mock)

	s := &agentStream{
		clients: make(map[*wsClient]struct{}),
		sizes:   make(map[*wsClient]*clientSize),
		agentID: "agent-1",
		cancel:  make(chan struct{}),
	}

	sm.startPipe(s)

	s.mu.Lock()
	piping := s.piping
	s.mu.Unlock()

	if piping {
		t.Error("piping should be false after PipeOutput failure")
	}
}

func TestStartPipeAbortOnCancel(t *testing.T) {
	mock := &mockTmuxManager{}
	sm := newStreamManager(mock)

	s := &agentStream{
		clients: make(map[*wsClient]struct{}),
		sizes:   make(map[*wsClient]*clientSize),
		agentID: "agent-1",
		cancel:  make(chan struct{}),
	}

	// Close cancel before calling startPipe so it aborts after sleep.
	close(s.cancel)

	sm.startPipe(s)

	s.mu.Lock()
	piping := s.piping
	s.mu.Unlock()

	if piping {
		t.Error("piping should be false after cancel")
	}
}
