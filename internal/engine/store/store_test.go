package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestNewCreatesDBAndTables(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sub", "dir", "test.db")
	s, err := New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Verify tables exist by querying sqlite_master
	var tables []string
	err = s.db.Select(&tables, "SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{"tickets": true, "column_mappings": true, "sync_log": true}
	for _, tbl := range tables {
		delete(want, tbl)
	}
	for tbl := range want {
		t.Errorf("missing table: %s", tbl)
	}
}

func TestCreateAndGetTicket(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	ticket := Ticket{
		ID:             "REX-1234",
		Summary:        "Fix auth bug",
		Status:         "Backlog",
		RemoteStatus:     "To Do",
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:      time.Now().UTC().Format(time.RFC3339),
		RemoteUpdatedAt:  time.Now().UTC().Format(time.RFC3339),
	}

	if err := s.CreateTicket(ctx, ticket); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetTicket(ctx, "REX-1234")
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "Fix auth bug" {
		t.Errorf("Summary = %q, want %q", got.Summary, "Fix auth bug")
	}
	if got.Status != "Backlog" {
		t.Errorf("Status = %q, want %q", got.Status, "Backlog")
	}
}

func TestCreateDuplicateTicketReturnsError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	ticket := Ticket{
		ID: "REX-1", Summary: "A", Status: "Backlog", RemoteStatus: "To Do",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		RemoteUpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.CreateTicket(ctx, ticket); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTicket(ctx, ticket); err == nil {
		t.Error("expected error for duplicate ticket, got nil")
	}
}

func TestGetNonExistentTicketReturnsError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetTicket(ctx, "NOPE-999")
	if err == nil {
		t.Error("expected error for non-existent ticket, got nil")
	}
}

func TestListTicketsByStatusOrderedBySortOrder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	tickets := []Ticket{
		{ID: "A-3", Summary: "Third", Status: "Doing", RemoteStatus: "In Progress", SortOrder: 2, CreatedAt: now, UpdatedAt: now, RemoteUpdatedAt: now},
		{ID: "A-1", Summary: "First", Status: "Doing", RemoteStatus: "In Progress", SortOrder: 0, CreatedAt: now, UpdatedAt: now, RemoteUpdatedAt: now},
		{ID: "A-2", Summary: "Second", Status: "Doing", RemoteStatus: "In Progress", SortOrder: 1, CreatedAt: now, UpdatedAt: now, RemoteUpdatedAt: now},
		{ID: "B-1", Summary: "Other", Status: "Backlog", RemoteStatus: "To Do", SortOrder: 0, CreatedAt: now, UpdatedAt: now, RemoteUpdatedAt: now},
	}
	for _, tk := range tickets {
		if err := s.CreateTicket(ctx, tk); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.ListTicketsByStatus(ctx, "Doing")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d tickets, want 3", len(got))
	}
	if got[0].ID != "A-1" || got[1].ID != "A-2" || got[2].ID != "A-3" {
		t.Errorf("order = [%s, %s, %s], want [A-1, A-2, A-3]", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestUpdateTicketPersistsChanges(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	ticket := Ticket{
		ID: "REX-1", Summary: "Original", Status: "Backlog", RemoteStatus: "To Do",
		CreatedAt: now, UpdatedAt: now, RemoteUpdatedAt: now,
	}
	if err := s.CreateTicket(ctx, ticket); err != nil {
		t.Fatal(err)
	}

	ticket.Summary = "Updated"
	ticket.Status = "Doing"
	ticket.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.UpdateTicket(ctx, ticket); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetTicket(ctx, "REX-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "Updated" {
		t.Errorf("Summary = %q, want %q", got.Summary, "Updated")
	}
	if got.Status != "Doing" {
		t.Errorf("Status = %q, want %q", got.Status, "Doing")
	}
}

func TestDeleteTicketRemovesIt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	ticket := Ticket{
		ID: "REX-1", Summary: "Bye", Status: "Backlog", RemoteStatus: "To Do",
		CreatedAt: now, UpdatedAt: now, RemoteUpdatedAt: now,
	}
	if err := s.CreateTicket(ctx, ticket); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteTicket(ctx, "REX-1"); err != nil {
		t.Fatal(err)
	}

	_, err := s.GetTicket(ctx, "REX-1")
	if err == nil {
		t.Error("expected not-found error after delete")
	}
}

func TestDeleteNonExistentTicketIsNoop(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.DeleteTicket(ctx, "NOPE-1"); err != nil {
		t.Errorf("expected no error deleting non-existent ticket, got %v", err)
	}
}
