package cli_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seedColumns(t *testing.T, s *store.Store) {
	t.Helper()
	ctx := context.Background()
	for i, col := range []string{"Backlog", "Doing", "Review", "Done"} {
		if err := s.CreateColumnMapping(ctx, store.ColumnMapping{
			ColumnName: col,
			SortOrder:  i,
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func seedTask(t *testing.T, s *store.Store, id, status string) {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{
		ID:        id,
		Title:     "Test " + id,
		Status:    status,
		CreatedAt: "2024-01-01T00:00:00Z",
		UpdatedAt: "2024-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTaskUpdate_MovesTaskToColumn(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Backlog")

	err := cli.TaskUpdate(s, "abc123", "Done")
	if err != nil {
		t.Fatalf("TaskUpdate: %v", err)
	}

	task, err := s.GetTask(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != "Done" {
		t.Errorf("task status = %q, want %q", task.Status, "Done")
	}
}

func TestTaskUpdate_CaseInsensitiveStatus(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Backlog")

	err := cli.TaskUpdate(s, "abc123", "done")
	if err != nil {
		t.Fatalf("TaskUpdate: %v", err)
	}

	task, _ := s.GetTask(context.Background(), "abc123")
	if task.Status != "Done" {
		t.Errorf("task status = %q, want %q", task.Status, "Done")
	}
}

func TestTaskUpdate_InvalidStatus(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Backlog")

	err := cli.TaskUpdate(s, "abc123", "invalid")
	if err == nil {
		t.Fatal("expected error for invalid status")
	}
}

func TestTaskUpdate_NonexistentTask(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)

	err := cli.TaskUpdate(s, "nonexistent", "Done")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
}

func TestAgentState_UpdatesActivity(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Doing")

	ctx := context.Background()
	s.InsertAgentSession(ctx, store.AgentSession{
		TaskID:      "abc123",
		TmuxSession: "legato-abc123",
		Command:     "shell",
		Status:      "running",
	})

	err := cli.AgentState(s, "abc123", "working")
	if err != nil {
		t.Fatalf("AgentState: %v", err)
	}

	session, err := s.GetAgentSessionByTaskID(ctx, "abc123")
	if err != nil {
		t.Fatalf("GetAgentSessionByTaskID: %v", err)
	}
	if session.Activity != "working" {
		t.Errorf("activity = %q, want %q", session.Activity, "working")
	}
}

func TestAgentState_ClearsActivity(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Doing")

	ctx := context.Background()
	s.InsertAgentSession(ctx, store.AgentSession{
		TaskID:      "abc123",
		TmuxSession: "legato-abc123",
		Command:     "shell",
		Status:      "running",
	})

	cli.AgentState(s, "abc123", "working")
	cli.AgentState(s, "abc123", "")

	session, _ := s.GetAgentSessionByTaskID(ctx, "abc123")
	if session.Activity != "" {
		t.Errorf("activity = %q, want empty", session.Activity)
	}
}

func TestTaskNote_AppendsNote(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Backlog")

	err := cli.TaskNote(s, "abc123", "Fixed the auth bug")
	if err != nil {
		t.Fatalf("TaskNote: %v", err)
	}

	task, _ := s.GetTask(context.Background(), "abc123")
	if task.Description == "" {
		t.Error("expected task description to be updated with note")
	}
}
