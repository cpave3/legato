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
	tickets     []RemoteTicket
	transitions []RemoteTransition
	searchErr   error
	transErr    error
	doTransErr  error
	doTransCalls []doTransCall
}

type doTransCall struct {
	ID           string
	TransitionID string
}

func (m *mockProvider) Search(_ context.Context, _ string) ([]RemoteTicket, error) {
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

	svc := NewSyncService(s, bus, provider, "project = TEST", 60*time.Second).(*syncService)
	return svc, s, provider, bus
}

// Task 3.1: Pull sync — insert new tickets
func TestPullSyncInsertNew(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()

	provider.tickets = []RemoteTicket{
		{
			ID: "TEST-1", Summary: "First ticket",
			DescriptionMD: "Some description",
			Status: "To Do", Priority: "High", IssueType: "Task",
			Assignee: "alice", Labels: []string{"backend"},
			URL: "https://jira.example.com/browse/TEST-1",
			UpdatedAt: time.Now(),
		},
	}

	result, err := svc.Sync(ctx)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.TicketsSynced != 1 {
		t.Errorf("synced = %d, want 1", result.TicketsSynced)
	}

	ticket, err := s.GetTicket(ctx, "TEST-1")
	if err != nil {
		t.Fatalf("get ticket: %v", err)
	}
	if ticket.Summary != "First ticket" {
		t.Errorf("summary = %q", ticket.Summary)
	}
	if ticket.DescriptionMD != "Some description" {
		t.Errorf("description_md = %q", ticket.DescriptionMD)
	}
	if ticket.Priority != "High" {
		t.Errorf("priority = %q", ticket.Priority)
	}
}

// Task 3.1: Pull sync — update changed ticket
func TestPullSyncUpdateExisting(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()

	oldTime := time.Now().Add(-1 * time.Hour)
	newTime := time.Now()

	// Insert existing ticket
	s.CreateTicket(ctx, store.Ticket{
		ID: "TEST-1", Summary: "Old summary",
		Status: "Backlog", RemoteStatus: "To Do",
		CreatedAt: oldTime.Format(time.RFC3339),
		UpdatedAt: oldTime.Format(time.RFC3339),
		RemoteUpdatedAt: oldTime.Format(time.RFC3339),
	})

	// Provider returns updated version
	provider.tickets = []RemoteTicket{
		{
			ID: "TEST-1", Summary: "New summary",
			Status: "To Do", Priority: "High",
			UpdatedAt: newTime,
		},
	}

	_, err := svc.Sync(ctx)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	ticket, err := s.GetTicket(ctx, "TEST-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ticket.Summary != "New summary" {
		t.Errorf("summary = %q, want 'New summary'", ticket.Summary)
	}
}

// Task 3.2: Status-to-column mapping on insert
func TestPullSyncColumnMapping(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()

	provider.tickets = []RemoteTicket{
		{ID: "TEST-1", Summary: "Backlog ticket", Status: "To Do", UpdatedAt: time.Now()},
		{ID: "TEST-2", Summary: "Doing ticket", Status: "In Progress", UpdatedAt: time.Now()},
		{ID: "TEST-3", Summary: "Done ticket", Status: "Done", UpdatedAt: time.Now()},
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
		ticket, err := s.GetTicket(ctx, tt.id)
		if err != nil {
			t.Fatalf("get %s: %v", tt.id, err)
		}
		if ticket.Status != tt.column {
			t.Errorf("%s: status = %q, want %q", tt.id, ticket.Status, tt.column)
		}
	}
}

// Task 3.3: Column update when Jira status changed externally
func TestPullSyncColumnUpdateOnStatusChange(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()

	oldTime := time.Now().Add(-1 * time.Hour)
	newTime := time.Now()

	// Insert ticket in Backlog column
	s.CreateTicket(ctx, store.Ticket{
		ID: "TEST-1", Summary: "Test",
		Status: "Backlog", RemoteStatus: "To Do",
		CreatedAt: oldTime.Format(time.RFC3339),
		UpdatedAt: oldTime.Format(time.RFC3339),
		RemoteUpdatedAt: oldTime.Format(time.RFC3339),
	})

	// Provider shows it moved to "In Progress" on Jira side
	provider.tickets = []RemoteTicket{
		{ID: "TEST-1", Summary: "Test", Status: "In Progress", UpdatedAt: newTime},
	}

	_, err := svc.Sync(ctx)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	ticket, err := s.GetTicket(ctx, "TEST-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ticket.Status != "Doing" {
		t.Errorf("status = %q, want 'Doing'", ticket.Status)
	}
}

// Task 3.4: Stale ticket detection
func TestPullSyncStaleTickets(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()

	now := time.Now()

	// Insert a ticket that won't appear in next sync
	s.CreateTicket(ctx, store.Ticket{
		ID: "TEST-OLD", Summary: "Old ticket",
		Status: "Backlog", RemoteStatus: "To Do",
		CreatedAt: now.Format(time.RFC3339),
		UpdatedAt: now.Format(time.RFC3339),
		RemoteUpdatedAt: now.Format(time.RFC3339),
	})

	// Provider returns no tickets
	provider.tickets = []RemoteTicket{}

	_, err := svc.Sync(ctx)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	// Ticket should be marked stale but still exist
	ticket, err := s.GetTicket(ctx, "TEST-OLD")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ticket.StaleAt == nil || *ticket.StaleAt == "" {
		t.Error("ticket should be marked stale")
	}
}

// Task 3.3: Column NOT updated when local move is within conflict window
func TestPullSyncLocalMoveWithinConflictWindow(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()

	now := time.Now()

	// Insert ticket that was moved locally 1 minute ago
	moveAt := now.Add(-1 * time.Minute).Format(time.RFC3339)
	s.CreateTicket(ctx, store.Ticket{
		ID: "TEST-1", Summary: "Test",
		Status: "Doing", RemoteStatus: "To Do",
		LocalMoveAt: &moveAt,
		CreatedAt: now.Add(-1 * time.Hour).Format(time.RFC3339),
		UpdatedAt: now.Format(time.RFC3339),
		RemoteUpdatedAt: now.Add(-1 * time.Hour).Format(time.RFC3339),
	})

	// Provider still shows it as "To Do" — conflict with local move
	provider.tickets = []RemoteTicket{
		{ID: "TEST-1", Summary: "Test", Status: "To Do", UpdatedAt: now},
	}

	_, err := svc.Sync(ctx)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	ticket, err := s.GetTicket(ctx, "TEST-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// Local should win — ticket stays in "Doing"
	if ticket.Status != "Doing" {
		t.Errorf("status = %q, want 'Doing' (local wins)", ticket.Status)
	}
}

// Task 5.1: Local-wins outside conflict window
func TestConflictResolutionOutsideWindow(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()
	now := time.Now()

	// Ticket was moved locally 10 minutes ago (outside 5-min window)
	moveAt := now.Add(-10 * time.Minute).Format(time.RFC3339)
	s.CreateTicket(ctx, store.Ticket{
		ID: "TEST-1", Summary: "Test",
		Status: "Doing", RemoteStatus: "To Do",
		LocalMoveAt:     &moveAt,
		CreatedAt:       now.Add(-1 * time.Hour).Format(time.RFC3339),
		UpdatedAt:       now.Format(time.RFC3339),
		RemoteUpdatedAt: now.Add(-1 * time.Hour).Format(time.RFC3339),
	})

	provider.tickets = []RemoteTicket{
		{ID: "TEST-1", Summary: "Test", Status: "To Do", UpdatedAt: now},
	}

	svc.Sync(ctx)

	ticket, _ := s.GetTicket(ctx, "TEST-1")
	// Remote should win — ticket goes back to "Backlog"
	if ticket.Status != "Backlog" {
		t.Errorf("status = %q, want 'Backlog' (remote wins after window)", ticket.Status)
	}
}

// Task 5.2: Conflict logging
func TestConflictLogging(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()
	now := time.Now()

	// Ticket moved locally 1 minute ago (inside window)
	moveAt := now.Add(-1 * time.Minute).Format(time.RFC3339)
	s.CreateTicket(ctx, store.Ticket{
		ID: "TEST-1", Summary: "Test",
		Status: "Doing", RemoteStatus: "To Do",
		LocalMoveAt:     &moveAt,
		CreatedAt:       now.Add(-1 * time.Hour).Format(time.RFC3339),
		UpdatedAt:       now.Format(time.RFC3339),
		RemoteUpdatedAt: now.Add(-1 * time.Hour).Format(time.RFC3339),
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

// Task 5.3: Periodic sync events
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

// Task 5.4: Offline resilience
func TestSyncOfflineResilience(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()
	now := time.Now()

	// Pre-populate with data
	s.CreateTicket(ctx, store.Ticket{
		ID: "TEST-1", Summary: "Cached ticket",
		Status: "Backlog", RemoteStatus: "To Do",
		CreatedAt:       now.Format(time.RFC3339),
		UpdatedAt:       now.Format(time.RFC3339),
		RemoteUpdatedAt: now.Format(time.RFC3339),
	})

	// Simulate network failure
	provider.searchErr = fmt.Errorf("network error: connection refused")

	_, err := svc.Sync(ctx)
	if err == nil {
		t.Fatal("expected sync error on network failure")
	}

	// Board data should still be accessible from SQLite
	ticket, getErr := s.GetTicket(ctx, "TEST-1")
	if getErr != nil {
		t.Fatalf("ticket should still be accessible: %v", getErr)
	}
	if ticket.Summary != "Cached ticket" {
		t.Error("cached data should be preserved")
	}
}

// Phase 6: Error event publishing tests
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

	now := time.Now()
	s.CreateTicket(ctx, store.Ticket{
		ID: "TEST-1", Summary: "Test",
		Status: "Backlog", RemoteStatus: "To Do",
		CreatedAt:       now.Format(time.RFC3339),
		UpdatedAt:       now.Format(time.RFC3339),
		RemoteUpdatedAt: now.Format(time.RFC3339),
	})

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

// Task 4.1: Push sync — async transition
func TestPushSyncCardMove(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()

	now := time.Now()

	// Create a ticket in Backlog
	s.CreateTicket(ctx, store.Ticket{
		ID: "TEST-1", Summary: "Test",
		Status: "Backlog", RemoteStatus: "To Do",
		CreatedAt: now.Format(time.RFC3339),
		UpdatedAt: now.Format(time.RFC3339),
		RemoteUpdatedAt: now.Format(time.RFC3339),
	})

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
	ticket, err := s.GetTicket(ctx, "TEST-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ticket.Status != "Doing" {
		t.Errorf("status = %q, want Doing", ticket.Status)
	}
	if ticket.LocalMoveAt == nil {
		t.Error("local_move_at should be set")
	}
}

// Task 4.2: Push success — update jira_status and sync_log
func TestPushSyncSuccess(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()

	now := time.Now()
	s.CreateTicket(ctx, store.Ticket{
		ID: "TEST-1", Summary: "Test",
		Status: "Backlog", RemoteStatus: "To Do",
		CreatedAt: now.Format(time.RFC3339),
		UpdatedAt: now.Format(time.RFC3339),
		RemoteUpdatedAt: now.Format(time.RFC3339),
	})

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

	ticket, err := s.GetTicket(ctx, "TEST-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	// After successful push, jira_status should update
	if ticket.RemoteStatus != "In Progress" {
		t.Errorf("jira_status = %q, want 'In Progress'", ticket.RemoteStatus)
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

// Task 4.3: Push failure — preserve column, set warning
func TestPushSyncFailure(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()

	now := time.Now()
	s.CreateTicket(ctx, store.Ticket{
		ID: "TEST-1", Summary: "Test",
		Status: "Backlog", RemoteStatus: "To Do",
		CreatedAt: now.Format(time.RFC3339),
		UpdatedAt: now.Format(time.RFC3339),
		RemoteUpdatedAt: now.Format(time.RFC3339),
	})

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

	ticket, err := s.GetTicket(ctx, "TEST-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	// Card should stay in the moved column (user intent preserved)
	if ticket.Status != "Doing" {
		t.Errorf("status = %q, want Doing (preserved)", ticket.Status)
	}

	// jira_status should NOT be updated
	if ticket.RemoteStatus != "To Do" {
		t.Errorf("jira_status = %q, want 'To Do' (unchanged)", ticket.RemoteStatus)
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

// Task 4.4: Retry failed pushes on manual sync
func TestPushRetryOnManualSync(t *testing.T) {
	svc, s, provider, _ := newTestSync(t)
	ctx := context.Background()

	now := time.Now()
	s.CreateTicket(ctx, store.Ticket{
		ID: "TEST-1", Summary: "Test",
		Status: "Doing", RemoteStatus: "To Do",
		CreatedAt: now.Format(time.RFC3339),
		UpdatedAt: now.Format(time.RFC3339),
		RemoteUpdatedAt: now.Format(time.RFC3339),
	})

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
