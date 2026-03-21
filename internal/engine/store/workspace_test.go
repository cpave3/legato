package store

import (
	"context"
	"testing"
	"time"
)

func TestCreateAndGetWorkspace(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	color := "#4A9EEF"
	id, err := s.CreateWorkspace(ctx, Workspace{Name: "Work", Color: &color, SortOrder: 0})
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("expected non-zero workspace ID")
	}

	w, err := s.GetWorkspace(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if w.Name != "Work" {
		t.Errorf("name = %q, want %q", w.Name, "Work")
	}
	if w.Color == nil || *w.Color != "#4A9EEF" {
		t.Errorf("color = %v, want %q", w.Color, "#4A9EEF")
	}
}

func TestCreateWorkspace_DuplicateName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.CreateWorkspace(ctx, Workspace{Name: "Work"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateWorkspace(ctx, Workspace{Name: "Work"})
	if err == nil {
		t.Fatal("expected uniqueness error for duplicate name")
	}
}

func TestListWorkspaces_Ordered(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.CreateWorkspace(ctx, Workspace{Name: "Personal", SortOrder: 1})
	s.CreateWorkspace(ctx, Workspace{Name: "Work", SortOrder: 0})

	ws, err := s.ListWorkspaces(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(ws))
	}
	if ws[0].Name != "Work" || ws[1].Name != "Personal" {
		t.Errorf("expected [Work, Personal], got [%s, %s]", ws[0].Name, ws[1].Name)
	}
}

func TestEnsureWorkspace_InsertsNew(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.EnsureWorkspace(ctx, Workspace{Name: "Work", SortOrder: 0})
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("expected non-zero ID")
	}

	ws, _ := s.ListWorkspaces(ctx)
	if len(ws) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(ws))
	}
}

func TestEnsureWorkspace_UpdatesExisting(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	color1 := "#111"
	s.EnsureWorkspace(ctx, Workspace{Name: "Work", Color: &color1, SortOrder: 0})

	color2 := "#222"
	s.EnsureWorkspace(ctx, Workspace{Name: "Work", Color: &color2, SortOrder: 5})

	ws, _ := s.ListWorkspaces(ctx)
	if len(ws) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(ws))
	}
	if ws[0].Color == nil || *ws[0].Color != "#222" {
		t.Errorf("color not updated: got %v", ws[0].Color)
	}
}

func TestGetWorkspace_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetWorkspace(ctx, 999)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestListTasksByStatusAndWorkspace(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	// Create workspaces
	wid1, _ := s.CreateWorkspace(ctx, Workspace{Name: "Work"})
	wid2, _ := s.CreateWorkspace(ctx, Workspace{Name: "Personal"})

	// Create tasks: 2 in Work, 1 in Personal, 1 unassigned — all "Backlog"
	s.CreateTask(ctx, Task{ID: "t1", Title: "Work 1", Status: "Backlog", WorkspaceID: &wid1, CreatedAt: now, UpdatedAt: now})
	s.CreateTask(ctx, Task{ID: "t2", Title: "Work 2", Status: "Backlog", WorkspaceID: &wid1, CreatedAt: now, UpdatedAt: now})
	s.CreateTask(ctx, Task{ID: "t3", Title: "Personal 1", Status: "Backlog", WorkspaceID: &wid2, CreatedAt: now, UpdatedAt: now})
	s.CreateTask(ctx, Task{ID: "t4", Title: "Unassigned", Status: "Backlog", CreatedAt: now, UpdatedAt: now})

	// ViewAll: returns all 4
	tasks, _ := s.ListTasksByStatusAndWorkspace(ctx, "Backlog", WorkspaceView{Kind: ViewAll})
	if len(tasks) != 4 {
		t.Errorf("ViewAll: expected 4 tasks, got %d", len(tasks))
	}

	// ViewWorkspace(Work): returns 2
	tasks, _ = s.ListTasksByStatusAndWorkspace(ctx, "Backlog", WorkspaceView{Kind: ViewWorkspace, WorkspaceID: wid1})
	if len(tasks) != 2 {
		t.Errorf("ViewWorkspace(Work): expected 2 tasks, got %d", len(tasks))
	}

	// ViewWorkspace(Personal): returns 1
	tasks, _ = s.ListTasksByStatusAndWorkspace(ctx, "Backlog", WorkspaceView{Kind: ViewWorkspace, WorkspaceID: wid2})
	if len(tasks) != 1 {
		t.Errorf("ViewWorkspace(Personal): expected 1 task, got %d", len(tasks))
	}

	// ViewUnassigned: returns 1
	tasks, _ = s.ListTasksByStatusAndWorkspace(ctx, "Backlog", WorkspaceView{Kind: ViewUnassigned})
	if len(tasks) != 1 {
		t.Errorf("ViewUnassigned: expected 1 task, got %d", len(tasks))
	}
	if tasks[0].ID != "t4" {
		t.Errorf("ViewUnassigned: expected t4, got %s", tasks[0].ID)
	}
}

func TestUpdateTaskWorkspace(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	wid, _ := s.CreateWorkspace(ctx, Workspace{Name: "Work"})
	s.CreateTask(ctx, Task{ID: "t1", Title: "Test", Status: "Backlog", CreatedAt: now, UpdatedAt: now})

	// Assign workspace
	if err := s.UpdateTaskWorkspace(ctx, "t1", &wid); err != nil {
		t.Fatal(err)
	}
	task, _ := s.GetTask(ctx, "t1")
	if task.WorkspaceID == nil || *task.WorkspaceID != wid {
		t.Errorf("expected workspace_id=%d, got %v", wid, task.WorkspaceID)
	}

	// Remove workspace
	if err := s.UpdateTaskWorkspace(ctx, "t1", nil); err != nil {
		t.Fatal(err)
	}
	task, _ = s.GetTask(ctx, "t1")
	if task.WorkspaceID != nil {
		t.Errorf("expected workspace_id=nil, got %v", task.WorkspaceID)
	}
}

func TestTaskWithWorkspaceID_CreateAndGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	wid, _ := s.CreateWorkspace(ctx, Workspace{Name: "Work"})
	err := s.CreateTask(ctx, Task{
		ID: "t1", Title: "Test", Status: "Backlog",
		WorkspaceID: &wid, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	task, err := s.GetTask(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if task.WorkspaceID == nil || *task.WorkspaceID != wid {
		t.Errorf("workspace_id = %v, want %d", task.WorkspaceID, wid)
	}
}
