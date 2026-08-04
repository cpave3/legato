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

func TestSetTaskWorktreePersistsAndClearsMetadata(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "worktree-task")

	meta := TaskWorktree{PrimaryDir: "/projects/legato", Path: "/projects/legato.feature", Branch: "feature", BaseBranch: "main"}
	if err := s.SetTaskWorktree(ctx, "worktree-task", &meta); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTask(ctx, "worktree-task")
	if err != nil {
		t.Fatal(err)
	}
	if got.WorktreePath == nil || *got.WorktreePath != meta.Path || got.WorktreeBranch == nil || *got.WorktreeBranch != meta.Branch {
		t.Fatalf("stored worktree = %#v, want %#v", got, meta)
	}
	if got.WorktreePrimaryDir == nil || *got.WorktreePrimaryDir != meta.PrimaryDir || got.WorktreeBaseBranch == nil || *got.WorktreeBaseBranch != meta.BaseBranch {
		t.Fatalf("stored worktree directories/base = %#v, want %#v", got, meta)
	}

	if err := s.SetTaskWorktree(ctx, "worktree-task", nil); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetTask(ctx, "worktree-task")
	if err != nil {
		t.Fatal(err)
	}
	if got.WorktreePath != nil || got.WorktreePrimaryDir != nil || got.WorktreeBranch != nil || got.WorktreeBaseBranch != nil {
		t.Fatalf("worktree metadata not cleared: %#v", got)
	}
}

func TestSetTaskWorktreeReturnsNotFound(t *testing.T) {
	s := newTestStore(t)
	meta := TaskWorktree{Path: "/tmp/wt"}
	if err := s.SetTaskWorktree(context.Background(), "missing", &meta); err != ErrNotFound {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestNewSetsBusyTimeout(t *testing.T) {
	s := newTestStore(t)

	var timeout int
	if err := s.db.Get(&timeout, "PRAGMA busy_timeout"); err != nil {
		t.Fatal(err)
	}
	if timeout != 5000 {
		t.Errorf("busy_timeout = %d, want 5000", timeout)
	}
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

func TestRemoteDescriptionMDRoundTripsThroughTaskWrites(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := Task{ID: "REX-REMOTE", Title: "Remote", DescriptionMD: "Local details", RemoteDescriptionMD: "Provider details", Status: "Backlog"}

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DescriptionMD != "Local details" || got.RemoteDescriptionMD != "Provider details" {
		t.Fatalf("created descriptions = %q / %q", got.DescriptionMD, got.RemoteDescriptionMD)
	}

	got.RemoteDescriptionMD = "Provider update"
	if err := s.UpdateTask(ctx, *got); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DescriptionMD != "Local details" || got.RemoteDescriptionMD != "Provider update" {
		t.Fatalf("updated descriptions = %q / %q", got.DescriptionMD, got.RemoteDescriptionMD)
	}

	got.RemoteDescriptionMD = "Provider upsert"
	if err := s.UpsertTask(ctx, *got); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DescriptionMD != "Local details" || got.RemoteDescriptionMD != "Provider upsert" {
		t.Fatalf("upserted descriptions = %q / %q", got.DescriptionMD, got.RemoteDescriptionMD)
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

func TestTaskGroupSurvivesCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	group := "backend"
	task := Task{ID: "GROUP-1", Title: "Grouped", Status: "Doing", Group: &group}

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Group == nil || *got.Group != group {
		t.Fatalf("Group after create = %v, want %q", got.Group, group)
	}

	group = "frontend"
	got.Group = &group
	if err := s.UpdateTask(ctx, *got); err != nil {
		t.Fatal(err)
	}
	listed, err := s.ListTasksByStatus(ctx, "Doing")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Group == nil || *listed[0].Group != group {
		t.Fatalf("listed Group = %v, want %q", listed[0].Group, group)
	}

	got.Group = nil
	if err := s.UpdateTask(ctx, *got); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Group != nil {
		t.Fatalf("Group after clear = %q, want nil", *got.Group)
	}
}

func TestSetTaskChimeraSessionIDPersistsAndReplaces(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, Task{ID: "TASK-1", Title: "Task", Status: "Backlog"}); err != nil {
		t.Fatal(err)
	}

	for _, sessionID := range []string{"TASK-1", "TASK-1-01"} {
		if err := s.SetTaskChimeraSessionID(ctx, "TASK-1", sessionID); err != nil {
			t.Fatal(err)
		}
		task, err := s.GetTask(ctx, "TASK-1")
		if err != nil {
			t.Fatal(err)
		}
		if task.ChimeraSessionID == nil || *task.ChimeraSessionID != sessionID {
			t.Fatalf("ChimeraSessionID = %v, want %q", task.ChimeraSessionID, sessionID)
		}
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

func TestUpdateTaskFromProviderPreservesConcurrentLocalDescription(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	provider := "jira"
	task := Task{ID: "REX-SYNC", Title: "Old", Description: "Old local", DescriptionMD: "Old local", RemoteDescriptionMD: "Old remote", Status: "Backlog", Provider: &provider}
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	staleProviderSnapshot, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	localEdit, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	localEdit.Description = "Concurrent local"
	localEdit.DescriptionMD = "Concurrent local"
	if err := s.UpdateTask(ctx, *localEdit); err != nil {
		t.Fatal(err)
	}

	staleProviderSnapshot.Title = "New provider title"
	staleProviderSnapshot.RemoteDescriptionMD = "New remote"
	if err := s.UpdateTaskFromProvider(ctx, *staleProviderSnapshot); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DescriptionMD != "Concurrent local" || got.RemoteDescriptionMD != "New remote" || got.Title != "New provider title" {
		t.Fatalf("task after provider update = %#v", got)
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

func TestDeleteTaskPreservesExecutionHistory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "parent-1")
	start := time.Now().UTC().Add(-time.Minute)
	seedExecutionActivity(t, s, "exec-parent", "parent-1", ActivityWorking, "/repo", start, start.Add(time.Minute))
	if err := s.DeleteTask(ctx, "parent-1"); err != nil {
		t.Fatal(err)
	}
	var taskCount, executionCount int
	if err := s.db.Get(&taskCount, `SELECT COUNT(*) FROM tasks WHERE id='parent-1'`); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Get(&executionCount, `SELECT COUNT(*) FROM executions WHERE id='exec-parent' AND task_id='parent-1'`); err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 || executionCount != 1 {
		t.Fatalf("task count=%d execution count=%d", taskCount, executionCount)
	}
}

func TestDeleteTaskDetachesWorkflowAssociationsAndPreservesHistory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "subject-1")

	task, err := s.GetTask(ctx, "subject-1")
	if err != nil {
		t.Fatal(err)
	}
	task.Status = "Done"
	if err := s.RecordTaskTransition(ctx, TaskTransition{
		ID: "transition-1", TaskID: task.ID, Source: "local",
		FromColumn: "Doing", ToColumn: "Done", TaskJSON: `{}`,
	}, *task); err != nil {
		t.Fatal(err)
	}
	request, err := s.UpsertWorkflowTriggerRequest(ctx, WorkflowTriggerRequest{
		ID: "request-1", TransitionID: "transition-1", WorkflowName: "on-done",
		DefinitionSource: "test", DefinitionDigest: "sha256:test", DefinitionJSON: `{}`,
		InputsJSON: `{}`, MissingInputsJSON: `[]`, Status: "started", RunID: "run-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	run := WorkflowRun{
		ID: "run-1", WorkflowName: "on-done", WorkflowTitle: "On done",
		DefinitionSource: "test", DefinitionDigest: "sha256:test", DefinitionJSON: `{}`,
		InputsJSON: `{}`, Status: "running", CurrentStepID: "record",
		RelatedTaskID: task.ID, TriggerRequestID: request.ID,
	}
	steps := []WorkflowStepRun{{
		RunID: run.ID, StepID: "record", StepOrder: 0, Title: "Record",
		ExecutorType: "agent.run", DefinitionJSON: `{}`, Status: "pending",
	}}
	if err := s.CreateWorkflowRun(ctx, run, steps); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteTask(ctx, task.ID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if _, err := s.GetTask(ctx, task.ID); err != ErrNotFound {
		t.Fatalf("GetTask error = %v, want ErrNotFound", err)
	}
	if _, err := s.GetTaskTransition(ctx, "transition-1"); err != ErrNotFound {
		t.Fatalf("GetTaskTransition error = %v, want ErrNotFound", err)
	}
	if _, err := s.GetWorkflowTriggerRequest(ctx, request.ID); err != ErrNotFound {
		t.Fatalf("GetWorkflowTriggerRequest error = %v, want ErrNotFound", err)
	}
	view, err := s.GetWorkflowRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetWorkflowRun: %v", err)
	}
	if view.Run.RelatedTaskID != "" || view.Run.TriggerRequestID != "" {
		t.Fatalf("retained workflow associations = related %q, trigger %q", view.Run.RelatedTaskID, view.Run.TriggerRequestID)
	}
	if len(view.Steps) != 1 || view.Steps[0].StepID != "record" {
		t.Fatalf("retained workflow steps = %+v", view.Steps)
	}

	rows, err := s.db.QueryxContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign key violation remains after task deletion")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
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
