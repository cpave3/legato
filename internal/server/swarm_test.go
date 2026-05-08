package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type mockSwarmService struct {
	startSwarmFunc     func(ctx context.Context, parentID, workingDir string) error
	dispatchFunc       func(ctx context.Context, subtaskID string) error
	messageFunc        func(ctx context.Context, subtaskID, text string) error
	messageParentFunc  func(ctx context.Context, parentID, text string) error
	broadcastFunc      func(ctx context.Context, parentID, text string) (int, error)
	closeFunc          func(ctx context.Context, subtaskID string) error
	finishFunc         func(ctx context.Context, parentID, summary string) error
	snapshotFunc       func(ctx context.Context, parentID string) ([]byte, error)
	listSubtaskInfosFunc func(ctx context.Context, parentID string) ([]service.SwarmSubtaskInfo, error)
	fetchInboxFunc     func(ctx context.Context, parentID string) ([]service.InboxEntry, error)
	peekInboxFunc      func(ctx context.Context, parentID string) ([]service.InboxEntry, error)
}

func (m *mockSwarmService) StartSwarm(ctx context.Context, parentID, workingDir string) error {
	if m.startSwarmFunc != nil {
		return m.startSwarmFunc(ctx, parentID, workingDir)
	}
	return nil
}

func (m *mockSwarmService) Dispatch(ctx context.Context, subtaskID string) error {
	if m.dispatchFunc != nil {
		return m.dispatchFunc(ctx, subtaskID)
	}
	return nil
}

func (m *mockSwarmService) Message(ctx context.Context, subtaskID, text string) error {
	if m.messageFunc != nil {
		return m.messageFunc(ctx, subtaskID, text)
	}
	return nil
}

func (m *mockSwarmService) MessageParent(ctx context.Context, parentID, text string) error {
	if m.messageParentFunc != nil {
		return m.messageParentFunc(ctx, parentID, text)
	}
	return nil
}

func (m *mockSwarmService) Broadcast(ctx context.Context, parentID, text string) (int, error) {
	if m.broadcastFunc != nil {
		return m.broadcastFunc(ctx, parentID, text)
	}
	return 0, nil
}

func (m *mockSwarmService) Close(ctx context.Context, subtaskID string) error {
	if m.closeFunc != nil {
		return m.closeFunc(ctx, subtaskID)
	}
	return nil
}

func (m *mockSwarmService) Finish(ctx context.Context, parentID, summary string) error {
	if m.finishFunc != nil {
		return m.finishFunc(ctx, parentID, summary)
	}
	return nil
}

func (m *mockSwarmService) Snapshot(ctx context.Context, parentID string) ([]byte, error) {
	if m.snapshotFunc != nil {
		return m.snapshotFunc(ctx, parentID)
	}
	return json.Marshal(map[string]interface{}{"parent": map[string]string{"id": parentID}})
}

func (m *mockSwarmService) ListSubtaskInfos(ctx context.Context, parentID string) ([]service.SwarmSubtaskInfo, error) {
	if m.listSubtaskInfosFunc != nil {
		return m.listSubtaskInfosFunc(ctx, parentID)
	}
	return nil, nil
}

func (m *mockSwarmService) FetchInbox(ctx context.Context, parentID string) ([]service.InboxEntry, error) {
	if m.fetchInboxFunc != nil {
		return m.fetchInboxFunc(ctx, parentID)
	}
	return nil, nil
}

func (m *mockSwarmService) PeekInbox(ctx context.Context, parentID string) ([]service.InboxEntry, error) {
	if m.peekInboxFunc != nil {
		return m.peekInboxFunc(ctx, parentID)
	}
	return nil, nil
}

func newTestServerWithSwarm(token string, swarm SwarmService) *Server {
	board := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(board, nil, nil, ":0")
	srv.swarm = swarm
	if token != "" {
		srv.SetAuthToken(token)
	}
	return srv
}

func TestSwarmStartHappyPath(t *testing.T) {
	sw := &mockSwarmService{
		startSwarmFunc: func(ctx context.Context, parentID, workingDir string) error {
			if parentID != "task-1" || workingDir != "/tmp/wd" {
				t.Errorf("unexpected args: %s, %s", parentID, workingDir)
			}
			return nil
		},
	}
	srv := newTestServerWithSwarm("", sw)
	body := `{"parent_task_id":"task-1","working_dir":"/tmp/wd"}`
	req := httptest.NewRequest("POST", "/api/swarm/start", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "ok" {
		t.Errorf("status = %q, want ok", resp["status"])
	}
}

func TestSwarmStartMethodNotAllowed(t *testing.T) {
	sw := &mockSwarmService{}
	srv := newTestServerWithSwarm("", sw)
	req := httptest.NewRequest("GET", "/api/swarm/start", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", w.Code)
	}
}

func TestSwarmStartParentNotFound(t *testing.T) {
	sw := &mockSwarmService{
		startSwarmFunc: func(ctx context.Context, parentID, workingDir string) error {
			return store.ErrNotFound
		},
	}
	srv := newTestServerWithSwarm("", sw)
	body := `{"parent_task_id":"missing","working_dir":"/tmp/wd"}`
	req := httptest.NewRequest("POST", "/api/swarm/start", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestSwarmStartDoubleStart(t *testing.T) {
	sw := &mockSwarmService{
		startSwarmFunc: func(ctx context.Context, parentID, workingDir string) error {
			return errors.New("parent task task-1 already has a running agent — kill it before starting a swarm")
		},
	}
	srv := newTestServerWithSwarm("", sw)
	body := `{"parent_task_id":"task-1","working_dir":"/tmp/wd"}`
	req := httptest.NewRequest("POST", "/api/swarm/start", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
}

func TestSwarmStartInvalidWorkingDir(t *testing.T) {
	sw := &mockSwarmService{}
	srv := newTestServerWithSwarm("", sw)
	body := `{"parent_task_id":"task-1"}`
	req := httptest.NewRequest("POST", "/api/swarm/start", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestSwarmDispatchHappyPath(t *testing.T) {
	sw := &mockSwarmService{
		dispatchFunc: func(ctx context.Context, subtaskID string) error {
			if subtaskID != "st-abc" {
				t.Errorf("subtaskID = %q, want st-abc", subtaskID)
			}
			return nil
		},
	}
	srv := newTestServerWithSwarm("", sw)
	body := `{"subtask_id":"st-abc"}`
	req := httptest.NewRequest("POST", "/api/swarm/dispatch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestSwarmMessageHappyPath(t *testing.T) {
	sw := &mockSwarmService{
		messageFunc: func(ctx context.Context, subtaskID, text string) error {
			if subtaskID != "st-abc" || text != "hello" {
				t.Errorf("unexpected args: %s, %s", subtaskID, text)
			}
			return nil
		},
	}
	srv := newTestServerWithSwarm("", sw)
	body := `{"task_id":"st-abc","text":"hello"}`
	req := httptest.NewRequest("POST", "/api/swarm/message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}



func TestSwarmMessageFallbackToParent(t *testing.T) {
	messageCalled := false
	parentCalled := false
	sw := &mockSwarmService{
		messageFunc: func(ctx context.Context, subtaskID, text string) error {
			messageCalled = true
			return store.ErrNotFound // subtask lookup fails
		},
		messageParentFunc: func(ctx context.Context, parentID, text string) error {
			parentCalled = true
			if parentID != "task-parent" || text != "hello" {
				t.Errorf("unexpected args: %s, %s", parentID, text)
			}
			return nil
		},
	}
	srv := newTestServerWithSwarm("", sw)
	body := `{"task_id":"task-parent","text":"hello"}`
	req := httptest.NewRequest("POST", "/api/swarm/message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !messageCalled {
		t.Error("Message should have been called first")
	}
	if !parentCalled {
		t.Error("MessageParent should have been called as fallback")
	}
}

func TestSwarmBroadcastHappyPath(t *testing.T) {
	sw := &mockSwarmService{
		broadcastFunc: func(ctx context.Context, parentID, text string) (int, error) {
			if parentID != "task-1" || text != "hello all" {
				t.Errorf("unexpected args: %s, %s", parentID, text)
			}
			return 3, nil
		},
	}
	srv := newTestServerWithSwarm("", sw)
	body := `{"parent_task_id":"task-1","text":"hello all"}`
	req := httptest.NewRequest("POST", "/api/swarm/broadcast", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "ok" {
		t.Errorf("status = %q, want ok", resp["status"])
	}
	if resp["count"] != float64(3) {
		t.Errorf("count = %v, want 3", resp["count"])
	}
}

func TestSwarmBroadcastParentNotFound(t *testing.T) {
	sw := &mockSwarmService{
		broadcastFunc: func(ctx context.Context, parentID, text string) (int, error) {
			return 0, store.ErrNotFound
		},
	}
	srv := newTestServerWithSwarm("", sw)
	body := `{"parent_task_id":"task-missing","text":"hello"}`
	req := httptest.NewRequest("POST", "/api/swarm/broadcast", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}
func TestSwarmCloseHappyPath(t *testing.T) {
	sw := &mockSwarmService{
		closeFunc: func(ctx context.Context, subtaskID string) error {
			if subtaskID != "st-abc" {
				t.Errorf("subtaskID = %q, want st-abc", subtaskID)
			}
			return nil
		},
	}
	srv := newTestServerWithSwarm("", sw)
	body := `{"subtask_id":"st-abc"}`
	req := httptest.NewRequest("POST", "/api/swarm/close", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestSwarmFinishHappyPath(t *testing.T) {
	sw := &mockSwarmService{
		finishFunc: func(ctx context.Context, parentID, summary string) error {
			if parentID != "task-1" || summary != "done" {
				t.Errorf("unexpected args: %s, %s", parentID, summary)
			}
			return nil
		},
	}
	srv := newTestServerWithSwarm("", sw)
	body := `{"parent_task_id":"task-1","summary":"done"}`
	req := httptest.NewRequest("POST", "/api/swarm/finish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestSwarmStatusHappyPath(t *testing.T) {
	sw := &mockSwarmService{
		snapshotFunc: func(ctx context.Context, parentID string) ([]byte, error) {
			b, _ := json.Marshal(map[string]interface{}{"parent": map[string]string{"id": "task-1"}})
			return b, nil
		},
		listSubtaskInfosFunc: func(ctx context.Context, parentID string) ([]service.SwarmSubtaskInfo, error) {
			return []service.SwarmSubtaskInfo{{ID: "st-1", Title: "Sub", Status: "queued"}}, nil
		},
	}
	srv := newTestServerWithSwarm("", sw)
	req := httptest.NewRequest("GET", "/api/swarm/status/task-1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["parent"] == nil {
		t.Error("expected parent in response")
	}
}

func TestSwarmInboxHappyPath(t *testing.T) {
	sw := &mockSwarmService{
		fetchInboxFunc: func(ctx context.Context, parentID string) ([]service.InboxEntry, error) {
			return []service.InboxEntry{{ID: 1, Kind: "built", Payload: "x"}}, nil
		},
	}
	srv := newTestServerWithSwarm("", sw)
	req := httptest.NewRequest("GET", "/api/swarm/inbox/task-1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp struct {
		Entries []service.InboxEntry `json:"entries"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(resp.Entries))
	}
}

func TestSwarmInboxPeek(t *testing.T) {
	calledPeek := false
	sw := &mockSwarmService{
		peekInboxFunc: func(ctx context.Context, parentID string) ([]service.InboxEntry, error) {
			calledPeek = true
			return []service.InboxEntry{{ID: 1, Kind: "built", Payload: "x"}}, nil
		},
		fetchInboxFunc: func(ctx context.Context, parentID string) ([]service.InboxEntry, error) {
			t.Error("FetchInbox should not be called when peeking")
			return nil, nil
		},
	}
	srv := newTestServerWithSwarm("", sw)
	req := httptest.NewRequest("GET", "/api/swarm/inbox/task-1?peek=true", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !calledPeek {
		t.Error("PeekInbox was not called")
	}
}

func TestSwarmPendingPlanHappyPath(t *testing.T) {
	srv := newTestServerWithSwarm("", &mockSwarmService{})
	srv.pendingMu.Lock()
	srv.pendingPlans["task-1"] = &pendingPlanEntry{
		PlanPath:    "/tmp/plan.json",
		ReplySocket: "/tmp/reply.sock",
		CreatedAt:   time.Now(),
	}
	srv.pendingMu.Unlock()

	req := httptest.NewRequest("GET", "/api/swarm/pending-plan/task-1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["plan_path"] != "/tmp/plan.json" {
		t.Errorf("plan_path = %q, want /tmp/plan.json", resp["plan_path"])
	}
}

func TestSwarmPendingPlanNotFound(t *testing.T) {
	srv := newTestServerWithSwarm("", &mockSwarmService{})
	req := httptest.NewRequest("GET", "/api/swarm/pending-plan/task-missing", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestSwarmPendingPlanExpired(t *testing.T) {
	srv := newTestServerWithSwarm("", &mockSwarmService{})
	srv.pendingMu.Lock()
	srv.pendingPlans["task-1"] = &pendingPlanEntry{
		PlanPath:    "/tmp/plan.json",
		ReplySocket: "/tmp/reply.sock",
		CreatedAt:   time.Now().Add(-10 * time.Minute),
	}
	srv.pendingMu.Unlock()

	req := httptest.NewRequest("GET", "/api/swarm/pending-plan/task-1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestSwarmUnauthorized(t *testing.T) {
	sw := &mockSwarmService{}
	srv := newTestServerWithSwarm("secret", sw)
	req := httptest.NewRequest("POST", "/api/swarm/start", strings.NewReader(`{"parent_task_id":"t","working_dir":"/"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "unauthorized" {
		t.Errorf("error = %q, want unauthorized", resp["error"])
	}
}

func TestSwarmCORSPreflight(t *testing.T) {
	sw := &mockSwarmService{}
	srv := newTestServerWithSwarm("secret", sw)
	req := httptest.NewRequest("OPTIONS", "/api/swarm/start", nil)
	req.Header.Set("Origin", "http://example.com")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("CORS origin = %q, want *", got)
	}
}

func TestStartSwarmEventsBroadcastsPlanProposed(t *testing.T) {
	bus := events.New()
	board := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(board, nil, nil, ":0")
	srv.bus = bus
	srv.pendingPlans = make(map[string]*pendingPlanEntry)
	srv.StartSwarmEvents()

	// Give goroutine time to start.
	time.Sleep(50 * time.Millisecond)

	bus.Publish(events.Event{
		Type: events.EventPlanProposed,
		Payload: events.PlanProposedPayload{
			ParentTaskID: "p1",
			PlanPath:     "/tmp/plan.json",
			ReplySocket:  "/tmp/reply.sock",
		},
	})

	// TODO: verify broadcast via a real websocket client in integration test.
	// For unit test, verify in-memory map.
	time.Sleep(50 * time.Millisecond)

	srv.pendingMu.RLock()
	_, ok := srv.pendingPlans["p1"]
	srv.pendingMu.RUnlock()
	if !ok {
		t.Error("pendingPlans should contain p1")
	}
}

func TestStartSwarmEventsBroadcastsSwarmChanged(t *testing.T) {
	bus := events.New()
	board := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(board, nil, nil, ":0")
	srv.bus = bus
	srv.pendingPlans = make(map[string]*pendingPlanEntry)

	// Pre-populate a pending plan.
	srv.pendingMu.Lock()
	srv.pendingPlans["p1"] = &pendingPlanEntry{
		PlanPath:    "/tmp/plan.json",
		ReplySocket: "/tmp/reply.sock",
		CreatedAt:   time.Now(),
	}
	srv.pendingMu.Unlock()

	srv.StartSwarmEvents()
	time.Sleep(50 * time.Millisecond)

	bus.Publish(events.Event{
		Type: events.EventSwarmChanged,
		Payload: events.SwarmChangedPayload{
			ParentTaskID: "p1",
			NewStatus:    "plan_applied",
		},
	})

	time.Sleep(50 * time.Millisecond)

	srv.pendingMu.RLock()
	_, ok := srv.pendingPlans["p1"]
	srv.pendingMu.RUnlock()
	if ok {
		t.Error("pendingPlans should be cleared on plan_applied")
	}
}

func TestStartSwarmEventsPendingPlanNotClearedOnOtherStatus(t *testing.T) {
	bus := events.New()
	board := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(board, nil, nil, ":0")
	srv.bus = bus
	srv.pendingPlans = make(map[string]*pendingPlanEntry)

	srv.pendingMu.Lock()
	srv.pendingPlans["p1"] = &pendingPlanEntry{
		PlanPath:    "/tmp/plan.json",
		ReplySocket: "/tmp/reply.sock",
		CreatedAt:   time.Now(),
	}
	srv.pendingMu.Unlock()

	srv.StartSwarmEvents()
	time.Sleep(50 * time.Millisecond)

	bus.Publish(events.Event{
		Type: events.EventSwarmChanged,
		Payload: events.SwarmChangedPayload{
			ParentTaskID: "p1",
			NewStatus:    "dispatched",
		},
	})

	time.Sleep(50 * time.Millisecond)

	srv.pendingMu.RLock()
	_, ok := srv.pendingPlans["p1"]
	srv.pendingMu.RUnlock()
	if !ok {
		t.Error("pendingPlans should NOT be cleared on dispatched")
	}
}

func TestSwarmServiceUnavailable(t *testing.T) {
	board := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}
	srv := New(board, nil, nil, ":0") // swarm is nil

	body := `{"parent_task_id":"t","working_dir":"/"}`
	req := httptest.NewRequest("POST", "/api/swarm/start", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}
