package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
)

func strptr(value string) *string {
	return &value
}

func TestTaskUpdateFieldsAppliesOneWriteAndRefresh(t *testing.T) {
	s := newInternalTestStore(t)
	seedInternalColumns(t, s)
	workspaceID, err := s.CreateWorkspace(context.Background(), store.Workspace{Name: "Personal"})
	if err != nil {
		t.Fatal(err)
	}
	seedInternalTask(t, s, store.Task{
		ID: "local-1", Title: "Before", Description: "Old", DescriptionMD: "Old", Status: "Backlog",
	})

	broadcasts := 0
	oldBroadcast := broadcastTaskUpdate
	broadcastTaskUpdate = func(msg ipc.Message) {
		broadcasts++
		if msg.TaskID != "local-1" || msg.Status != "Doing" {
			t.Errorf("broadcast = %#v", msg)
		}
	}
	t.Cleanup(func() { broadcastTaskUpdate = oldBroadcast })

	err = TaskUpdateFields(s, "local-1", TaskUpdateOptions{
		Status:      strptr("doing"),
		Title:       strptr("After"),
		Description: strptr("New details"),
		Workspace:   strptr("personal"),
	})
	if err != nil {
		t.Fatalf("TaskUpdateFields: %v", err)
	}

	task, err := s.GetTask(context.Background(), "local-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "Doing" || task.Title != "After" ||
		task.Description != "New details" || task.DescriptionMD != "New details" ||
		task.WorkspaceID == nil || *task.WorkspaceID != workspaceID {
		t.Errorf("task = %#v", task)
	}
	if broadcasts != 1 {
		t.Errorf("broadcasts = %d, want 1", broadcasts)
	}
}

func TestTaskUpdateFieldsInvalidStatusIsAtomic(t *testing.T) {
	s := newInternalTestStore(t)
	seedInternalColumns(t, s)
	seedInternalTask(t, s, store.Task{
		ID: "local-1", Title: "Before", Description: "Old", DescriptionMD: "Old", Status: "Backlog",
	})

	err := TaskUpdateFields(s, "local-1", TaskUpdateOptions{
		Status: strptr("Missing"), Title: strptr("After"), Description: strptr("New"),
	})
	if err == nil {
		t.Fatal("expected invalid status error")
	}
	assertInternalTaskUnchanged(t, s, "local-1")
}

func TestTaskUpdateFieldsMissingTask(t *testing.T) {
	s := newInternalTestStore(t)
	seedInternalColumns(t, s)

	err := TaskUpdateFields(s, "missing", TaskUpdateOptions{Title: strptr("After")})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %v, want not found", err)
	}
}

func TestTaskUpdateFieldsProviderRestrictionIsAtomic(t *testing.T) {
	s := newInternalTestStore(t)
	seedInternalColumns(t, s)
	provider := "jira"
	seedInternalTask(t, s, store.Task{
		ID: "jira-1", Title: "Before", Description: "Old", DescriptionMD: "Old",
		Status: "Backlog", Provider: &provider,
	})

	err := TaskUpdateFields(s, "jira-1", TaskUpdateOptions{
		Status: strptr("Doing"), Title: strptr("After"),
	})
	if err == nil || !strings.Contains(err.Error(), "cannot edit title") {
		t.Fatalf("error = %v, want provider restriction", err)
	}
	assertInternalTaskUnchanged(t, s, "jira-1")
}

func newInternalTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seedInternalColumns(t *testing.T, s *store.Store) {
	t.Helper()
	for i, name := range []string{"Backlog", "Doing", "Done"} {
		if err := s.CreateColumnMapping(context.Background(), store.ColumnMapping{
			ColumnName: name, SortOrder: i,
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func seedInternalTask(t *testing.T, s *store.Store, task store.Task) {
	t.Helper()
	task.CreatedAt = "2024-01-01T00:00:00Z"
	task.UpdatedAt = task.CreatedAt
	if err := s.CreateTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}
}

func assertInternalTaskUnchanged(t *testing.T, s *store.Store, id string) {
	t.Helper()
	task, err := s.GetTask(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if task.Title != "Before" || task.Description != "Old" ||
		task.DescriptionMD != "Old" || task.Status != "Backlog" {
		t.Errorf("task changed after rejected update: %#v", task)
	}
}
