package store

import (
	"context"
	"testing"
	"time"
)

func TestArchiveTask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	task := Task{
		ID: "t1", Title: "Task 1", Status: "Done",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatal(err)
	}

	if err := s.ArchiveTask(ctx, "t1"); err != nil {
		t.Fatalf("ArchiveTask: %v", err)
	}

	got, err := s.GetTask(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ArchivedAt == nil {
		t.Fatal("expected ArchivedAt to be set")
	}
}

func TestArchiveTask_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	err := s.ArchiveTask(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
}

func TestArchiveTask_AlreadyArchived(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	task := Task{
		ID: "t1", Title: "Task 1", Status: "Done",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatal(err)
	}

	if err := s.ArchiveTask(ctx, "t1"); err != nil {
		t.Fatal(err)
	}
	// Second call should be a no-op, not an error
	if err := s.ArchiveTask(ctx, "t1"); err != nil {
		t.Fatalf("expected no error for already-archived task, got: %v", err)
	}
}

func TestArchiveTasksByStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	tasks := []Task{
		{ID: "t1", Title: "Done 1", Status: "Done", CreatedAt: now, UpdatedAt: now},
		{ID: "t2", Title: "Done 2", Status: "Done", CreatedAt: now, UpdatedAt: now},
		{ID: "t3", Title: "In Progress", Status: "Doing", CreatedAt: now, UpdatedAt: now},
	}
	for _, tk := range tasks {
		if err := s.CreateTask(ctx, tk); err != nil {
			t.Fatal(err)
		}
	}

	count, err := s.ArchiveTasksByStatus(ctx, "Done")
	if err != nil {
		t.Fatalf("ArchiveTasksByStatus: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 archived, got %d", count)
	}

	// Doing task should not be archived
	doing, _ := s.GetTask(ctx, "t3")
	if doing.ArchivedAt != nil {
		t.Fatal("Doing task should not be archived")
	}
}

func TestArchiveTasksByStatus_SkipsAlreadyArchived(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	tasks := []Task{
		{ID: "t1", Title: "Done 1", Status: "Done", CreatedAt: now, UpdatedAt: now},
		{ID: "t2", Title: "Done 2", Status: "Done", CreatedAt: now, UpdatedAt: now},
	}
	for _, tk := range tasks {
		if err := s.CreateTask(ctx, tk); err != nil {
			t.Fatal(err)
		}
	}

	// Archive first task
	if err := s.ArchiveTask(ctx, "t1"); err != nil {
		t.Fatal(err)
	}

	count, err := s.ArchiveTasksByStatus(ctx, "Done")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 newly archived, got %d", count)
	}
}

func TestListTasksByStatus_ExcludesArchived(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	tasks := []Task{
		{ID: "t1", Title: "Active", Status: "Done", CreatedAt: now, UpdatedAt: now},
		{ID: "t2", Title: "Archived", Status: "Done", CreatedAt: now, UpdatedAt: now},
	}
	for _, tk := range tasks {
		if err := s.CreateTask(ctx, tk); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.ArchiveTask(ctx, "t2"); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListTasksByStatus(ctx, "Done")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 task, got %d", len(got))
	}
	if got[0].ID != "t1" {
		t.Fatalf("expected t1, got %s", got[0].ID)
	}
}

func TestListTasksByStatusAndWorkspace_ExcludesArchived(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	// Create a workspace
	wsID, err := s.CreateWorkspace(ctx, Workspace{Name: "ws1", SortOrder: 0})
	if err != nil {
		t.Fatal(err)
	}

	tasks := []Task{
		{ID: "t1", Title: "Active", Status: "Done", WorkspaceID: &wsID, CreatedAt: now, UpdatedAt: now},
		{ID: "t2", Title: "Archived", Status: "Done", WorkspaceID: &wsID, CreatedAt: now, UpdatedAt: now},
	}
	for _, tk := range tasks {
		if err := s.CreateTask(ctx, tk); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.ArchiveTask(ctx, "t2"); err != nil {
		t.Fatal(err)
	}

	// Test ViewWorkspace
	got, err := s.ListTasksByStatusAndWorkspace(ctx, "Done", WorkspaceView{Kind: ViewWorkspace, WorkspaceID: wsID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("ViewWorkspace: expected 1 task, got %d", len(got))
	}

	// Test ViewUnassigned — archived task with no workspace
	unassigned := Task{ID: "t3", Title: "Unassigned", Status: "Done", CreatedAt: now, UpdatedAt: now}
	if err := s.CreateTask(ctx, unassigned); err != nil {
		t.Fatal(err)
	}
	archivedUnassigned := Task{ID: "t4", Title: "Archived Unassigned", Status: "Done", CreatedAt: now, UpdatedAt: now}
	if err := s.CreateTask(ctx, archivedUnassigned); err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveTask(ctx, "t4"); err != nil {
		t.Fatal(err)
	}

	got, err = s.ListTasksByStatusAndWorkspace(ctx, "Done", WorkspaceView{Kind: ViewUnassigned})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("ViewUnassigned: expected 1 task, got %d", len(got))
	}
}
