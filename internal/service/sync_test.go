package service

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// mockProvider implements TicketProvider for testing.
type mockProvider struct {
	tickets      []RemoteTicket
	transitions  []RemoteTransition
	searchErr    error
	searchFn     func(query string) ([]RemoteTicket, error)
	transErr     error
	doTransErr   error
	doTransCalls []doTransCall
}

type doTransCall struct {
	ID           string
	TransitionID string
}

func (m *mockProvider) Search(_ context.Context, query string) ([]RemoteTicket, error) {
	if m.searchFn != nil {
		return m.searchFn(query)
	}
	if m.searchErr != nil {
		return nil, m.searchErr
	}
	return m.tickets, nil
}

func (m *mockProvider) GetTicket(_ context.Context, id string) (*RemoteTicket, error) {
	for _, t := range m.tickets {
		if t.ID == id {
			return &t, nil
		}
	}
	return nil, nil
}

func (m *mockProvider) ListTransitions(_ context.Context, _ string) ([]RemoteTransition, error) {
	if m.transErr != nil {
		return nil, m.transErr
	}
	return m.transitions, nil
}

func (m *mockProvider) DoTransition(_ context.Context, id string, transitionID string) error {
	m.doTransCalls = append(m.doTransCalls, doTransCall{ID: id, TransitionID: transitionID})
	return m.doTransErr
}

func newTestSync(t *testing.T) (*syncService, *store.Store, *mockProvider, *events.Bus) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	bus := events.New()
	provider := &mockProvider{}

	// Seed column mappings
	ctx := context.Background()
	mappings := []store.ColumnMapping{
		{ColumnName: "Backlog", RemoteStatuses: `["To Do","Open","Backlog"]`, SortOrder: 0},
		{ColumnName: "Doing", RemoteStatuses: `["In Progress"]`, SortOrder: 1},
		{ColumnName: "Done", RemoteStatuses: `["Done","Closed"]`, SortOrder: 2},
	}
	for _, m := range mappings {
		if err := s.CreateColumnMapping(ctx, m); err != nil {
			t.Fatalf("mapping: %v", err)
		}
	}

	svc := NewSyncService(s, bus, provider, "project = TEST", []string{"TEST"}, 60*time.Second).(*syncService)
	return svc, s, provider, bus
}

// helper to create a synced task with remote_meta
func createSyncedTask(t *testing.T, s *store.Store, id, title, status, remoteStatus string, updatedAt time.Time, extras map[string]string) {
	t.Helper()
	ctx := context.Background()
	provider := "jira"
	meta := map[string]string{
		"remote_status":     remoteStatus,
		"remote_updated_at": updatedAt.UTC().Format(time.RFC3339),
	}
	for k, v := range extras {
		meta[k] = v
	}
	metaJSON, _ := json.Marshal(meta)
	metaStr := string(metaJSON)
	now := time.Now().UTC().Format(time.RFC3339)

	err := s.CreateTask(ctx, store.Task{
		ID: id, Title: title, Status: status,
		Provider: &provider, RemoteID: &id, RemoteMeta: &metaStr,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func getRemoteMeta(t *testing.T, s *store.Store, id string) map[string]string {
	t.Helper()
	task, err := s.GetTask(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]string
	if task.RemoteMeta != nil {
		json.Unmarshal([]byte(*task.RemoteMeta), &meta)
	}
	return meta
}

// Pull sync — insert new tasks
func TestPullSyncSkipsNewTasks(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()

	provider.tickets = []RemoteTicket{
		{
			ID: "TEST-1", Summary: "First task",
			Status: "To Do", Priority: "High",
			UpdatedAt: time.Now(),
		},
	}

	result, err := svc.Sync(ctx)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.TasksSynced != 0 {
		t.Errorf("synced = %d, want 0 (pull should not insert new tasks)", result.TasksSynced)
	}

	// Task should NOT exist locally
	_, err = s.GetTask(ctx, "TEST-1")
	if err == nil {
		t.Error("expected task to not exist — pull sync should skip unknown tasks")
	}
}

// Pull sync — update changed task
func TestPullSyncUpdateExisting(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()

	oldTime := time.Now().Add(-1 * time.Hour)
	newTime := time.Now()

	createSyncedTask(t, s, "TEST-1", "Old title", "Backlog", "To Do", oldTime, nil)

	// Provider returns updated version
	provider.tickets = []RemoteTicket{
		{
			ID: "TEST-1", Summary: "New title",
			Status: "To Do", Priority: "High",
			UpdatedAt: newTime,
		},
	}

	_, err := svc.Sync(ctx)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	task, err := s.GetTask(ctx, "TEST-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if task.Title != "New title" {
		t.Errorf("title = %q, want 'New title'", task.Title)
	}
}

// Status-to-column mapping on update
func TestPullSyncColumnMapping(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()

	// Pre-create tasks so pull sync can update them
	oldTime := time.Now().Add(-1 * time.Hour)
	createSyncedTask(t, s, "TEST-1", "Backlog task", "Backlog", "To Do", oldTime, nil)
	createSyncedTask(t, s, "TEST-2", "Doing task", "Backlog", "To Do", oldTime, nil)
	createSyncedTask(t, s, "TEST-3", "Done task", "Backlog", "To Do", oldTime, nil)

	provider.tickets = []RemoteTicket{
		{ID: "TEST-1", Summary: "Backlog task", Status: "To Do", UpdatedAt: time.Now()},
		{ID: "TEST-2", Summary: "Doing task", Status: "In Progress", UpdatedAt: time.Now()},
		{ID: "TEST-3", Summary: "Done task", Status: "Done", UpdatedAt: time.Now()},
	}

	_, err := svc.Sync(ctx)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	tests := []struct {
		id     string
		column string
	}{
		{"TEST-1", "Backlog"},
		{"TEST-2", "Doing"},
		{"TEST-3", "Done"},
	}
	for _, tt := range tests {
		task, err := s.GetTask(ctx, tt.id)
		if err != nil {
			t.Fatalf("get %s: %v", tt.id, err)
		}
		if task.Status != tt.column {
			t.Errorf("%s: status = %q, want %q", tt.id, task.Status, tt.column)
		}
	}
}

// Column update when Jira status changed externally
func TestPullSyncColumnUpdateOnStatusChange(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()

	oldTime := time.Now().Add(-1 * time.Hour)
	newTime := time.Now()

	createSyncedTask(t, s, "TEST-1", "Test", "Backlog", "To Do", oldTime, nil)

	// Provider shows it moved to "In Progress" on Jira side
	provider.tickets = []RemoteTicket{
		{ID: "TEST-1", Summary: "Test", Status: "In Progress", UpdatedAt: newTime},
	}

	_, err := svc.Sync(ctx)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	task, err := s.GetTask(ctx, "TEST-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if task.Status != "Doing" {
		t.Errorf("status = %q, want 'Doing'", task.Status)
	}
}

// Stale task detection
func TestPullSyncStaleTasks(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()

	now := time.Now()

	// Insert a task that won't appear in next sync
	createSyncedTask(t, s, "TEST-OLD", "Old task", "Backlog", "To Do", now, nil)

	// Provider returns no tickets
	provider.tickets = []RemoteTicket{}

	_, err := svc.Sync(ctx)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	// Task should be marked stale via remote_meta
	meta := getRemoteMeta(t, s, "TEST-OLD")
	if meta["stale_at"] == "" {
		t.Error("task should be marked stale in remote_meta")
	}
}

// Column NOT updated when local move is within conflict window
func TestPullSyncLocalMoveWithinConflictWindow(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()

	now := time.Now()
	moveAt := now.Add(-1 * time.Minute).Format(time.RFC3339)

	createSyncedTask(t, s, "TEST-1", "Test", "Doing", "To Do", now.Add(-1*time.Hour), map[string]string{
		"local_move_at": moveAt,
	})

	// Provider still shows it as "To Do" — conflict with local move
	provider.tickets = []RemoteTicket{
		{ID: "TEST-1", Summary: "Test", Status: "To Do", UpdatedAt: now},
	}

	_, err := svc.Sync(ctx)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	task, err := s.GetTask(ctx, "TEST-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// Local should win — task stays in "Doing"
	if task.Status != "Doing" {
		t.Errorf("status = %q, want 'Doing' (local wins)", task.Status)
	}
}

// Local-wins outside conflict window
func TestConflictResolutionOutsideWindow(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()
	now := time.Now()

	moveAt := now.Add(-10 * time.Minute).Format(time.RFC3339)

	createSyncedTask(t, s, "TEST-1", "Test", "Doing", "To Do", now.Add(-1*time.Hour), map[string]string{
		"local_move_at": moveAt,
	})

	provider.tickets = []RemoteTicket{
		{ID: "TEST-1", Summary: "Test", Status: "To Do", UpdatedAt: now},
	}

	svc.Sync(ctx)

	task, _ := s.GetTask(ctx, "TEST-1")
	// Remote should win — task goes back to "Backlog"
	if task.Status != "Backlog" {
		t.Errorf("status = %q, want 'Backlog' (remote wins after window)", task.Status)
	}
}

// Conflict logging
func TestConflictLogging(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()
	now := time.Now()

	moveAt := now.Add(-1 * time.Minute).Format(time.RFC3339)

	createSyncedTask(t, s, "TEST-1", "Test", "Doing", "To Do", now.Add(-1*time.Hour), map[string]string{
		"local_move_at": moveAt,
	})

	provider.tickets = []RemoteTicket{
		{ID: "TEST-1", Summary: "Test", Status: "Done", UpdatedAt: now},
	}

	svc.Sync(ctx)

	logs, _ := s.ListSyncLogs(ctx, "TEST-1")
	found := false
	for _, l := range logs {
		if l.Action == "conflict_local_wins" {
			found = true
		}
	}
	if !found {
		t.Error("expected conflict_local_wins entry in sync_log")
	}
}

// Periodic sync events
func TestSyncPublishesEvents(t *testing.T) {
	svc, _, provider, bus := newTestSync(t)
	ctx := context.Background()

	started := bus.Subscribe(events.EventSyncStarted)
	completed := bus.Subscribe(events.EventSyncCompleted)

	provider.tickets = []RemoteTicket{}

	svc.Sync(ctx)

	select {
	case <-started:
		// OK
	default:
		t.Error("expected SyncStarted event")
	}

	select {
	case <-completed:
		// OK
	default:
		t.Error("expected SyncCompleted event")
	}
}

// Offline resilience
func TestSyncOfflineResilience(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()

	createSyncedTask(t, s, "TEST-1", "Cached task", "Backlog", "To Do", time.Now(), nil)

	// Simulate network failure
	provider.searchErr = fmt.Errorf("network error: connection refused")

	_, err := svc.Sync(ctx)
	if err == nil {
		t.Fatal("expected sync error on network failure")
	}

	// Board data should still be accessible from SQLite
	task, getErr := s.GetTask(ctx, "TEST-1")
	if getErr != nil {
		t.Fatalf("task should still be accessible: %v", getErr)
	}
	if task.Title != "Cached task" {
		t.Error("cached data should be preserved")
	}
}

// Error event publishing tests
func TestSyncFailurePublishesSyncErrorEvent(t *testing.T) {
	svc, _, provider, bus := newTestSync(t)
	ctx := context.Background()

	errCh := bus.Subscribe(events.EventSyncError)
	provider.searchErr = fmt.Errorf("network error: connection refused")

	_, err := svc.Sync(ctx)
	if err == nil {
		t.Fatal("expected sync error")
	}

	select {
	case got := <-errCh:
		p, ok := got.Payload.(events.ErrorPayload)
		if !ok {
			t.Fatalf("expected ErrorPayload, got %T", got.Payload)
		}
		if p.ErrorType != "offline" {
			t.Errorf("ErrorType = %q, want offline", p.ErrorType)
		}
	default:
		t.Error("expected EventSyncError event")
	}
}

func TestPushFailurePublishesTransitionFailedEvent(t *testing.T) {
	svc, s, provider, bus := newTestSync(t)
	ctx := context.Background()

	createSyncedTask(t, s, "TEST-1", "Test", "Backlog", "To Do", time.Now(), nil)

	mappings, _ := s.ListColumnMappings(ctx)
	for _, m := range mappings {
		if m.ColumnName == "Doing" {
			m.RemoteTransition = "21"
			s.UpdateColumnMapping(ctx, m)
		}
	}

	errCh := bus.Subscribe(events.EventTransitionFailed)
	provider.doTransErr = fmt.Errorf("transition not available")

	svc.PushMove(ctx, "TEST-1", "Doing")
	time.Sleep(100 * time.Millisecond)

	select {
	case got := <-errCh:
		p, ok := got.Payload.(events.ErrorPayload)
		if !ok {
			t.Fatalf("expected ErrorPayload, got %T", got.Payload)
		}
		if p.TicketKey != "TEST-1" {
			t.Errorf("TicketKey = %q, want TEST-1", p.TicketKey)
		}
		if p.ErrorType != "transition_failed" {
			t.Errorf("ErrorType = %q, want transition_failed", p.ErrorType)
		}
	default:
		t.Error("expected EventTransitionFailed event")
	}
}

// SearchRemote tests

func TestSearchRemoteReturnsMatchingTickets(t *testing.T) {
	svc, _, provider, _ := newTestSync(t)
	svc.projectKeys = []string{"TEST"}
	ctx := context.Background()

	provider.tickets = []RemoteTicket{
		{ID: "TEST-10", Summary: "Fix login bug", Status: "To Do", Priority: "High", IssueType: "Bug"},
		{ID: "TEST-11", Summary: "Add search feature", Status: "In Progress", Priority: "Medium", IssueType: "Story"},
	}

	results, err := svc.SearchRemote(ctx, "login")
	if err != nil {
		t.Fatalf("SearchRemote: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results (mock returns all), got %d", len(results))
	}
	if results[0].ID != "TEST-10" {
		t.Errorf("results[0].ID = %q, want TEST-10", results[0].ID)
	}
	if results[0].Summary != "Fix login bug" {
		t.Errorf("results[0].Summary = %q", results[0].Summary)
	}
}

func TestSearchRemoteBuildsJQLWithProjectKeys(t *testing.T) {
	svc, _, provider, _ := newTestSync(t)
	svc.projectKeys = []string{"REX", "INF"}
	ctx := context.Background()

	// Capture the query sent to the provider
	var capturedQuery string
	provider.searchFn = func(query string) ([]RemoteTicket, error) {
		capturedQuery = query
		return nil, nil
	}

	svc.SearchRemote(ctx, "auth bug")

	if !strings.Contains(capturedQuery, "REX") || !strings.Contains(capturedQuery, "INF") {
		t.Errorf("JQL should contain project keys, got: %s", capturedQuery)
	}
	if !strings.Contains(capturedQuery, "auth bug") {
		t.Errorf("JQL should contain search text, got: %s", capturedQuery)
	}
	if !strings.Contains(capturedQuery, "summary ~") {
		t.Errorf("JQL should use summary search, got: %s", capturedQuery)
	}
	if !strings.Contains(capturedQuery, "ORDER BY") {
		t.Errorf("JQL should have ORDER BY, got: %s", capturedQuery)
	}
}

func TestSearchRemoteSkipsShortQuery(t *testing.T) {
	svc, _, provider, _ := newTestSync(t)
	ctx := context.Background()

	called := false
	provider.searchFn = func(query string) ([]RemoteTicket, error) {
		called = true
		return nil, nil
	}

	results, err := svc.SearchRemote(ctx, "a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Error("expected nil results for short query")
	}
	if called {
		t.Error("should not call provider for single-char query")
	}
}

// ImportRemoteTask tests

func TestImportRemoteTaskCreatesLocalTask(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()

	provider.tickets = []RemoteTicket{
		{
			ID: "TEST-42", Summary: "Important feature",
			DescriptionMD: "## Details\nBuild it.",
			Status: "In Progress", Priority: "High", IssueType: "Story",
			Assignee: "bob", Labels: []string{"frontend"},
			URL: "https://jira.example.com/browse/TEST-42",
			UpdatedAt: time.Now(),
		},
	}

	card, err := svc.ImportRemoteTask(ctx, "TEST-42")
	if err != nil {
		t.Fatalf("ImportRemoteTask: %v", err)
	}
	if card.ID != "TEST-42" {
		t.Errorf("card.ID = %q, want TEST-42", card.ID)
	}
	if card.Title != "Important feature" {
		t.Errorf("card.Title = %q", card.Title)
	}

	// Verify it's in the DB with provider metadata
	task, err := s.GetTask(ctx, "TEST-42")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Provider == nil || *task.Provider != "jira" {
		t.Error("expected provider = jira")
	}
	if task.Status != "Doing" { // "In Progress" maps to "Doing"
		t.Errorf("status = %q, want Doing", task.Status)
	}
}

func TestImportRemoteTaskAlreadyTracked(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()

	// Pre-create the task
	createSyncedTask(t, s, "TEST-42", "Already here", "Backlog", "To Do", time.Now(), nil)

	provider.tickets = []RemoteTicket{
		{ID: "TEST-42", Summary: "Already here", Status: "To Do", UpdatedAt: time.Now()},
	}

	card, err := svc.ImportRemoteTask(ctx, "TEST-42")
	if err != nil {
		t.Fatalf("ImportRemoteTask: %v", err)
	}
	// Should return the existing card without error
	if card.ID != "TEST-42" {
		t.Errorf("card.ID = %q", card.ID)
	}
}

func TestImportRemoteTaskNotFound(t *testing.T) {
	svc, _, _, _ := newTestSync(t)
	ctx := context.Background()

	// Provider returns nil for unknown ticket
	_, err := svc.ImportRemoteTask(ctx, "NOPE-99")
	if err == nil {
		t.Fatal("expected error for unknown ticket")
	}
}

func statusToColumn(status string, mappings []store.ColumnMapping) string {
	for _, m := range mappings {
		var statuses []string
		json.Unmarshal([]byte(m.RemoteStatuses), &statuses)
		for _, s := range statuses {
			if strings.EqualFold(s, status) {
				return m.ColumnName
			}
		}
	}
	return ""
}

// Push sync — async transition
func TestPushSyncCardMove(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()

	createSyncedTask(t, s, "TEST-1", "Test", "Backlog", "To Do", time.Now(), nil)

	// Set up transition mapping: Doing column uses transition ID "21"
	mappings, _ := s.ListColumnMappings(ctx)
	for _, m := range mappings {
		if m.ColumnName == "Doing" {
			m.RemoteTransition = "21"
			s.UpdateColumnMapping(ctx, m)
		}
	}

	provider.transitions = []RemoteTransition{
		{ID: "21", Name: "Start Progress", TargetStatus: "In Progress"},
	}

	// Push the move
	err := svc.PushMove(ctx, "TEST-1", "Doing")
	if err != nil {
		t.Fatalf("push: %v", err)
	}

	// SQLite should be updated immediately
	task, err := s.GetTask(ctx, "TEST-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if task.Status != "Doing" {
		t.Errorf("status = %q, want Doing", task.Status)
	}

	// local_move_at should be set in remote_meta
	meta := getRemoteMeta(t, s, "TEST-1")
	if meta["local_move_at"] == "" {
		t.Error("local_move_at should be set in remote_meta")
	}
}

// Push success — update remote_status in remote_meta and sync_log
func TestPushSyncSuccess(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()

	createSyncedTask(t, s, "TEST-1", "Test", "Backlog", "To Do", time.Now(), nil)

	mappings, _ := s.ListColumnMappings(ctx)
	for _, m := range mappings {
		if m.ColumnName == "Doing" {
			m.RemoteTransition = "21"
			s.UpdateColumnMapping(ctx, m)
		}
	}

	provider.transitions = []RemoteTransition{
		{ID: "21", Name: "Start Progress", TargetStatus: "In Progress"},
	}

	svc.PushMove(ctx, "TEST-1", "Doing")

	// Wait for async transition to complete
	time.Sleep(100 * time.Millisecond)

	// After successful push, remote_status in remote_meta should update
	meta := getRemoteMeta(t, s, "TEST-1")
	if meta["remote_status"] != "In Progress" {
		t.Errorf("remote_status = %q, want 'In Progress'", meta["remote_status"])
	}

	// Sync log should have a success entry
	logs, _ := s.ListSyncLogs(ctx, "TEST-1")
	found := false
	for _, l := range logs {
		if l.Action == "push_success" {
			found = true
		}
	}
	if !found {
		t.Error("expected push_success in sync_log")
	}
}

// Push failure — preserve column, set warning
func TestPushSyncFailure(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()

	createSyncedTask(t, s, "TEST-1", "Test", "Backlog", "To Do", time.Now(), nil)

	mappings, _ := s.ListColumnMappings(ctx)
	for _, m := range mappings {
		if m.ColumnName == "Doing" {
			m.RemoteTransition = "21"
			s.UpdateColumnMapping(ctx, m)
		}
	}

	provider.doTransErr = fmt.Errorf("transition failed: API error")

	svc.PushMove(ctx, "TEST-1", "Doing")

	// Wait for async transition to complete
	time.Sleep(100 * time.Millisecond)

	task, err := s.GetTask(ctx, "TEST-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	// Card should stay in the moved column (user intent preserved)
	if task.Status != "Doing" {
		t.Errorf("status = %q, want Doing (preserved)", task.Status)
	}

	// remote_status should NOT be updated
	meta := getRemoteMeta(t, s, "TEST-1")
	if meta["remote_status"] != "To Do" {
		t.Errorf("remote_status = %q, want 'To Do' (unchanged)", meta["remote_status"])
	}

	// Sync log should have a failure entry
	logs, _ := s.ListSyncLogs(ctx, "TEST-1")
	found := false
	for _, l := range logs {
		if l.Action == "push_failed" {
			found = true
		}
	}
	if !found {
		t.Error("expected push_failed in sync_log")
	}
}

// Retry failed pushes on manual sync
func TestPushRetryOnManualSync(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()

	now := time.Now()
	createSyncedTask(t, s, "TEST-1", "Test", "Doing", "To Do", now, nil)

	mappings, _ := s.ListColumnMappings(ctx)
	for _, m := range mappings {
		if m.ColumnName == "Doing" {
			m.RemoteTransition = "21"
			s.UpdateColumnMapping(ctx, m)
		}
	}

	// First push fails
	provider.doTransErr = fmt.Errorf("network error")
	svc.PushMove(ctx, "TEST-1", "Doing")
	time.Sleep(100 * time.Millisecond)

	// Now fix the error and retry via sync
	provider.doTransErr = nil
	provider.tickets = []RemoteTicket{
		{ID: "TEST-1", Summary: "Test", Status: "To Do", UpdatedAt: now},
	}
	svc.Sync(ctx)
	time.Sleep(100 * time.Millisecond)

	// Check that retry was attempted
	if len(provider.doTransCalls) < 2 {
		t.Errorf("expected at least 2 transition calls (initial + retry), got %d", len(provider.doTransCalls))
	}
}

// Local task push — should not trigger remote transition
func TestPushLocalTaskNoRemoteTransition(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	// Create a local task (no provider)
	s.CreateTask(ctx, store.Task{
		ID: "local1", Title: "Local task", Status: "Backlog",
		CreatedAt: now, UpdatedAt: now,
	})

	mappings, _ := s.ListColumnMappings(ctx)
	for _, m := range mappings {
		if m.ColumnName == "Doing" {
			m.RemoteTransition = "21"
			s.UpdateColumnMapping(ctx, m)
		}
	}

	err := svc.PushMove(ctx, "local1", "Doing")
	if err != nil {
		t.Fatalf("push: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// No remote transition should have been attempted
	if len(provider.doTransCalls) != 0 {
		t.Errorf("expected 0 transition calls for local task, got %d", len(provider.doTransCalls))
	}

	// Task should still be moved locally
	task, _ := s.GetTask(ctx, "local1")
	if task.Status != "Doing" {
		t.Errorf("status = %q, want Doing", task.Status)
	}
}
