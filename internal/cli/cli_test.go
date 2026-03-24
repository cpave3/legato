package cli_test

import (
	"context"
	"path/filepath"
	"strings"
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

func TestAgentState_RecordsStateInterval(t *testing.T) {
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

	durations, err := s.GetStateDurations(ctx, "abc123")
	if err != nil {
		t.Fatalf("GetStateDurations: %v", err)
	}
	if _, ok := durations["working"]; !ok {
		t.Error("expected working duration to be recorded")
	}
}

func TestAgentState_TransitionClosesAndOpensInterval(t *testing.T) {
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
	cli.AgentState(s, "abc123", "waiting")

	durations, err := s.GetStateDurations(ctx, "abc123")
	if err != nil {
		t.Fatalf("GetStateDurations: %v", err)
	}
	if _, ok := durations["working"]; !ok {
		t.Error("expected working duration after transition")
	}
	if _, ok := durations["waiting"]; !ok {
		t.Error("expected waiting duration after transition")
	}
}

func TestAgentSummary_MixedStates(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "task1", "Doing")
	seedTask(t, s, "task2", "Doing")
	seedTask(t, s, "task3", "Doing")

	ctx := context.Background()
	s.InsertAgentSession(ctx, store.AgentSession{TaskID: "task1", TmuxSession: "legato-task1", Command: "shell", Status: "running"})
	s.InsertAgentSession(ctx, store.AgentSession{TaskID: "task2", TmuxSession: "legato-task2", Command: "shell", Status: "running"})
	s.InsertAgentSession(ctx, store.AgentSession{TaskID: "task3", TmuxSession: "legato-task3", Command: "shell", Status: "running"})
	s.UpdateAgentActivity(ctx, "task1", "working")
	s.UpdateAgentActivity(ctx, "task2", "waiting")
	// task3 idle

	out, err := cli.AgentSummary(s, "")
	if err != nil {
		t.Fatalf("AgentSummary: %v", err)
	}
	// Should contain tmux style markup and counts
	if !strings.Contains(out, "1 working") {
		t.Errorf("output missing working count: %q", out)
	}
	if !strings.Contains(out, "1 waiting") {
		t.Errorf("output missing waiting count: %q", out)
	}
	if !strings.Contains(out, "1 idle") {
		t.Errorf("output missing idle count: %q", out)
	}
	if !strings.Contains(out, "#[fg=") {
		t.Errorf("output missing tmux style markup: %q", out)
	}
}

func TestAgentSummary_ZeroCountsOmitted(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "task1", "Doing")

	ctx := context.Background()
	s.InsertAgentSession(ctx, store.AgentSession{TaskID: "task1", TmuxSession: "legato-task1", Command: "shell", Status: "running"})
	s.UpdateAgentActivity(ctx, "task1", "working")

	out, err := cli.AgentSummary(s, "")
	if err != nil {
		t.Fatalf("AgentSummary: %v", err)
	}
	if !strings.Contains(out, "1 working") {
		t.Errorf("output missing working count: %q", out)
	}
	// Zero-count waiting should be omitted
	if strings.Contains(out, "waiting") {
		t.Errorf("output should omit zero waiting: %q", out)
	}
	// Idle always shown
	if !strings.Contains(out, "idle") {
		t.Errorf("output should always show idle: %q", out)
	}
}

func TestAgentSummary_NoSessions(t *testing.T) {
	s := newTestStore(t)

	out, err := cli.AgentSummary(s, "")
	if err != nil {
		t.Fatalf("AgentSummary: %v", err)
	}
	if out == "" {
		t.Error("expected non-empty output even with no sessions")
	}
}

func TestAgentSummary_ExcludeTask(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "task1", "Doing")
	seedTask(t, s, "task2", "Doing")

	ctx := context.Background()
	s.InsertAgentSession(ctx, store.AgentSession{TaskID: "task1", TmuxSession: "legato-task1", Command: "shell", Status: "running"})
	s.InsertAgentSession(ctx, store.AgentSession{TaskID: "task2", TmuxSession: "legato-task2", Command: "shell", Status: "running"})
	s.UpdateAgentActivity(ctx, "task1", "working")
	s.UpdateAgentActivity(ctx, "task2", "working")

	out, err := cli.AgentSummary(s, "task1")
	if err != nil {
		t.Fatalf("AgentSummary: %v", err)
	}
	if !strings.Contains(out, "1 working") {
		t.Errorf("output should show 1 working (task1 excluded): %q", out)
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
