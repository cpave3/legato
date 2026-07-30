package store

import (
	"context"
	"testing"
)

func TestSyncColumnMappingsReconcilesColumnsAndMovesRemovedTasksToUnmapped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for i, column := range []ColumnMapping{
		{ColumnName: "Backlog", RemoteStatuses: `["Old"]`},
		{ColumnName: "Doing", RemoteStatuses: `[]`},
		{ColumnName: "Done", RemoteStatuses: `[]`},
	} {
		column.SortOrder = i
		if err := s.CreateColumnMapping(ctx, column); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CreateTask(ctx, Task{ID: "task-1", Title: "Active", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}

	result, err := s.SyncColumnMappings(ctx, []ColumnMapping{
		{ColumnName: "Backlog", RemoteStatuses: `["To Do"]`},
		{ColumnName: "Triage", RemoteStatuses: `[]`},
		{ColumnName: "Unmapped", RemoteStatuses: `["Unknown"]`},
		{ColumnName: "Done", RemoteStatuses: `[]`},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback != "Unmapped" || result.TasksMoved != 1 {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Added) != 2 || result.Added[0] != "Triage" || len(result.Removed) != 1 || result.Removed[0] != "Doing" {
		t.Fatalf("result = %#v", result)
	}
	columns, err := s.ListColumnMappings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Backlog", "Triage", "Unmapped", "Done"}
	for i, column := range columns {
		if column.ColumnName != want[i] || column.SortOrder != i {
			t.Fatalf("column %d = %#v, want %q at order %d", i, column, want[i], i)
		}
	}
	task, err := s.GetTask(ctx, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "Unmapped" {
		t.Fatalf("task status = %q, want Unmapped", task.Status)
	}

	second, err := s.SyncColumnMappings(ctx, []ColumnMapping{
		{ColumnName: "Backlog", RemoteStatuses: `["To Do"]`},
		{ColumnName: "Triage", RemoteStatuses: `[]`},
		{ColumnName: "Unmapped", RemoteStatuses: `["Unknown"]`},
		{ColumnName: "Done", RemoteStatuses: `[]`},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Added) != 0 || len(second.Updated) != 0 || len(second.Removed) != 0 || second.TasksMoved != 0 {
		t.Fatalf("second sync was not idempotent: %#v", second)
	}
}

func TestSyncColumnMappingsUsesFirstColumnWhenUnmappedIsRemoved(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateColumnMapping(ctx, ColumnMapping{ColumnName: "Unmapped", RemoteStatuses: `[]`}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTask(ctx, Task{ID: "task-1", Title: "Unknown", Status: "Unmapped"}); err != nil {
		t.Fatal(err)
	}

	result, err := s.SyncColumnMappings(ctx, []ColumnMapping{
		{ColumnName: "Inbox", RemoteStatuses: `[]`},
		{ColumnName: "Done", RemoteStatuses: `[]`},
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.GetTask(ctx, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback != "Inbox" || task.Status != "Inbox" {
		t.Fatalf("result = %#v, task status = %q", result, task.Status)
	}
}

func TestSyncColumnMappingsRejectsInvalidConfigWithoutChangingBoard(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateColumnMapping(ctx, ColumnMapping{ColumnName: "Backlog", RemoteStatuses: `[]`}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SyncColumnMappings(ctx, []ColumnMapping{
		{ColumnName: "Doing"},
		{ColumnName: "doing"},
	}); err == nil {
		t.Fatal("expected duplicate column error")
	}
	columns, err := s.ListColumnMappings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(columns) != 1 || columns[0].ColumnName != "Backlog" {
		t.Fatalf("columns changed after rejected sync: %#v", columns)
	}
}
