package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

func TestIntegration_BoardServiceEndToEnd(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "integration.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	bus := events.New()
	svc := NewBoardService(s, bus)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	// Seed columns
	for _, col := range []store.ColumnMapping{
		{ColumnName: "Backlog", RemoteStatuses: `["To Do"]`, SortOrder: 0},
		{ColumnName: "Doing", RemoteStatuses: `["In Progress"]`, SortOrder: 1},
		{ColumnName: "Done", RemoteStatuses: `["Done"]`, SortOrder: 2},
	} {
		if err := s.CreateColumnMapping(ctx, col); err != nil {
			t.Fatal(err)
		}
	}

	// Seed tickets
	for _, ticket := range []store.Ticket{
		{ID: "INT-1", Summary: "Alpha task", DescriptionMD: "Alpha description.\n\n## Details\n\nMore info here.",
			Status: "Backlog", RemoteStatus: "To Do", Priority: "High", IssueType: "Story",
			Assignee: "dev1", Labels: "backend", EpicKey: "INT-100", EpicName: "Integration Epic",
			URL: "https://jira.example.com/INT-1", CreatedAt: now, UpdatedAt: now, RemoteUpdatedAt: now, SortOrder: 0},
		{ID: "INT-2", Summary: "Beta task", DescriptionMD: "Beta description.",
			Status: "Backlog", RemoteStatus: "To Do", Priority: "Medium", IssueType: "Bug",
			CreatedAt: now, UpdatedAt: now, RemoteUpdatedAt: now, SortOrder: 1},
		{ID: "INT-3", Summary: "Gamma task", DescriptionMD: "Gamma description.",
			Status: "Doing", RemoteStatus: "In Progress", Priority: "Low", IssueType: "Task",
			CreatedAt: now, UpdatedAt: now, RemoteUpdatedAt: now, SortOrder: 0},
	} {
		if err := s.CreateTicket(ctx, ticket); err != nil {
			t.Fatal(err)
		}
	}

	// 1. ListColumns
	cols, err := svc.ListColumns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(cols))
	}

	// 2. ListCards
	cards, err := svc.ListCards(ctx, "Backlog")
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 {
		t.Fatalf("expected 2 backlog cards, got %d", len(cards))
	}

	// 3. GetCard
	detail, err := svc.GetCard(ctx, "INT-1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Summary != "Alpha task" {
		t.Fatalf("expected 'Alpha task', got %q", detail.Summary)
	}

	// 4. MoveCard
	if err := svc.MoveCard(ctx, "INT-1", "Doing"); err != nil {
		t.Fatal(err)
	}
	backlog, _ := svc.ListCards(ctx, "Backlog")
	doing, _ := svc.ListCards(ctx, "Doing")
	if len(backlog) != 1 {
		t.Errorf("expected 1 backlog card after move, got %d", len(backlog))
	}
	if len(doing) != 2 {
		t.Errorf("expected 2 doing cards after move, got %d", len(doing))
	}

	// 5. ReorderCard — move INT-1 to position 0 in Doing
	if err := svc.ReorderCard(ctx, "INT-1", 0); err != nil {
		t.Fatal(err)
	}
	doing, _ = svc.ListCards(ctx, "Doing")
	if doing[0].ID != "INT-1" {
		t.Errorf("expected INT-1 at position 0, got %s", doing[0].ID)
	}

	// 6. SearchCards
	results, err := svc.SearchCards(ctx, "beta")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != "INT-2" {
		t.Errorf("search for 'beta' returned %v", results)
	}

	// 7. ExportCardContext — description format
	descOut, err := svc.ExportCardContext(ctx, "INT-1", ExportFormatDescription)
	if err != nil {
		t.Fatal(err)
	}
	if descOut == "" {
		t.Error("description export should not be empty")
	}

	// 8. ExportCardContext — full format
	fullOut, err := svc.ExportCardContext(ctx, "INT-1", ExportFormatFull)
	if err != nil {
		t.Fatal(err)
	}
	if fullOut == "" {
		t.Error("full export should not be empty")
	}
}

func TestIntegration_SyncThenBoard(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sync_integration.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	bus := events.New()
	syncSvc := NewStubSyncService(s, bus)
	boardSvc := NewBoardService(s, bus)
	ctx := context.Background()

	// Sync seeds data
	result, err := syncSvc.Sync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.TicketsSynced < 8 {
		t.Fatalf("expected at least 8 synced, got %d", result.TicketsSynced)
	}

	// Board should see the synced data
	cols, err := boardSvc.ListColumns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) == 0 {
		t.Fatal("expected columns after sync")
	}

	total := 0
	for _, col := range cols {
		cards, err := boardSvc.ListCards(ctx, col.Name)
		if err != nil {
			t.Fatal(err)
		}
		total += len(cards)
	}
	if total != result.TicketsSynced {
		t.Errorf("board shows %d cards but sync reported %d", total, result.TicketsSynced)
	}

	// Can get detail for a synced card
	cards, _ := boardSvc.ListCards(ctx, cols[0].Name)
	if len(cards) > 0 {
		detail, err := boardSvc.GetCard(ctx, cards[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		if detail.ID == "" {
			t.Error("expected non-empty card detail")
		}
	}

	// Can export a synced card
	allCards, _ := boardSvc.SearchCards(ctx, "")
	if len(allCards) > 0 {
		out, err := boardSvc.ExportCardContext(ctx, allCards[0].ID, ExportFormatFull)
		if err != nil {
			t.Fatal(err)
		}
		if out == "" {
			t.Error("expected non-empty export")
		}
	}
}
