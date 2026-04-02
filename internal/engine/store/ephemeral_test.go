package store

import (
	"context"
	"testing"
	"time"
)

func TestCreateEphemeralTask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Seed a column so CreateEphemeralTask can find it
	if err := s.CreateColumnMapping(ctx, ColumnMapping{
		ColumnName: "Backlog", SortOrder: 0,
	}); err != nil {
		t.Fatal(err)
	}

	id, err := s.CreateEphemeralTask(ctx, "poking around")
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 8 {
		t.Errorf("expected 8-char ID, got %q", id)
	}

	task, err := s.GetTask(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if task.Title != "poking around" {
		t.Errorf("Title = %q, want %q", task.Title, "poking around")
	}
	if !task.Ephemeral {
		t.Error("expected Ephemeral = true")
	}
	if task.Status != "Backlog" {
		t.Errorf("Status = %q, want %q", task.Status, "Backlog")
	}
}

func TestEphemeralTaskExcludedFromListByStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.CreateColumnMapping(ctx, ColumnMapping{
		ColumnName: "Backlog", SortOrder: 0,
	}); err != nil {
		t.Fatal(err)
	}

	// Create a regular task
	now := time.Now().UTC().Format(time.RFC3339)
	if err := s.CreateTask(ctx, Task{
		ID: "regular1", Title: "Regular task", Status: "Backlog",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	// Create an ephemeral task
	_, err := s.CreateEphemeralTask(ctx, "ephemeral session")
	if err != nil {
		t.Fatal(err)
	}

	// ListTasksByStatus should only return the regular task
	tasks, err := s.ListTasksByStatus(ctx, "Backlog")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].ID != "regular1" {
		t.Errorf("expected regular1, got %s", tasks[0].ID)
	}
}

func TestEphemeralTaskExcludedFromWorkspaceQueries(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.CreateColumnMapping(ctx, ColumnMapping{
		ColumnName: "Backlog", SortOrder: 0,
	}); err != nil {
		t.Fatal(err)
	}

	// Create an ephemeral task
	_, err := s.CreateEphemeralTask(ctx, "ephemeral")
	if err != nil {
		t.Fatal(err)
	}

	// Unassigned view
	tasks, err := s.ListTasksByStatusAndWorkspace(ctx, "Backlog", WorkspaceView{Kind: ViewUnassigned})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks in unassigned view, got %d", len(tasks))
	}

	// All view
	tasks, err = s.ListTasksByStatusAndWorkspace(ctx, "Backlog", WorkspaceView{Kind: ViewAll})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks in all view, got %d", len(tasks))
	}
}
