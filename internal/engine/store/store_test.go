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

	want := map[string]bool{"tasks": true, "column_mappings": true, "sync_log": true}
	for _, tbl := range tables {
		delete(want, tbl)
	}
	for tbl := range want {
		t.Errorf("missing table: %s", tbl)
	}
}

func TestCreateAndGetTask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task := Task{
		ID:        "REX-1234",
		Title:     "Fix auth bug",
		Status:    "Backlog",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetTask(ctx, "REX-1234")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Fix auth bug" {
		t.Errorf("Title = %q, want %q", got.Title, "Fix auth bug")
	}
	if got.Status != "Backlog" {
		t.Errorf("Status = %q, want %q", got.Status, "Backlog")
	}
}

func TestCreateDuplicateTaskReturnsError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task := Task{
		ID: "REX-1", Title: "A", Status: "Backlog",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTask(ctx, task); err == nil {
		t.Error("expected error for duplicate task, got nil")
	}
}

func TestGetNonExistentTaskReturnsError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetTask(ctx, "NOPE-999")
	if err == nil {
		t.Error("expected error for non-existent task, got nil")
	}
}

func TestListTasksByStatusOrderedBySortOrder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	tasks := []Task{
		{ID: "A-3", Title: "Third", Status: "Doing", SortOrder: 2, CreatedAt: now, UpdatedAt: now},
		{ID: "A-1", Title: "First", Status: "Doing", SortOrder: 0, CreatedAt: now, UpdatedAt: now},
		{ID: "A-2", Title: "Second", Status: "Doing", SortOrder: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "B-1", Title: "Other", Status: "Backlog", SortOrder: 0, CreatedAt: now, UpdatedAt: now},
	}
	for _, tk := range tasks {
		if err := s.CreateTask(ctx, tk); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.ListTasksByStatus(ctx, "Doing")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d tasks, want 3", len(got))
	}
	if got[0].ID != "A-1" || got[1].ID != "A-2" || got[2].ID != "A-3" {
		t.Errorf("order = [%s, %s, %s], want [A-1, A-2, A-3]", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestUpdateTaskPersistsChanges(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	task := Task{
		ID: "REX-1", Title: "Original", Status: "Backlog",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatal(err)
	}

	task.Title = "Updated"
	task.Status = "Doing"
	task.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.UpdateTask(ctx, task); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetTask(ctx, "REX-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Updated" {
		t.Errorf("Title = %q, want %q", got.Title, "Updated")
	}
	if got.Status != "Doing" {
		t.Errorf("Status = %q, want %q", got.Status, "Doing")
	}
}

func TestDeleteTaskRemovesIt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	task := Task{
		ID: "REX-1", Title: "Bye", Status: "Backlog",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteTask(ctx, "REX-1"); err != nil {
		t.Fatal(err)
	}

	_, err := s.GetTask(ctx, "REX-1")
	if err == nil {
		t.Error("expected not-found error after delete")
	}
}

func TestDeleteNonExistentTaskIsNoop(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.DeleteTask(ctx, "NOPE-1"); err != nil {
		t.Errorf("expected no error deleting non-existent task, got %v", err)
	}
}

func TestUpsertTaskInsertsAndUpdates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	task := Task{
		ID: "REX-1", Title: "Original", Status: "Backlog",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.UpsertTask(ctx, task); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetTask(ctx, "REX-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Original" {
		t.Errorf("Title = %q, want %q", got.Title, "Original")
	}

	task.Title = "Updated"
	if err := s.UpsertTask(ctx, task); err != nil {
		t.Fatal(err)
	}

	got, err = s.GetTask(ctx, "REX-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Updated" {
		t.Errorf("Title = %q, want %q", got.Title, "Updated")
	}
}

func TestListAllTasks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	for _, id := range []string{"C-1", "A-1", "B-1"} {
		if err := s.CreateTask(ctx, Task{ID: id, Title: id, Status: "Backlog", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}

	tasks, err := s.ListAllTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("got %d tasks, want 3", len(tasks))
	}
	if tasks[0].ID != "A-1" {
		t.Errorf("first task = %q, want A-1 (ordered by id)", tasks[0].ID)
	}
}

func TestListTaskIDs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	for _, id := range []string{"C-1", "A-1", "B-1"} {
		if err := s.CreateTask(ctx, Task{ID: id, Title: id, Status: "Backlog", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}

	ids, err := s.ListTaskIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 {
		t.Fatalf("got %d ids, want 3", len(ids))
	}
	if ids[0] != "A-1" {
		t.Errorf("first id = %q, want A-1", ids[0])
	}
}

func TestTaskWithProviderFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	provider := "jira"
	remoteID := "REX-1234"
	remoteMeta := `{"remote_status":"In Progress","assignee":"cameron"}`

	task := Task{
		ID:         "REX-1234",
		Title:      "Synced task",
		Status:     "Doing",
		Provider:   &provider,
		RemoteID:   &remoteID,
		RemoteMeta: &remoteMeta,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetTask(ctx, "REX-1234")
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider == nil || *got.Provider != "jira" {
		t.Errorf("Provider = %v, want jira", got.Provider)
	}
	if got.RemoteID == nil || *got.RemoteID != "REX-1234" {
		t.Errorf("RemoteID = %v, want REX-1234", got.RemoteID)
	}
	if got.RemoteMeta == nil || *got.RemoteMeta != remoteMeta {
		t.Errorf("RemoteMeta = %v, want %s", got.RemoteMeta, remoteMeta)
	}
}

func TestLocalTaskHasNullProviderFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	task := Task{
		ID:        "abc12345",
		Title:     "Local task",
		Status:    "Backlog",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetTask(ctx, "abc12345")
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != nil {
		t.Errorf("Provider = %v, want nil for local task", got.Provider)
	}
	if got.RemoteID != nil {
		t.Errorf("RemoteID = %v, want nil for local task", got.RemoteID)
	}
	if got.RemoteMeta != nil {
		t.Errorf("RemoteMeta = %v, want nil for local task", got.RemoteMeta)
	}
}
