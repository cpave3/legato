package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

func setupTestBoard(t *testing.T) (*store.Store, *events.Bus, BoardService) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	bus := events.New()
	svc := NewBoardService(s, bus)
	return s, bus, svc
}

func seedColumns(t *testing.T, s *store.Store) {
	t.Helper()
	ctx := context.Background()
	cols := []store.ColumnMapping{
		{ColumnName: "Backlog", RemoteStatuses: `["To Do"]`, SortOrder: 0},
		{ColumnName: "In Progress", RemoteStatuses: `["In Progress"]`, SortOrder: 1},
		{ColumnName: "Done", RemoteStatuses: `["Done"]`, SortOrder: 2},
	}
	for _, c := range cols {
		if err := s.CreateColumnMapping(ctx, c); err != nil {
			t.Fatalf("creating column mapping: %v", err)
		}
	}
}

func seedTasks(t *testing.T, s *store.Store) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	tasks := []store.Task{
		{ID: "T-1", Title: "First task", Status: "Backlog",
			Priority: "High", SortOrder: 0, CreatedAt: now, UpdatedAt: now},
		{ID: "T-2", Title: "Second task", Status: "Backlog",
			Priority: "Medium", SortOrder: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "T-3", Title: "Third task", Status: "In Progress",
			Priority: "Low", SortOrder: 0, CreatedAt: now, UpdatedAt: now},
	}
	for _, tk := range tasks {
		if err := s.CreateTask(ctx, tk); err != nil {
			panic(err)
		}
	}
}

// ListColumns tests

func TestListColumns_ReturnsInOrder(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)

	cols, err := svc.ListColumns(context.Background())
	if err != nil {
		t.Fatalf("ListColumns: %v", err)
	}
	if len(cols) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(cols))
	}
	expected := []string{"Backlog", "In Progress", "Done"}
	for i, col := range cols {
		if col.Name != expected[i] {
			t.Errorf("column %d: expected %q, got %q", i, expected[i], col.Name)
		}
	}
}

func TestListColumns_EmptyReturnsEmptySlice(t *testing.T) {
	_, _, svc := setupTestBoard(t)

	cols, err := svc.ListColumns(context.Background())
	if err != nil {
		t.Fatalf("ListColumns: %v", err)
	}
	if len(cols) != 0 {
		t.Fatalf("expected 0 columns, got %d", len(cols))
	}
}

// ListCards tests

func TestListCards_ReturnsSorted(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)
	seedTasks(t, s)

	cards, err := svc.ListCards(context.Background(), "Backlog")
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(cards))
	}
	if cards[0].ID != "T-1" || cards[1].ID != "T-2" {
		t.Errorf("wrong order: got %s, %s", cards[0].ID, cards[1].ID)
	}
}

func TestListCards_EmptyColumn(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)

	cards, err := svc.ListCards(context.Background(), "Done")
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if len(cards) != 0 {
		t.Fatalf("expected 0 cards, got %d", len(cards))
	}
}

func TestListCards_InvalidColumn(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)

	_, err := svc.ListCards(context.Background(), "Nonexistent")
	if err == nil {
		t.Fatal("expected error for invalid column")
	}
}

// GetCard tests

func TestGetCard_Exists(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)
	seedTasks(t, s)

	card, err := svc.GetCard(context.Background(), "T-1")
	if err != nil {
		t.Fatalf("GetCard: %v", err)
	}
	if card.ID != "T-1" {
		t.Errorf("expected T-1, got %s", card.ID)
	}
	if card.Title != "First task" {
		t.Errorf("expected 'First task', got %q", card.Title)
	}
	if card.Priority != "High" {
		t.Errorf("expected High priority, got %q", card.Priority)
	}
}

func TestGetCard_NotFound(t *testing.T) {
	_, _, svc := setupTestBoard(t)

	card, err := svc.GetCard(context.Background(), "NOPE-1")
	if err == nil {
		t.Fatal("expected error for missing card")
	}
	if card != nil {
		t.Fatal("expected nil card")
	}
}

// MoveCard tests

func TestMoveCard_Success(t *testing.T) {
	s, bus, svc := setupTestBoard(t)
	seedColumns(t, s)
	seedTasks(t, s)

	// Subscribe to events before move
	ch := bus.Subscribe(events.EventCardMoved)

	err := svc.MoveCard(context.Background(), "T-1", "In Progress")
	if err != nil {
		t.Fatalf("MoveCard: %v", err)
	}

	// Verify card moved
	card, _ := svc.GetCard(context.Background(), "T-1")
	if card.Status != "In Progress" {
		t.Errorf("expected status 'In Progress', got %q", card.Status)
	}

	// Verify event published
	select {
	case evt := <-ch:
		if evt.Type != events.EventCardMoved {
			t.Errorf("expected EventCardMoved, got %d", evt.Type)
		}
	default:
		t.Error("expected EventCardMoved event")
	}
}

func TestMoveCard_SameColumn_NoOp(t *testing.T) {
	s, bus, svc := setupTestBoard(t)
	seedColumns(t, s)
	seedTasks(t, s)

	ch := bus.Subscribe(events.EventCardMoved)

	err := svc.MoveCard(context.Background(), "T-1", "Backlog")
	if err != nil {
		t.Fatalf("MoveCard: %v", err)
	}

	// No event should be published
	select {
	case <-ch:
		t.Error("no event expected for same-column move")
	default:
	}
}

func TestMoveCard_InvalidColumn(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)
	seedTasks(t, s)

	err := svc.MoveCard(context.Background(), "T-1", "Nonexistent")
	if err == nil {
		t.Fatal("expected error for invalid column")
	}

	// Card should not have changed
	card, _ := svc.GetCard(context.Background(), "T-1")
	if card.Status != "Backlog" {
		t.Errorf("card should still be in Backlog, got %q", card.Status)
	}
}

func TestMoveCard_PlacedAtEnd(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)
	seedTasks(t, s)

	// T-3 is already in "In Progress" at sort_order 0
	// Move T-1 to "In Progress" — should be at sort_order 1
	err := svc.MoveCard(context.Background(), "T-1", "In Progress")
	if err != nil {
		t.Fatalf("MoveCard: %v", err)
	}

	cards, _ := svc.ListCards(context.Background(), "In Progress")
	if len(cards) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(cards))
	}
	if cards[0].ID != "T-3" || cards[1].ID != "T-1" {
		t.Errorf("expected T-3, T-1 order, got %s, %s", cards[0].ID, cards[1].ID)
	}
}

// ReorderCard tests

func TestReorderCard_MoveToPosition(t *testing.T) {
	s, bus, svc := setupTestBoard(t)
	seedColumns(t, s)
	seedTasks(t, s)

	ch := bus.Subscribe(events.EventCardUpdated)

	// Move T-2 to position 0 (before T-1)
	err := svc.ReorderCard(context.Background(), "T-2", 0)
	if err != nil {
		t.Fatalf("ReorderCard: %v", err)
	}

	cards, _ := svc.ListCards(context.Background(), "Backlog")
	if cards[0].ID != "T-2" || cards[1].ID != "T-1" {
		t.Errorf("expected T-2, T-1 order, got %s, %s", cards[0].ID, cards[1].ID)
	}

	select {
	case evt := <-ch:
		if evt.Type != events.EventCardUpdated {
			t.Errorf("expected EventCardUpdated, got %d", evt.Type)
		}
	default:
		t.Error("expected EventCardUpdated event")
	}
}

func TestReorderCard_OutOfRange_PlacesAtEnd(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)
	seedTasks(t, s)

	err := svc.ReorderCard(context.Background(), "T-1", 100)
	if err != nil {
		t.Fatalf("ReorderCard: %v", err)
	}

	cards, _ := svc.ListCards(context.Background(), "Backlog")
	if cards[len(cards)-1].ID != "T-1" {
		t.Errorf("expected T-1 at end, got %s", cards[len(cards)-1].ID)
	}
}

// SearchCards tests

func TestSearchCards_ByKey(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)
	seedTasks(t, s)

	cards, err := svc.SearchCards(context.Background(), "T-1")
	if err != nil {
		t.Fatalf("SearchCards: %v", err)
	}
	if len(cards) != 1 || cards[0].ID != "T-1" {
		t.Errorf("expected 1 card T-1, got %d cards", len(cards))
	}
}

func TestSearchCards_ByTitle(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)
	seedTasks(t, s)

	cards, err := svc.SearchCards(context.Background(), "second")
	if err != nil {
		t.Fatalf("SearchCards: %v", err)
	}
	if len(cards) != 1 || cards[0].ID != "T-2" {
		t.Errorf("expected T-2, got %v", cards)
	}
}

func TestSearchCards_CaseInsensitive(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)
	seedTasks(t, s)

	cards, err := svc.SearchCards(context.Background(), "FIRST")
	if err != nil {
		t.Fatalf("SearchCards: %v", err)
	}
	if len(cards) != 1 || cards[0].ID != "T-1" {
		t.Errorf("expected T-1, got %v", cards)
	}
}

func TestSearchCards_EmptyQueryReturnsAll(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)
	seedTasks(t, s)

	cards, err := svc.SearchCards(context.Background(), "")
	if err != nil {
		t.Fatalf("SearchCards: %v", err)
	}
	if len(cards) != 3 {
		t.Errorf("expected 3 cards, got %d", len(cards))
	}
}

func TestSearchCards_NoMatch(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)
	seedTasks(t, s)

	cards, err := svc.SearchCards(context.Background(), "zzzzz")
	if err != nil {
		t.Fatalf("SearchCards: %v", err)
	}
	if len(cards) != 0 {
		t.Errorf("expected 0 cards, got %d", len(cards))
	}
}

// CreateTask tests

func TestCreateTask_Success(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)

	card, err := svc.CreateTask(context.Background(), "New task", "Backlog", "High")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if card.Title != "New task" {
		t.Errorf("Title = %q, want 'New task'", card.Title)
	}
	if card.Priority != "High" {
		t.Errorf("Priority = %q, want 'High'", card.Priority)
	}
	if card.Status != "Backlog" {
		t.Errorf("Status = %q, want 'Backlog'", card.Status)
	}
	if card.ID == "" {
		t.Error("expected non-empty ID")
	}

	// Verify persisted
	detail, err := svc.GetCard(context.Background(), card.ID)
	if err != nil {
		t.Fatalf("GetCard after create: %v", err)
	}
	if detail.Title != "New task" {
		t.Errorf("persisted Title = %q", detail.Title)
	}
	if detail.Provider != "" {
		t.Errorf("local task should have empty Provider, got %q", detail.Provider)
	}
}

// DeleteTask tests

func TestDeleteTask_LocalTask(t *testing.T) {
	s, bus, svc := setupTestBoard(t)
	seedColumns(t, s)
	seedTasks(t, s) // T-1, T-2 in Backlog; T-3 in In Progress (all local)

	ch := bus.Subscribe(events.EventCardsRefreshed)

	err := svc.DeleteTask(context.Background(), "T-1")
	if err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	// Task should be gone
	_, err = svc.GetCard(context.Background(), "T-1")
	if err == nil {
		t.Fatal("expected error after deleting task")
	}

	// Other tasks still exist
	cards, _ := svc.ListCards(context.Background(), "Backlog")
	if len(cards) != 1 || cards[0].ID != "T-2" {
		t.Errorf("expected only T-2 remaining, got %v", cards)
	}

	// Should publish refresh event
	select {
	case evt := <-ch:
		if evt.Type != events.EventCardsRefreshed {
			t.Errorf("expected EventCardsRefreshed, got %d", evt.Type)
		}
	default:
		t.Error("expected EventCardsRefreshed event")
	}
}

func TestDeleteTask_RemoteTask_RemovesLocalOnly(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)

	// Create a remote-tracking task
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	provider := "jira"
	remoteID := "REX-123"
	meta := `{"remote_status":"In Progress","issue_type":"Story"}`
	task := store.Task{
		ID: "REX-123", Title: "Remote task", Status: "Backlog",
		Priority: "High", SortOrder: 0, CreatedAt: now, UpdatedAt: now,
		Provider: &provider, RemoteID: &remoteID, RemoteMeta: &meta,
	}
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("seeding remote task: %v", err)
	}

	// Delete it — should succeed (removes local ref)
	err := svc.DeleteTask(ctx, "REX-123")
	if err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	// Task should be gone from local DB
	_, err = svc.GetCard(ctx, "REX-123")
	if err == nil {
		t.Fatal("expected error after deleting remote-tracking task")
	}
}

func TestDeleteTask_NotFound(t *testing.T) {
	_, _, svc := setupTestBoard(t)

	err := svc.DeleteTask(context.Background(), "NOPE-1")
	if err == nil {
		t.Fatal("expected error for missing task")
	}
}

func TestCreateTask_PlacedAtEndOfColumn(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)
	seedTasks(t, s)

	card, err := svc.CreateTask(context.Background(), "New task", "Backlog", "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	cards, _ := svc.ListCards(context.Background(), "Backlog")
	if cards[len(cards)-1].ID != card.ID {
		t.Errorf("new task should be at end of column")
	}
}
