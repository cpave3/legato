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

func TestListCardsByWorkspacePreservesWorkspaceIdentity(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	ctx := context.Background()
	seedColumns(t, s)
	workspaceID, err := s.CreateWorkspace(ctx, store.Workspace{Name: "Platform"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTask(ctx, store.Task{ID: "LEG-42", Title: "Assigned", Status: "Backlog", WorkspaceID: &workspaceID}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTask(ctx, store.Task{ID: "LEG-43", Title: "Unassigned", Status: "Backlog"}); err != nil {
		t.Fatal(err)
	}

	assigned, err := svc.ListCardsByWorkspace(ctx, "Backlog", store.WorkspaceView{Kind: store.ViewWorkspace, WorkspaceID: workspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if len(assigned) != 1 || assigned[0].WorkspaceID == nil || *assigned[0].WorkspaceID != workspaceID {
		t.Fatalf("assigned cards = %+v, want workspace ID %d", assigned, workspaceID)
	}

	unassigned, err := svc.ListCardsByWorkspace(ctx, "Backlog", store.WorkspaceView{Kind: store.ViewUnassigned})
	if err != nil {
		t.Fatal(err)
	}
	if len(unassigned) != 1 || unassigned[0].WorkspaceID != nil {
		t.Fatalf("unassigned cards = %+v, want nil workspace ID", unassigned)
	}
}

func TestListArchivedTasksPreservesWorkspace(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	ctx := context.Background()
	color := "#123456"
	workspaceID, err := s.CreateWorkspace(ctx, store.Workspace{Name: "Platform", Color: &color})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTask(ctx, store.Task{ID: "LEG-44", Title: "Archived task", Status: "Done", WorkspaceID: &workspaceID}); err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveTask(ctx, "LEG-44"); err != nil {
		t.Fatal(err)
	}
	cards, err := svc.ListArchivedTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 || cards[0].WorkspaceName != "Platform" || cards[0].WorkspaceColor != "#123456" {
		t.Fatalf("cards = %+v", cards)
	}
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

func TestListCards_ReportsPersistedWorktree(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)
	seedTasks(t, s)
	meta := store.TaskWorktree{PrimaryDir: "/repo", Path: "/repo/.worktrees/t-1", Branch: "t-1", BaseBranch: "main"}
	if err := s.SetTaskWorktree(context.Background(), "T-1", &meta); err != nil {
		t.Fatal(err)
	}

	cards, err := svc.ListCards(context.Background(), "Backlog")
	if err != nil {
		t.Fatal(err)
	}
	if !cards[0].HasWorktree {
		t.Fatalf("card = %+v, want persisted worktree indicator", cards[0])
	}
	if cards[1].HasWorktree {
		t.Fatalf("card = %+v, want no worktree indicator", cards[1])
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

func TestGetCardIncludesPersistedWorktree(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)
	seedTasks(t, s)
	ctx := context.Background()
	want := &store.TaskWorktree{PrimaryDir: "/projects/app", Path: "/projects/app-t1", Branch: "t-1", BaseBranch: "main"}
	if err := s.SetTaskWorktree(ctx, "T-1", want); err != nil {
		t.Fatal(err)
	}

	card, err := svc.GetCard(ctx, "T-1")
	if err != nil {
		t.Fatal(err)
	}
	if card.Worktree == nil || *card.Worktree != *want {
		t.Fatalf("worktree = %#v, want %#v", card.Worktree, want)
	}
}

func TestGetCardReportsPersistedChimeraSession(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)
	seedTasks(t, s)
	ctx := context.Background()
	if err := s.SetTaskChimeraSessionID(ctx, "T-1", "chimera-session"); err != nil {
		t.Fatal(err)
	}

	card, err := svc.GetCard(ctx, "T-1")
	if err != nil {
		t.Fatal(err)
	}
	if !card.HasChimeraSession {
		t.Fatal("HasChimeraSession = false, want true")
	}
}

func TestGetCardIncludesTaskNotes(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)
	seedTasks(t, s)
	ctx := context.Background()
	if err := s.InsertTaskNote(ctx, store.TaskNote{ID: "tn-1", TaskID: "T-1", Body: "## Triage\n\nInvestigate retries.", Source: "workflow"}); err != nil {
		t.Fatal(err)
	}
	card, err := svc.GetCard(ctx, "T-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(card.Notes) != 1 || card.Notes[0].Body != "## Triage\n\nInvestigate retries." {
		t.Fatalf("notes = %#v", card.Notes)
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

	card, err := svc.CreateTask(context.Background(), "New task", "", "Backlog", "High", nil)
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

func TestCreateTask_WithDescription(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)

	card, err := svc.CreateTask(context.Background(), "New task", "Some description", "Backlog", "High", nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	detail, err := svc.GetCard(context.Background(), card.ID)
	if err != nil {
		t.Fatalf("GetCard: %v", err)
	}
	if detail.DescriptionMD != "Some description" {
		t.Errorf("DescriptionMD = %q, want 'Some description'", detail.DescriptionMD)
	}
}

// UpdateTaskDescription tests

func TestUpdateTaskDescription_Success(t *testing.T) {
	s, bus, svc := setupTestBoard(t)
	seedColumns(t, s)
	seedTasks(t, s) // T-1 is a local task

	ch := bus.Subscribe(events.EventCardsRefreshed)

	err := svc.UpdateTaskDescription(context.Background(), "T-1", "Updated description")
	if err != nil {
		t.Fatalf("UpdateTaskDescription: %v", err)
	}

	detail, _ := svc.GetCard(context.Background(), "T-1")
	if detail.DescriptionMD != "Updated description" {
		t.Errorf("DescriptionMD = %q, want 'Updated description'", detail.DescriptionMD)
	}

	select {
	case evt := <-ch:
		if evt.Type != events.EventCardsRefreshed {
			t.Errorf("expected EventCardsRefreshed, got %d", evt.Type)
		}
	default:
		t.Error("expected EventCardsRefreshed event")
	}
}

func TestUpdateTaskDescription_RemoteTaskUpdatesLegatoDetails(t *testing.T) {
	s, bus, svc := setupTestBoard(t)
	seedColumns(t, s)
	ctx := context.Background()
	provider := "jira"
	remoteID := "REX-123"
	task := store.Task{
		ID: "REX-123", Title: "Remote", Description: "", DescriptionMD: "old local",
		RemoteDescriptionMD: "Provider details", Status: "Backlog", Provider: &provider, RemoteID: &remoteID,
	}
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	ch := bus.Subscribe(events.EventCardsRefreshed)

	if err := svc.UpdateTaskDescription(ctx, task.ID, "new **local** detail"); err != nil {
		t.Fatal(err)
	}
	stored, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Description != "new **local** detail" || stored.DescriptionMD != "new **local** detail" || stored.RemoteDescriptionMD != "Provider details" {
		t.Fatalf("stored descriptions = %q / %q / %q", stored.Description, stored.DescriptionMD, stored.RemoteDescriptionMD)
	}
	detail, err := svc.GetCard(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := "Provider details\n\n## Legato details\n\nnew **local** detail"
	if detail.DescriptionMD != want || detail.LocalDescriptionMD != "new **local** detail" || detail.RemoteDescriptionMD != "Provider details" {
		t.Fatalf("detail descriptions = %q / %q / %q", detail.DescriptionMD, detail.LocalDescriptionMD, detail.RemoteDescriptionMD)
	}
	select {
	case <-ch:
	default:
		t.Fatal("expected cards-refreshed event")
	}

	if err := svc.UpdateTaskDescription(ctx, task.ID, ""); err != nil {
		t.Fatal(err)
	}
	detail, err = svc.GetCard(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.DescriptionMD != "Provider details" || detail.LocalDescriptionMD != "" {
		t.Fatalf("cleared detail = %#v", detail)
	}
}

func TestUpdateTaskDescription_NotFound(t *testing.T) {
	_, _, svc := setupTestBoard(t)

	err := svc.UpdateTaskDescription(context.Background(), "NOPE-1", "desc")
	if err == nil {
		t.Fatal("expected error for missing task")
	}
}

func TestCreateTask_PlacedAtEndOfColumn(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)
	seedTasks(t, s)

	card, err := svc.CreateTask(context.Background(), "New task", "", "Backlog", "", nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	cards, _ := svc.ListCards(context.Background(), "Backlog")
	if cards[len(cards)-1].ID != card.ID {
		t.Errorf("new task should be at end of column")
	}
}

func TestTaskMetadataUpdatesAreMergedAndReadable(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)
	seedTasks(t, s)
	metadata := svc.(TaskMetadataService)

	if err := metadata.SetTaskMetadata(context.Background(), "T-1", "triaged", true); err != nil {
		t.Fatal(err)
	}
	if err := metadata.SetTaskMetadata(context.Background(), "T-1", "repository.name", "legato"); err != nil {
		t.Fatal(err)
	}
	detail, err := svc.GetCard(context.Background(), "T-1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Metadata["triaged"] != true {
		t.Fatalf("metadata = %#v", detail.Metadata)
	}
	repository, ok := detail.Metadata["repository"].(map[string]any)
	if !ok || repository["name"] != "legato" {
		t.Fatalf("repository metadata = %#v", detail.Metadata["repository"])
	}
}

// UpdateTaskTitle tests

func TestUpdateTaskTitle_Success(t *testing.T) {
	s, bus, svc := setupTestBoard(t)
	seedColumns(t, s)
	seedTasks(t, s) // T-1 is a local task

	ch := bus.Subscribe(events.EventCardsRefreshed)

	err := svc.UpdateTaskTitle(context.Background(), "T-1", "Renamed task")
	if err != nil {
		t.Fatalf("UpdateTaskTitle: %v", err)
	}

	detail, _ := svc.GetCard(context.Background(), "T-1")
	if detail.Title != "Renamed task" {
		t.Errorf("Title = %q, want 'Renamed task'", detail.Title)
	}

	select {
	case evt := <-ch:
		if evt.Type != events.EventCardsRefreshed {
			t.Errorf("expected EventCardsRefreshed, got %d", evt.Type)
		}
	default:
		t.Error("expected EventCardsRefreshed event")
	}
}

func TestUpdateTaskTitle_RemoteTaskRejected(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)

	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	provider := "jira"
	remoteID := "REX-99"
	meta := `{"remote_status":"Open"}`
	task := store.Task{
		ID: "REX-99", Title: "Remote", Status: "Backlog",
		Priority: "High", SortOrder: 0, CreatedAt: now, UpdatedAt: now,
		Provider: &provider, RemoteID: &remoteID, RemoteMeta: &meta,
	}
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatal(err)
	}

	err := svc.UpdateTaskTitle(ctx, "REX-99", "New name")
	if err == nil {
		t.Fatal("expected error for remote task")
	}
}

func TestUpdateTaskTitle_NotFound(t *testing.T) {
	_, _, svc := setupTestBoard(t)

	err := svc.UpdateTaskTitle(context.Background(), "NOPE-1", "title")
	if err == nil {
		t.Fatal("expected error for missing task")
	}
}

func TestUpdateTaskTitle_EmptyRejected(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)
	seedTasks(t, s)

	err := svc.UpdateTaskTitle(context.Background(), "T-1", "")
	if err == nil {
		t.Fatal("expected error for empty title")
	}
}
