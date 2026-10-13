package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/github"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
	"nhooyr.io/websocket"
)

func intPtr(value int) *int { return &value }

type mockBoardService struct {
	columns          []service.Column
	cards            map[string][]service.Card
	workspaceCards   map[string][]service.Card
	workspaces       []service.Workspace
	cardDetail       *service.CardDetail
	cardErr          error
	searchResults    []service.Card
	createdCard      *service.Card
	createdWorkspace *int
	archiveCount     int
	archivedCards    []service.Card
	listColumnsCalls int
	listCardsCalls   int

	// call-tracking fields for patch verification
	updatedTitle           string
	updatedDesc            string
	updatedWorkspace       *int
	updatedWorkspaceCalled bool
	movedTo                string
	moveErr                error
}

func (m *mockBoardService) ListColumns(_ context.Context) ([]service.Column, error) {
	m.listColumnsCalls++
	return m.columns, nil
}

func (m *mockBoardService) ListCards(_ context.Context, column string) ([]service.Card, error) {
	m.listCardsCalls++
	return m.cards[column], nil
}

func (m *mockBoardService) ListCardsByWorkspace(_ context.Context, column string, view store.WorkspaceView) ([]service.Card, error) {
	if m.workspaceCards != nil {
		return m.workspaceCards[column], nil
	}
	return m.ListCards(context.Background(), column)
}

func (m *mockBoardService) GetCard(_ context.Context, id string) (*service.CardDetail, error) {
	if m.cardErr != nil {
		return nil, m.cardErr
	}
	if m.cardDetail == nil {
		return nil, store.ErrNotFound
	}
	// Return nil for unknown IDs unless it's our test card
	if m.cardDetail.ID != "" && m.cardDetail.ID != id {
		return nil, store.ErrNotFound
	}
	return m.cardDetail, nil
}
func (m *mockBoardService) MoveCard(_ context.Context, _ string, col string) error {
	m.movedTo = col
	return m.moveErr
}
func (m *mockBoardService) ReorderCard(_ context.Context, _ string, _ int) error { return nil }
func (m *mockBoardService) SearchCards(_ context.Context, query string) ([]service.Card, error) {
	return m.searchResults, nil
}
func (m *mockBoardService) ExportCardContext(_ context.Context, _ string, _ service.ExportFormat) (string, error) {
	return "exported context", nil
}
func (m *mockBoardService) DeleteTask(_ context.Context, id string) error {
	if m.cardDetail == nil || m.cardDetail.ID != id {
		return store.ErrNotFound
	}
	return nil
}
func (m *mockBoardService) CreateTask(_ context.Context, _, _, _, _ string, workspaceID *int) (*service.Card, error) {
	m.createdWorkspace = workspaceID
	if m.createdCard != nil {
		return m.createdCard, nil
	}
	return &service.Card{ID: "new-1", Title: "New task", Status: "Backlog"}, nil
}
func (m *mockBoardService) UpdateTaskDescription(_ context.Context, _, desc string) error {
	m.updatedDesc = desc
	return nil
}
func (m *mockBoardService) UpdateTaskTitle(_ context.Context, _, title string) error {
	m.updatedTitle = title
	return nil
}
func (m *mockBoardService) UpdateTaskWorkspace(_ context.Context, _ string, ws *int) error {
	m.updatedWorkspaceCalled = true
	m.updatedWorkspace = ws
	return nil
}
func (m *mockBoardService) ListWorkspaces(_ context.Context) ([]service.Workspace, error) {
	return m.workspaces, nil
}
func (m *mockBoardService) ArchiveDoneCards(_ context.Context) (int, error) {
	return m.archiveCount, nil
}
func (m *mockBoardService) ArchiveTask(_ context.Context, id string) error {
	if m.cardDetail == nil || m.cardDetail.ID != id {
		return store.ErrNotFound
	}
	return nil
}
func (m *mockBoardService) ListArchivedTasks(_ context.Context) ([]service.Card, error) {
	return m.archivedCards, nil
}
func (m *mockBoardService) RestoreTask(_ context.Context, _ string) error { return nil }
func (m *mockBoardService) CountDoneCards(_ context.Context) (int, error) { return 0, nil }

func TestSettingsEndpointReportsWorktreeCapability(t *testing.T) {
	s := New(nil, nil, nil, "")
	s.SetWorktreeService(&worktreeServiceFake{})
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	var response struct {
		WorktreesEnabled bool `json:"worktrees_enabled"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if !response.WorktreesEnabled {
		t.Fatal("worktree capability not reported")
	}
}

func TestSettingsEndpointReportsHotReloadBuildCapability(t *testing.T) {
	s := New(nil, nil, nil, "")
	request := httptest.NewRequest(http.MethodGet, "/api/settings", nil)

	response := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(response, request)
	var disabled struct {
		HotReloadEnabled bool `json:"hot_reload_enabled"`
	}
	if err := json.NewDecoder(response.Body).Decode(&disabled); err != nil {
		t.Fatal(err)
	}
	if disabled.HotReloadEnabled {
		t.Fatal("hot reload build capability reported without a configured trigger")
	}

	s.SetHotReloadBuildTrigger(func(<-chan struct{}) (bool, error) { return true, nil })
	response = httptest.NewRecorder()
	s.server.Handler.ServeHTTP(response, request)
	var enabled struct {
		HotReloadEnabled bool `json:"hot_reload_enabled"`
	}
	if err := json.NewDecoder(response.Body).Decode(&enabled); err != nil {
		t.Fatal(err)
	}
	if !enabled.HotReloadEnabled {
		t.Fatal("configured hot reload build capability not reported")
	}
}

func TestHotReloadBuildEndpointStartsConfiguredBuild(t *testing.T) {
	s := New(nil, nil, nil, "")
	calls := 0
	released := make(chan struct{}, 1)
	s.SetHotReloadBuildTrigger(func(start <-chan struct{}) (bool, error) {
		calls++
		go func() {
			<-start
			released <- struct{}{}
		}()
		return true, nil
	})

	response := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/hot-reload/build", nil))

	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if calls != 1 {
		t.Fatalf("build trigger calls=%d", calls)
	}
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("build was not released after the response was written")
	}
}

func TestHotReloadBuildEndpointRejectsUnavailableAndRunningBuilds(t *testing.T) {
	s := New(nil, nil, nil, "")
	response := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/hot-reload/build", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("unconfigured status=%d", response.Code)
	}

	s.SetHotReloadBuildTrigger(func(<-chan struct{}) (bool, error) { return false, nil })
	response = httptest.NewRecorder()
	s.server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/hot-reload/build", nil))
	if response.Code != http.StatusConflict {
		t.Fatalf("running status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestHotReloadBuildEndpointReportsTriggerErrorsAndRejectsOtherMethods(t *testing.T) {
	s := New(nil, nil, nil, "")
	s.SetHotReloadBuildTrigger(func(<-chan struct{}) (bool, error) { return false, errors.New("cannot prepare build") })

	response := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/hot-reload/build", nil))
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "cannot prepare build") {
		t.Fatalf("trigger error status=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	s.server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/hot-reload/build", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method status=%d", response.Code)
	}
}

func TestSettingsEndpointIncludesRepositoryPresets(t *testing.T) {
	s := New(nil, nil, nil, "")
	s.SetWorkflowService(nil, "", "", workflowengine.RepositoryPreset{
		Name:       "Legato",
		Repo:       "cpave3/legato",
		PrimaryDir: "/projects/legato",
		BaseBranch: "main",
	})
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Repositories []workflowengine.RepositoryPreset `json:"repositories"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if len(response.Repositories) != 1 || response.Repositories[0].Name != "Legato" || response.Repositories[0].PrimaryDir != "/projects/legato" {
		t.Fatalf("repositories=%+v", response.Repositories)
	}
}

func TestAgentMessageQueuesThroughServiceWhenRequested(t *testing.T) {
	agents := &promptAgentService{}
	s := New(nil, agents, nil, "")
	client := &wsClient{ctx: context.Background(), sendFn: func(WSMessage) {}}

	s.handleWSMessage(client, WSMessage{
		Type:    MsgAgentMessage,
		AgentID: "task-1",
		Content: "Follow this up next",
		Queue:   true,
	})

	if agents.queuedTaskID != "task-1" || agents.queuedMessage != "Follow this up next" {
		t.Fatalf("queued task=%q message=%q", agents.queuedTaskID, agents.queuedMessage)
	}
}

func TestArchivedTasksEndpointUsesBoardCardJSONShape(t *testing.T) {
	board := &mockBoardService{archivedCards: []service.Card{{ID: "LEG-44", Title: "Archived task", Status: "Done", WorkspaceName: "Platform", WorkspaceColor: "#123456"}}}
	s := New(board, nil, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/archived", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"id":"LEG-44"`) || !strings.Contains(body, `"workspace_name":"Platform"`) || strings.Contains(body, `"ID"`) {
		t.Fatalf("body=%s", body)
	}
}

type mockSyncService struct {
	searchResults     []service.RemoteSearchResult
	importedCard      *service.Card
	importedWorkspace *int
	movedTaskID       string
	movedTo           string
}

func (m *mockSyncService) Sync(_ context.Context) (*service.SyncResult, error) { return nil, nil }
func (m *mockSyncService) Status() service.SyncStatus                          { return service.SyncStatus{} }

type promptAgentService struct {
	agent            service.AgentSession
	setActivities    []string
	setWorkingDirs   []string
	queuedTaskID     string
	queuedMessage    string
	chimeraEndpoint  string
	chimeraSessionID string
	agentEnvironment []string
	permissionTaskID string
	permissionReqID  string
	permissionAction service.PermissionAction
	permissionErr    error
	questionTaskID   string
	questionReqID    string
	questionAnswer   service.QuestionAnswer
	questionErr      error
	captureOutput    string
	captureErr       error
	capturedTaskID   string
	killedTaskID     string
}

func (p *promptAgentService) SpawnAgent(context.Context, string, int, int, ...service.AgentSpawnOptions) error {
	return nil
}
func (p *promptAgentService) KillAgent(_ context.Context, taskID string) error {
	p.killedTaskID = taskID
	return nil
}
func (p *promptAgentService) SetTaskGroup(context.Context, string, *string) error { return nil }
func (p *promptAgentService) ListAgents(context.Context) ([]service.AgentSession, error) {
	return []service.AgentSession{p.agent}, nil
}
func (p *promptAgentService) ListAgentsByParent(context.Context, string) ([]service.AgentSession, error) {
	return nil, nil
}
func (p *promptAgentService) ReconcileSessions(context.Context) error { return nil }
func (p *promptAgentService) CaptureOutput(_ context.Context, taskID string) (string, error) {
	p.capturedTaskID = taskID
	return p.captureOutput, p.captureErr
}
func (p *promptAgentService) AttachCmd(context.Context, string) (*exec.Cmd, error) {
	return nil, nil
}
func (p *promptAgentService) GetTaskDurations(context.Context, []string) (map[string]service.DurationData, error) {
	return nil, nil
}
func (p *promptAgentService) GetAgentSummary(context.Context, string) (int, int, int, int, error) {
	return 0, 0, 0, 0, nil
}
func (p *promptAgentService) SetAgentActivity(_ context.Context, _ string, activity, workingDir string) error {
	p.setActivities = append(p.setActivities, activity)
	p.setWorkingDirs = append(p.setWorkingDirs, workingDir)
	p.agent.Activity = activity
	return nil
}
func (p *promptAgentService) SpawnEphemeralAgent(context.Context, string, int, int, ...service.AgentSpawnOptions) error {
	return nil
}
func (p *promptAgentService) LastSpawnConflicts() []service.AgentSpawnConflict { return nil }
func (p *promptAgentService) RegisteredAdapters() []string                     { return nil }
func (p *promptAgentService) DefaultAdapter() string                           { return "" }
func (p *promptAgentService) AdapterFor(string) service.AIToolAdapter          { return nil }
func (p *promptAgentService) GetStateTimeline(context.Context, string, time.Duration, int) ([]string, error) {
	return nil, nil
}
func (p *promptAgentService) SetTaskNotifyEnabled(context.Context, string, bool) error { return nil }
func (p *promptAgentService) GetTaskNotifyEnabled(context.Context, string) (bool, error) {
	return false, nil
}
func (p *promptAgentService) DiscoverChimera(context.Context, string) error { return nil }
func (p *promptAgentService) GetTaskResumeSession(context.Context, string, string) (*service.TaskResumeSession, error) {
	return nil, nil
}
func (p *promptAgentService) GetAgentEnvironment(context.Context, string) ([]string, error) {
	return p.agentEnvironment, nil
}
func (p *promptAgentService) ListPeers(context.Context, string, string, string) ([]service.AgentPeer, error) {
	return nil, nil
}
func (p *promptAgentService) SendPeerMessage(context.Context, string, string, string) error {
	return nil
}
func (p *promptAgentService) BroadcastToPeers(context.Context, string, string, string, string) (service.AgentBroadcastResult, error) {
	return service.AgentBroadcastResult{}, nil
}
func (p *promptAgentService) SendMessage(context.Context, string, string) error {
	return nil
}
func (p *promptAgentService) QueueMessage(_ context.Context, taskID, message string) error {
	p.queuedTaskID = taskID
	p.queuedMessage = message
	return nil
}
func (p *promptAgentService) InterruptAgent(context.Context, string) error {
	return nil
}
func (p *promptAgentService) ResolvePermission(_ context.Context, taskID, requestID string, action service.PermissionAction) error {
	p.permissionTaskID = taskID
	p.permissionReqID = requestID
	p.permissionAction = action
	return p.permissionErr
}
func (p *promptAgentService) AnswerQuestion(_ context.Context, taskID, requestID string, answer service.QuestionAnswer) error {
	p.questionTaskID = taskID
	p.questionReqID = requestID
	p.questionAnswer = answer
	return p.questionErr
}
func (p *promptAgentService) HasChimeraEndpoint(context.Context, string) bool {
	return p.chimeraEndpoint != ""
}
func (p *promptAgentService) GetAgentSession(context.Context, string) (*store.AgentSession, error) {
	if p.chimeraEndpoint == "" {
		return nil, fmt.Errorf("not found")
	}
	ep := p.chimeraEndpoint
	sid := p.chimeraSessionID
	return &store.AgentSession{ChimeraEndpoint: &ep, ChimeraSessionID: &sid}, nil
}
func (m *mockSyncService) Subscribe() <-chan service.SyncEvent     { return nil }
func (m *mockSyncService) StartScheduler(_ context.Context) func() { return func() {} }
func (m *mockSyncService) SearchRemote(_ context.Context, _ string) ([]service.RemoteSearchResult, error) {
	return m.searchResults, nil
}
func (m *mockSyncService) ImportRemoteTask(_ context.Context, _ string, workspaceID *int) (*service.Card, error) {
	m.importedWorkspace = workspaceID
	return m.importedCard, nil
}
func (m *mockSyncService) PushMove(_ context.Context, taskID, targetColumn string) error {
	m.movedTaskID = taskID
	m.movedTo = targetColumn
	return nil
}

type mockPRTaskActions struct {
	mockPRTrackingService
	contextTaskID string
	context       *store.CodeContext
	refreshed     string
	rediscovered  string
}

func (m *mockPRTaskActions) SetCodeContext(_ context.Context, taskID string, value *store.CodeContext) error {
	m.contextTaskID, m.context = taskID, value
	return nil
}
func (m *mockPRTaskActions) GetCodeContext(context.Context, string) (*store.CodeContext, error) {
	return m.context, nil
}
func (m *mockPRTaskActions) RefreshPR(_ context.Context, taskID string) error {
	m.refreshed = taskID
	return nil
}
func (m *mockPRTaskActions) RediscoverPR(_ context.Context, taskID string) error {
	m.rediscovered = taskID
	return nil
}

type mockPRTrackingService struct {
	linkedTaskID string
	linkPRFunc   func(ctx context.Context, taskID, owner, repo string, prNumber int) error
	fetchPRFunc  func(owner, repo string, prNumber int) (*github.PRStatus, error)
}

func (m *mockPRTrackingService) LinkBranch(_ context.Context, taskID, branch, repo string) error {
	return nil
}
func (m *mockPRTrackingService) LinkPR(_ context.Context, taskID, owner, repo string, prNumber int) error {
	m.linkedTaskID = taskID
	if m.linkPRFunc != nil {
		return m.linkPRFunc(context.Background(), taskID, owner, repo, prNumber)
	}
	return nil
}
func (m *mockPRTrackingService) UnlinkBranch(_ context.Context, _ string) error { return nil }
func (m *mockPRTrackingService) PollOnce(_ context.Context) error               { return nil }
func (m *mockPRTrackingService) PollAll(_ context.Context) error                { return nil }
func (m *mockPRTrackingService) StartPolling(_ context.Context) func()          { return func() {} }
func (m *mockPRTrackingService) GetPRStatus(_ context.Context, _ string) (*store.PRMeta, error) {
	return nil, nil
}
func (m *mockPRTrackingService) DetectRepo() (owner, repo string, err error) { return "", "", nil }
func (m *mockPRTrackingService) FetchPRByNumber(owner, repo string, prNumber int) (*github.PRStatus, error) {
	if m.fetchPRFunc != nil {
		return m.fetchPRFunc(owner, repo, prNumber)
	}
	return &github.PRStatus{Number: prNumber}, nil
}

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
	if resp.Version == "" {
		t.Error("version should not be empty")
	}
	if svc.listColumnsCalls != 0 || svc.listCardsCalls != 0 {
		t.Errorf("health queried board: ListColumns=%d, ListCards=%d", svc.listColumnsCalls, svc.listCardsCalls)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &fields); err != nil {
		t.Fatalf("unmarshal fields: %v", err)
	}
	if len(fields) != 2 || fields["status"] == nil || fields["version"] == nil {
		t.Errorf("health fields = %v, want only status and version", fields)
	}
	for _, secret := range []string{"REX-1", "First", "Backlog", "Doing"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Errorf("health response leaked board value %q", secret)
		}
	}
}

func TestAgentResponsesExposeAttributionForHTTPAndWebsocket(t *testing.T) {
	agents := &promptAgentService{agent: service.AgentSession{
		ID: 7, TaskID: "workflow-task", ExecutionID: "exec-1", AttributionTaskID: "user-task",
		ExecutionKind: store.ExecutionWorkflowAgent, WorkflowRunID: "run-1", Status: "running",
	}}
	srv := New(&mockBoardService{}, agents, nil, ":0")

	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var httpAgents []AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &httpAgents); err != nil {
		t.Fatal(err)
	}
	assertAgentAttribution := func(source string, got AgentResponse) {
		t.Helper()
		if got.TaskID != "workflow-task" || got.ExecutionID != "exec-1" || got.AttributionTaskID != "user-task" || got.ExecutionKind != store.ExecutionWorkflowAgent || got.WorkflowRunID != "run-1" {
			t.Fatalf("%s agent = %#v", source, got)
		}
	}
	assertAgentAttribution("http", httpAgents[0])

	var message WSMessage
	srv.sendAgentList(&wsClient{sendFn: func(got WSMessage) { message = got }})
	if message.Type != MsgAgentList || len(message.Agents) != 1 {
		t.Fatalf("websocket message = %#v", message)
	}
	assertAgentAttribution("websocket", message.Agents[0])
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

func TestFrontendMetaIsPublicAndNotCached(t *testing.T) {
	srv := New(&mockBoardService{}, nil, nil, ":0")
	srv.SetAuthToken("secret")
	req := httptest.NewRequest(http.MethodGet, "/frontend-meta", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q", got)
	}
	var meta struct {
		BuildID string `json:"build_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.BuildID == "" || meta.BuildID == "dev" {
		t.Fatalf("build ID = %q", meta.BuildID)
	}
}

func TestMobileMetaIsPublicAndVersioned(t *testing.T) {
	srv := New(&mockBoardService{}, nil, nil, ":0")
	srv.SetAuthToken("secret")
	req := httptest.NewRequest(http.MethodGet, "/api/meta", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var meta MobileMetaResponse
	if err := json.Unmarshal(w.Body.Bytes(), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Version == "" || meta.MobileShellAPI != 1 || meta.AuthBootstrap != "local_storage_v1" {
		t.Fatalf("meta = %#v", meta)
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

func TestAuthMiddlewareRejectsWithoutToken(t *testing.T) {
	svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(svc, nil, nil, ":0")
	srv.SetAuthToken("test-secret-token")
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/api/agents", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAuthMiddlewareRejectsWrongToken(t *testing.T) {
	svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(svc, nil, nil, ":0")
	srv.SetAuthToken("test-secret-token")
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/api/agents", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAuthMiddlewareAcceptsCorrectToken(t *testing.T) {
	svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(svc, nil, nil, ":0")
	srv.SetAuthToken("test-secret-token")
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/api/agents", nil)
	req.Header.Set("Authorization", "Bearer test-secret-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestAuthMiddlewareHealthExempt(t *testing.T) {
	svc := &mockBoardService{
		columns: []service.Column{{Name: "Backlog", SortOrder: 0}},
		cards:   map[string][]service.Card{"Backlog": {}},
	}
	srv := New(svc, nil, nil, ":0")
	srv.SetAuthToken("test-secret-token")
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("health status = %d, want 200", w.Code)
	}

	req = httptest.NewRequest("GET", "/api/board", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated board status = %d, want 401", w.Code)
	}

	req = httptest.NewRequest("GET", "/api/board", nil)
	req.Header.Set("Authorization", "Bearer test-secret-token")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("authenticated board status = %d, want 200", w.Code)
	}
}

func TestAuthMiddlewareOptionsExempt(t *testing.T) {
	svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(svc, nil, nil, ":0")
	srv.SetAuthToken("test-secret-token")
	handler := srv.Handler()

	req := httptest.NewRequest("OPTIONS", "/api/agents", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("OPTIONS status = %d, want 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("CORS origin = %q, want *", got)
	}
}

func TestAuthMiddlewareNoTokenConfigured(t *testing.T) {
	svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(svc, nil, nil, ":0")
	// No SetAuthToken call — auth disabled.
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/api/agents", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (auth disabled)", w.Code)
	}
}

func TestAuthMiddlewareWSRejectsWithoutToken(t *testing.T) {
	svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(svc, nil, nil, ":0")
	srv.SetAuthToken("test-secret-token")
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/ws", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("WS status = %d, want 401", w.Code)
	}
}

func TestAuthMiddlewareWSAcceptsTokenInQuery(t *testing.T) {
	svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(svc, nil, nil, ":0")
	srv.SetAuthToken("test-secret-token")
	handler := srv.Handler()

	// The handler will try to upgrade, which fails in tests without a real WS client,
	// but it should get past auth (not 401).
	req := httptest.NewRequest("GET", "/ws?token=test-secret-token", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// WebSocket upgrade fails gracefully (not 401).
	if w.Code == http.StatusUnauthorized {
		t.Error("WS with valid token should not return 401")
	}
}

func TestCORSHeadersOnResponse(t *testing.T) {
	svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(svc, nil, nil, ":0")
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/api/agents", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("CORS origin = %q, want *", got)
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
func (m *mockTmuxManager) Kill(name string) error                         { return nil }
func (m *mockTmuxManager) Capture(name string) (string, error)            { return "", nil }
func (m *mockTmuxManager) CaptureWithEscapes(name string) (string, error) { return "", nil }
func (m *mockTmuxManager) Attach(name string) *exec.Cmd                   { return exec.Command("echo") }
func (m *mockTmuxManager) ListSessions() ([]string, error)                { return nil, nil }
func (m *mockTmuxManager) IsAlive(name string) (bool, error)              { return false, nil }
func (m *mockTmuxManager) SendKeys(name, keys string) error               { return nil }
func (m *mockTmuxManager) SendKey(name, key string) error {
	m.sentKeys = append(m.sentKeys, key)
	return nil
}
func (m *mockTmuxManager) SendKeysLine(name, line string) error            { return nil }
func (m *mockTmuxManager) SendKeysMultiline(name, payload string) error    { return nil }
func (m *mockTmuxManager) SendKeysShellCommand(name, command string) error { return nil }
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

func TestPromptDetectionSetsDetectorOwnedWaiting(t *testing.T) {
	agents := &promptAgentService{
		agent: service.AgentSession{TaskID: "agent-1", AgentKind: "codex", Activity: ""},
	}
	sm := newStreamManager(&mockTmuxManager{}, agents)

	output := `Question 1/1 (1 unanswered)
  Choose one?

  › 1. Yes
    2. No

  tab to add notes | enter to submit all | esc to interrupt`
	detection := sm.detectPromptState("agent-1", output)

	if !detection.Blocking {
		t.Fatal("expected blocking detection")
	}
	if got := agents.setActivities; len(got) != 1 || got[0] != "waiting" {
		t.Fatalf("setActivities = %#v, want [waiting]", got)
	}
	sm.mu.Lock()
	owned := sm.detectorWaiting["agent-1"]
	sm.mu.Unlock()
	if !owned {
		t.Fatal("expected detector waiting ownership")
	}
}

func TestPromptDetectionDoesNotClearHookOwnedWaiting(t *testing.T) {
	agents := &promptAgentService{
		agent: service.AgentSession{TaskID: "agent-1", AgentKind: "codex", Activity: "waiting"},
	}
	sm := newStreamManager(&mockTmuxManager{}, agents)

	detection := sm.detectPromptState("agent-1", "ordinary output")

	if detection.Blocking {
		t.Fatal("ordinary output should not be blocking")
	}
	if len(agents.setActivities) != 0 {
		t.Fatalf("setActivities = %#v, want none", agents.setActivities)
	}
}

func TestClearDetectorWaitingClearsOnlyOwnedWaiting(t *testing.T) {
	agents := &promptAgentService{
		agent: service.AgentSession{TaskID: "agent-1", AgentKind: "codex", Activity: "waiting"},
	}
	sm := newStreamManager(&mockTmuxManager{}, agents)
	sm.detectorWaiting["agent-1"] = true

	sm.clearDetectorWaiting("agent-1")

	if got := agents.setActivities; len(got) != 1 || got[0] != "" {
		t.Fatalf("setActivities = %#v, want clear", got)
	}
}

// ---------- New board endpoint tests ----------

func TestBoardEndpointProjectsRelatedAgentSummaryOntoAttributedTask(t *testing.T) {
	svc := &mockBoardService{
		columns: []service.Column{{Name: "Doing"}},
		cards:   map[string][]service.Card{"Doing": {{ID: "task-a", Title: "Task A", Status: "Doing"}}},
	}
	agents := &promptAgentService{agent: service.AgentSession{
		TaskID: "workflow-b", AttributionTaskID: "task-a", Status: "running", Activity: "working",
	}}
	srv := New(svc, agents, nil, ":0")

	req := httptest.NewRequest(http.MethodGet, "/api/board", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp BoardResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	card := resp.Columns[0].Cards[0]
	if !card.AgentActive || card.AgentState != "working" {
		t.Fatalf("related agent card = %#v", card)
	}
}

func TestBoardEndpointReturnsShape(t *testing.T) {
	workspaceID := 1
	svc := &mockBoardService{
		columns: []service.Column{
			{Name: "Backlog", SortOrder: 0},
		},
		cards: map[string][]service.Card{
			"Backlog": {
				{ID: "t1", Title: "Task one", Status: "Backlog", Priority: "High", IssueType: "Bug", Provider: "jira", HasWarning: true, WorkspaceID: &workspaceID, WorkspaceName: "Work", WorkspaceColor: "#4A9EEF"},
			},
		},
		workspaces: []service.Workspace{
			{ID: 1, Name: "Work", Color: "#4A9EEF"},
		},
	}

	srv := New(svc, nil, nil, ":0")
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/api/board", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp BoardResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Columns) != 1 {
		t.Fatalf("columns = %d, want 1", len(resp.Columns))
	}
	if resp.Columns[0].Name != "Backlog" {
		t.Errorf("column[0].name = %q, want Backlog", resp.Columns[0].Name)
	}
	if len(resp.Columns[0].Cards) != 1 {
		t.Fatalf("cards = %d, want 1", len(resp.Columns[0].Cards))
	}
	card := resp.Columns[0].Cards[0]
	if card.ID != "t1" {
		t.Errorf("card.id = %q, want t1", card.ID)
	}
	if card.Priority != "High" {
		t.Errorf("card.priority = %q, want High", card.Priority)
	}
	if card.Provider != "jira" {
		t.Errorf("card.provider = %q, want jira", card.Provider)
	}
	if !card.HasWarning {
		t.Error("card.has_warning should be true")
	}
	if card.WorkspaceName != "Work" {
		t.Errorf("card.workspace_name = %q, want Work", card.WorkspaceName)
	}
	if card.WorkspaceID == nil || *card.WorkspaceID != workspaceID {
		t.Errorf("card.workspace_id = %v, want %d", card.WorkspaceID, workspaceID)
	}
	if len(resp.Workspaces) != 1 {
		t.Fatalf("workspaces = %d, want 1", len(resp.Workspaces))
	}
	if resp.Workspaces[0].Name != "Work" {
		t.Errorf("workspace[0].name = %q, want Work", resp.Workspaces[0].Name)
	}
}

func TestDeleteTaskMissingReturns404(t *testing.T) {
	svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(svc, nil, nil, ":0")
	handler := srv.Handler()

	req := httptest.NewRequest("DELETE", "/api/tasks/missing", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestCreateTaskReturns201(t *testing.T) {
	svc := &mockBoardService{
		columns:     []service.Column{},
		cards:       map[string][]service.Card{},
		createdCard: &service.Card{ID: "new-1", Title: "New task", Status: "Backlog", Priority: "Medium"},
	}
	srv := New(svc, nil, nil, ":0")
	handler := srv.Handler()

	body := `{"title":"New task","column":"Backlog","priority":"Medium"}`
	req := httptest.NewRequest("POST", "/api/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}

	var resp CardResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ID != "new-1" {
		t.Errorf("id = %q, want new-1", resp.ID)
	}
}

func TestCreateTaskWorkspaceDefaultDistinguishesOmittedNullAndExplicit(t *testing.T) {
	defaultID := 7
	tests := []struct {
		name   string
		body   string
		wantID *int
	}{
		{name: "omitted uses default", body: `{"title":"New task","column":"Backlog"}`, wantID: &defaultID},
		{name: "null stays unassigned", body: `{"title":"New task","column":"Backlog","workspace_id":null}`},
		{name: "explicit wins", body: `{"title":"New task","column":"Backlog","workspace_id":9}`, wantID: intPtr(9)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockBoardService{createdCard: &service.Card{ID: "new-1", Title: "New task", Status: "Backlog"}}
			srv := New(svc, nil, nil, ":0")
			srv.SetWorkspaceDefaults(&defaultID, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)
			if w.Code != http.StatusCreated {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
			if tt.wantID == nil {
				if svc.createdWorkspace != nil {
					t.Fatalf("workspace = %v, want nil", svc.createdWorkspace)
				}
			} else if svc.createdWorkspace == nil || *svc.createdWorkspace != *tt.wantID {
				t.Fatalf("workspace = %v, want %d", svc.createdWorkspace, *tt.wantID)
			}
		})
	}
}

func TestArchiveDoneReturnsCount(t *testing.T) {
	svc := &mockBoardService{
		columns:      []service.Column{},
		cards:        map[string][]service.Card{},
		archiveCount: 3,
	}
	srv := New(svc, nil, nil, ":0")
	handler := srv.Handler()

	req := httptest.NewRequest("POST", "/api/board/archive-done", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp ArchiveDoneResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Archived != 3 {
		t.Errorf("archived = %d, want 3", resp.Archived)
	}
}

func TestSearchTasksShortQuery(t *testing.T) {
	svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(svc, nil, nil, ":0")
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/api/tasks/search?q=x", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp []CardResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("results = %d, want 0 for short query", len(resp))
	}
}

func TestSearchTasksLongerQuery(t *testing.T) {
	svc := &mockBoardService{
		columns: []service.Column{},
		cards:   map[string][]service.Card{},
		searchResults: []service.Card{
			{ID: "t1", Title: "Find me"},
		},
	}
	srv := New(svc, nil, nil, ":0")
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/api/tasks/search?q=find", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp []CardResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp) != 1 {
		t.Errorf("results = %d, want 1", len(resp))
	}
}

func TestRemoteSearchNilSyncService(t *testing.T) {
	svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(svc, nil, nil, ":0")
	// sync service is nil by default
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/api/remote/search?q=query", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestRemoteSearchWithSyncService(t *testing.T) {
	svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(svc, nil, nil, ":0")
	srv.SetSyncService(&mockSyncService{
		searchResults: []service.RemoteSearchResult{
			{ID: "JIRA-1", Summary: "Test issue", Status: "Open", Priority: "High", IssueType: "Bug"},
		},
	})
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/api/remote/search?q=query", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp []RemoteSearchResultResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp) != 1 {
		t.Errorf("results = %d, want 1", len(resp))
	}
}

func TestBoardEventsBroadcastCardsChanged(t *testing.T) {
	bus := events.New()
	svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := NewWithSwarm(svc, nil, nil, ":0", nil, bus, "")
	srv.StartBoardEvents()

	// Start a test WebSocket server
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() { _ = srv.Serve(ln) }()
	defer srv.Stop(context.Background())

	// Helper to read a message from WS
	readMsg := func(conn *websocket.Conn) WSMessage {
		// Give broadcaster time to run
		time.Sleep(150 * time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("ws read: %v", err)
		}
		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("unmarshal ws msg: %v", err)
		}
		return msg
	}

	// Dial WebSocket
	wsURL := fmt.Sprintf("ws://%s/ws", ln.Addr().String())
	wsCtx, wsCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer wsCancel()
	conn, _, err := websocket.Dial(wsCtx, wsURL, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Consume initial agent_list message
	_ = readMsg(conn)

	t.Run("CardsRefreshed", func(t *testing.T) {
		bus.Publish(events.Event{Type: events.EventCardsRefreshed})
		msg := readMsg(conn)
		if msg.Type != MsgCardsChanged {
			t.Errorf("type = %q, want %q", msg.Type, MsgCardsChanged)
		}
	})

	t.Run("CardMoved", func(t *testing.T) {
		bus.Publish(events.Event{Type: events.EventCardMoved})
		msg := readMsg(conn)
		if msg.Type != MsgCardsChanged {
			t.Errorf("type = %q, want %q", msg.Type, MsgCardsChanged)
		}
	})

	t.Run("CardUpdated", func(t *testing.T) {
		bus.Publish(events.Event{Type: events.EventCardUpdated})
		msg := readMsg(conn)
		if msg.Type != MsgCardsChanged {
			t.Errorf("type = %q, want %q", msg.Type, MsgCardsChanged)
		}
	})

	t.Run("PRStatusUpdated", func(t *testing.T) {
		bus.Publish(events.Event{Type: events.EventPRStatusUpdated})
		msg := readMsg(conn)
		if msg.Type != MsgCardsChanged {
			t.Errorf("type = %q, want %q", msg.Type, MsgCardsChanged)
		}
	})
}

func TestGetTaskDetail_Returns200(t *testing.T) {
	svc := &mockBoardService{
		columns: []service.Column{},
		cards:   map[string][]service.Card{},
		cardDetail: &service.CardDetail{
			ID:                  "task-1",
			Title:               "Test task",
			DescriptionMD:       "provider\n\n## Legato details\n\nlocal",
			LocalDescriptionMD:  "local",
			RemoteDescriptionMD: "provider",
			Status:              "Doing",
			Priority:            "High",
			HasChimeraSession:   true,
			CreatedAt:           time.Now(),
			UpdatedAt:           time.Now(),
			Notes:               []store.TaskNote{{ID: "tn-1", TaskID: "task-1", Body: "Triage note", Source: "workflow"}},
			PRMeta: &service.PRMetaView{
				Repo:     "acme/app",
				Branch:   "feat/x",
				PRNumber: 42,
			},
		},
	}
	srv := New(svc, nil, nil, ":0")
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/api/tasks/task-1", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp CardDetailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.DescriptionMD != "provider\n\n## Legato details\n\nlocal" || resp.LocalDescriptionMD != "local" || resp.RemoteDescriptionMD != "provider" {
		t.Fatalf("descriptions = %q / %q / %q", resp.DescriptionMD, resp.LocalDescriptionMD, resp.RemoteDescriptionMD)
	}
	if len(resp.Notes) != 1 || resp.Notes[0].Body != "Triage note" {
		t.Fatalf("notes = %#v", resp.Notes)
	}
	if !resp.HasChimeraSession {
		t.Fatal("has_chimera_session = false, want true")
	}
	if resp.PRMeta == nil {
		t.Fatal("pr_meta is nil")
	}
	if resp.PRMeta.Repo != "acme/app" {
		t.Errorf("pr_meta.repo = %q, want acme/app", resp.PRMeta.Repo)
	}
	if resp.PRMeta.Branch != "feat/x" {
		t.Errorf("pr_meta.branch = %q, want feat/x", resp.PRMeta.Branch)
	}
}

func TestGetTaskDetail_Returns404(t *testing.T) {
	svc := &mockBoardService{
		columns:    []service.Column{},
		cards:      map[string][]service.Card{},
		cardDetail: nil,
	}
	srv := New(svc, nil, nil, ":0")
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/api/tasks/unknown", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestGetTaskDetail_FullFormat(t *testing.T) {
	svc := &mockBoardService{
		columns: []service.Column{},
		cards:   map[string][]service.Card{},
		cardDetail: &service.CardDetail{
			ID:        "task-1",
			Title:     "Test task",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	srv := New(svc, nil, nil, ":0")
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/api/tasks/task-1?format=full", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "text/plain" {
		t.Errorf("content-type = %q, want text/plain", ct)
	}
	if len(w.Body.Bytes()) == 0 {
		t.Error("body is empty")
	}
}

func TestArchiveTaskReturns200(t *testing.T) {
	svc := &mockBoardService{
		columns: []service.Column{},
		cards:   map[string][]service.Card{},
		cardDetail: &service.CardDetail{
			ID:        "task-1",
			Title:     "Task to archive",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	srv := New(svc, nil, nil, ":0")
	handler := srv.Handler()

	req := httptest.NewRequest("POST", "/api/tasks/task-1/archive", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestArchiveTaskMissingReturns404(t *testing.T) {
	svc := &mockBoardService{
		columns:    []service.Column{},
		cards:      map[string][]service.Card{},
		cardDetail: nil,
	}
	srv := New(svc, nil, nil, ":0")
	handler := srv.Handler()

	req := httptest.NewRequest("POST", "/api/tasks/unknown/archive", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestRemoteImportReturns201(t *testing.T) {
	svc := &mockBoardService{
		columns: []service.Column{},
		cards:   map[string][]service.Card{},
	}
	srv := New(svc, nil, nil, ":0")
	srv.SetSyncService(&mockSyncService{
		importedCard: &service.Card{
			ID:       "imp-1",
			Title:    "Imported ticket",
			Priority: "Medium",
			Status:   "Backlog",
		},
	})
	handler := srv.Handler()

	body := `{"ticket_id":"JIRA-42"}`
	req := httptest.NewRequest("POST", "/api/remote/import", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}
	var resp CardResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ID != "imp-1" {
		t.Errorf("id = %q, want imp-1", resp.ID)
	}
}

func TestRemoteImportWorkspaceDefaultDistinguishesOmittedNullAndExplicit(t *testing.T) {
	defaultID := 7
	tests := []struct {
		name   string
		body   string
		wantID *int
	}{
		{name: "omitted uses default", body: `{"ticket_id":"JIRA-42"}`, wantID: &defaultID},
		{name: "null stays unassigned", body: `{"ticket_id":"JIRA-42","workspace_id":null}`},
		{name: "explicit wins", body: `{"ticket_id":"JIRA-42","workspace_id":9}`, wantID: intPtr(9)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			syncSvc := &mockSyncService{importedCard: &service.Card{ID: "imp-1", Title: "Imported ticket", Status: "Backlog"}}
			srv := New(&mockBoardService{}, nil, nil, ":0")
			srv.SetSyncService(syncSvc)
			srv.SetWorkspaceDefaults(nil, &defaultID)
			req := httptest.NewRequest(http.MethodPost, "/api/remote/import", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)
			if w.Code != http.StatusCreated {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
			if tt.wantID == nil {
				if syncSvc.importedWorkspace != nil {
					t.Fatalf("workspace = %v, want nil", syncSvc.importedWorkspace)
				}
			} else if syncSvc.importedWorkspace == nil || *syncSvc.importedWorkspace != *tt.wantID {
				t.Fatalf("workspace = %v, want %d", syncSvc.importedWorkspace, *tt.wantID)
			}
		})
	}
}

func TestSettingsExposeWorkspaceDefaults(t *testing.T) {
	taskID, importID := 2, 3
	srv := New(&mockBoardService{}, nil, nil, ":0")
	srv.SetWorkspaceDefaults(&taskID, &importID)
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var response struct {
		DefaultWorkspaceID       *int `json:"default_workspace_id"`
		DefaultImportWorkspaceID *int `json:"default_import_workspace_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.DefaultWorkspaceID == nil || *response.DefaultWorkspaceID != taskID || response.DefaultImportWorkspaceID == nil || *response.DefaultImportWorkspaceID != importID {
		t.Fatalf("response = %+v", response)
	}
}

func TestPatchTaskRoutesEdits(t *testing.T) {
	t.Run("title", func(t *testing.T) {
		svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
		srv := New(svc, nil, nil, ":0")
		handler := srv.Handler()
		body := `{"title":"new title"}`
		req := httptest.NewRequest("PATCH", "/api/tasks/t1", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if svc.updatedTitle != "new title" {
			t.Errorf("updatedTitle = %q, want new title", svc.updatedTitle)
		}
	})

	t.Run("description", func(t *testing.T) {
		svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
		srv := New(svc, nil, nil, ":0")
		handler := srv.Handler()
		body := `{"description":"new desc"}`
		req := httptest.NewRequest("PATCH", "/api/tasks/t1", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if svc.updatedDesc != "new desc" {
			t.Errorf("updatedDesc = %q, want new desc", svc.updatedDesc)
		}
	})

	t.Run("workspace_id", func(t *testing.T) {
		svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
		srv := New(svc, nil, nil, ":0")
		handler := srv.Handler()
		body := `{"workspace_id":3}`
		req := httptest.NewRequest("PATCH", "/api/tasks/t1", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if svc.updatedWorkspace == nil || *svc.updatedWorkspace != 3 {
			t.Errorf("updatedWorkspace = %v, want 3", svc.updatedWorkspace)
		}
	})

	t.Run("workspace_id null", func(t *testing.T) {
		svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
		srv := New(svc, nil, nil, ":0")
		handler := srv.Handler()
		body := `{"workspace_id":null}`
		req := httptest.NewRequest("PATCH", "/api/tasks/t1", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if !svc.updatedWorkspaceCalled || svc.updatedWorkspace != nil {
			t.Errorf("workspace update called = %v with %v, want called with nil", svc.updatedWorkspaceCalled, svc.updatedWorkspace)
		}
	})

	t.Run("column", func(t *testing.T) {
		svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
		srv := New(svc, nil, nil, ":0")
		var notified string
		srv.notifyTaskChanged = func(taskID string) { notified = taskID }
		handler := srv.Handler()
		body := `{"column":"Doing"}`
		req := httptest.NewRequest("PATCH", "/api/tasks/t1", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if svc.movedTo != "Doing" {
			t.Errorf("movedTo = %q, want Doing", svc.movedTo)
		}
		if notified != "t1" {
			t.Errorf("notified task = %q, want t1", notified)
		}
	})

	t.Run("failed column move does not notify", func(t *testing.T) {
		svc := &mockBoardService{moveErr: errors.New("move failed")}
		srv := New(svc, nil, nil, ":0")
		notified := false
		srv.notifyTaskChanged = func(string) { notified = true }
		req := httptest.NewRequest("PATCH", "/api/tasks/t1", strings.NewReader(`{"column":"Doing"}`))
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError || notified {
			t.Fatalf("status=%d notified=%v", rec.Code, notified)
		}
	})

	t.Run("partial mutation still notifies", func(t *testing.T) {
		svc := &mockBoardService{moveErr: errors.New("move failed")}
		srv := New(svc, nil, nil, ":0")
		notified := false
		srv.notifyTaskChanged = func(string) { notified = true }
		req := httptest.NewRequest("PATCH", "/api/tasks/t1", strings.NewReader(`{"title":"changed","column":"Doing"}`))
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError || !notified || svc.updatedTitle != "changed" {
			t.Fatalf("status=%d notified=%v title=%q", rec.Code, notified, svc.updatedTitle)
		}
	})

	t.Run("column uses sync-aware move when available", func(t *testing.T) {
		board := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
		syncSvc := &mockSyncService{}
		srv := New(board, nil, nil, ":0")
		srv.SetSyncService(syncSvc)
		req := httptest.NewRequest("PATCH", "/api/tasks/t1", strings.NewReader(`{"column":"Triage"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if syncSvc.movedTaskID != "t1" || syncSvc.movedTo != "Triage" {
			t.Fatalf("sync move = (%q, %q)", syncSvc.movedTaskID, syncSvc.movedTo)
		}
		if board.movedTo != "" {
			t.Fatalf("board fallback unexpectedly moved to %q", board.movedTo)
		}
	})
}

func TestPRPreviewHandler_Returns200(t *testing.T) {
	svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(svc, nil, nil, ":0")
	srv.SetPRTrackingService(&mockPRTrackingService{
		fetchPRFunc: func(owner, repo string, prNumber int) (*github.PRStatus, error) {
			return &github.PRStatus{
				Number:         prNumber,
				URL:            "https://github.com/acme/app/pull/42",
				State:          "OPEN",
				IsDraft:        false,
				CheckStatus:    "pass",
				ReviewDecision: "APPROVED",
				CommentCount:   3,
				HeadBranch:     "feat/x",
				Title:          "Fix the thing",
			}, nil
		},
	})
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/api/tasks/t1/pr-preview?owner=acme&repo=app&number=42", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp PRPreviewResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Number != 42 {
		t.Errorf("number = %d, want 42", resp.Number)
	}
	if resp.HeadBranch != "feat/x" {
		t.Errorf("head_branch = %q, want feat/x", resp.HeadBranch)
	}
}

func TestPRPreviewHandler_NilService_Returns503(t *testing.T) {
	svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(svc, nil, nil, ":0")
	// prTracking is nil by default
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/api/tasks/t1/pr-preview?owner=acme&repo=app&number=42", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestTargetedPRAndCodeContextHandlersForwardTaskActions(t *testing.T) {
	board := &mockBoardService{}
	actions := &mockPRTaskActions{}
	srv := New(board, nil, nil, ":0")
	srv.SetPRTrackingService(actions)

	contextBody := `{"repo":"acme/widget","branch":"feature/TASK-1"}`
	req := httptest.NewRequest(http.MethodPut, "/api/tasks/TASK-1/code-context", strings.NewReader(contextBody))
	req.SetPathValue("id", "TASK-1")
	response := httptest.NewRecorder()
	srv.codeContextHandler().ServeHTTP(response, req)
	if response.Code != http.StatusOK || actions.contextTaskID != "TASK-1" || actions.context == nil || actions.context.RepoSource != "manual" {
		t.Fatalf("code=%d task=%q context=%#v body=%s", response.Code, actions.contextTaskID, actions.context, response.Body.String())
	}

	for endpoint, target := range map[string]*string{"refresh-pr": &actions.refreshed, "rediscover-pr": &actions.rediscovered} {
		req = httptest.NewRequest(http.MethodPost, "/api/tasks/TASK-1/"+endpoint, nil)
		req.SetPathValue("id", "TASK-1")
		response = httptest.NewRecorder()
		if endpoint == "refresh-pr" {
			srv.refreshPRHandler().ServeHTTP(response, req)
		} else {
			srv.rediscoverPRHandler().ServeHTTP(response, req)
		}
		if response.Code != http.StatusOK || *target != "TASK-1" {
			t.Fatalf("%s code=%d target=%q", endpoint, response.Code, *target)
		}
	}
}

func TestLinkPRHandler_Returns200(t *testing.T) {
	svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(svc, nil, nil, ":0")
	mockPR := &mockPRTrackingService{}
	srv.SetPRTrackingService(mockPR)
	handler := srv.Handler()

	body := `{"owner":"acme","repo":"app","pr_number":42}`
	req := httptest.NewRequest("POST", "/api/tasks/task-1/link-pr", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if mockPR.linkedTaskID != "task-1" {
		t.Errorf("linkedTaskID = %q, want task-1", mockPR.linkedTaskID)
	}
}

func TestLinkPRHandler_NilService_Returns503(t *testing.T) {
	svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(svc, nil, nil, ":0")
	// prTracking is nil by default
	handler := srv.Handler()

	body := `{"owner":"acme","repo":"app","pr_number":42}`
	req := httptest.NewRequest("POST", "/api/tasks/task-1/link-pr", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestCORSAllowsBoardMethods(t *testing.T) {
	svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(svc, nil, nil, ":0")
	handler := srv.Handler()

	t.Run("PATCH", func(t *testing.T) {
		req := httptest.NewRequest("OPTIONS", "/api/tasks/t1", nil)
		req.Header.Set("Origin", "http://example.com")
		req.Header.Set("Access-Control-Request-Method", "PATCH")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", w.Code)
		}
		methods := w.Header().Get("Access-Control-Allow-Methods")
		if !strings.Contains(methods, "PATCH") {
			t.Errorf("Allow-Methods = %q, expected PATCH", methods)
		}
	})

	t.Run("DELETE", func(t *testing.T) {
		req := httptest.NewRequest("OPTIONS", "/api/tasks/t1", nil)
		req.Header.Set("Origin", "http://example.com")
		req.Header.Set("Access-Control-Request-Method", "DELETE")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", w.Code)
		}
		methods := w.Header().Get("Access-Control-Allow-Methods")
		if !strings.Contains(methods, "DELETE") {
			t.Errorf("Allow-Methods = %q, expected DELETE", methods)
		}
	})
}

func TestKillAgentHandlerCancelsConductorSwarm(t *testing.T) {
	agents := &promptAgentService{agent: service.AgentSession{TaskID: "PARENT-1", Role: "conductor"}}
	var cancelled string
	swarmSvc := &mockSwarmService{cancelSwarmFunc: func(_ context.Context, parentID string) error {
		cancelled = parentID
		return nil
	}}
	srv := NewWithSwarm(nil, agents, nil, "", swarmSvc, nil, "")
	req := httptest.NewRequest(http.MethodPost, "/api/agents/kill", strings.NewReader(`{"task_id":"PARENT-1"}`))
	rec := httptest.NewRecorder()

	srv.killAgentHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if cancelled != "PARENT-1" {
		t.Fatalf("cancelled swarm = %q, want PARENT-1", cancelled)
	}
	if agents.killedTaskID != "" {
		t.Fatalf("directly killed agent %q; conductor must route through CancelSwarm", agents.killedTaskID)
	}
}

func TestKillAgentHandlerKillsOrdinaryAgentOnly(t *testing.T) {
	agents := &promptAgentService{agent: service.AgentSession{TaskID: "AGENT-1", Role: "worker"}}
	cancelled := false
	swarmSvc := &mockSwarmService{cancelSwarmFunc: func(context.Context, string) error {
		cancelled = true
		return nil
	}}
	srv := NewWithSwarm(nil, agents, nil, "", swarmSvc, nil, "")
	req := httptest.NewRequest(http.MethodPost, "/api/agents/kill", strings.NewReader(`{"task_id":"AGENT-1"}`))
	rec := httptest.NewRecorder()

	srv.killAgentHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if agents.killedTaskID != "AGENT-1" {
		t.Fatalf("killed agent = %q, want AGENT-1", agents.killedTaskID)
	}
	if cancelled {
		t.Fatal("ordinary agent kill cancelled a swarm")
	}
}
