package store

import (
	"context"
	"testing"
)

func TestColumnMappingCRUDCycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Create
	m := ColumnMapping{
		ColumnName:   "Backlog",
		RemoteStatuses: `["To Do","Open"]`,
		SortOrder:    0,
	}
	if err := s.CreateColumnMapping(ctx, m); err != nil {
		t.Fatal(err)
	}

	m2 := ColumnMapping{
		ColumnName:   "Doing",
		RemoteStatuses: `["In Progress"]`,
		SortOrder:    1,
	}
	if err := s.CreateColumnMapping(ctx, m2); err != nil {
		t.Fatal(err)
	}

	// List — ordered by sort_order
	mappings, err := s.ListColumnMappings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(mappings) != 2 {
		t.Fatalf("got %d mappings, want 2", len(mappings))
	}
	if mappings[0].ColumnName != "Backlog" {
		t.Errorf("first mapping = %q, want %q", mappings[0].ColumnName, "Backlog")
	}

	// Update
	mappings[0].RemoteStatuses = `["To Do","Open","Backlog"]`
	if err := s.UpdateColumnMapping(ctx, mappings[0]); err != nil {
		t.Fatal(err)
	}
	updated, err := s.ListColumnMappings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if updated[0].RemoteStatuses != `["To Do","Open","Backlog"]` {
		t.Errorf("RemoteStatuses = %q after update", updated[0].RemoteStatuses)
	}

	// Delete
	if err := s.DeleteColumnMapping(ctx, mappings[0].ID); err != nil {
		t.Fatal(err)
	}
	remaining, err := s.ListColumnMappings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 {
		t.Errorf("got %d mappings after delete, want 1", len(remaining))
	}
}

func TestDuplicateColumnNameReturnsError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	m := ColumnMapping{ColumnName: "Backlog", RemoteStatuses: `["To Do"]`}
	if err := s.CreateColumnMapping(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateColumnMapping(ctx, m); err == nil {
		t.Error("expected constraint error for duplicate column_name")
	}
}
