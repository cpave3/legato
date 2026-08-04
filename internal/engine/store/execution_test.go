package store

import (
	"context"
	"testing"
	"time"
)

func executionAt(id, taskID string, started time.Time) Execution {
	return Execution{ID: id, Kind: ExecutionInteractive, Status: ExecutionRunning, TaskID: taskID, ExecutionTaskID: taskID, TaskTitle: "Task " + taskID, StartedAt: started}
}

func TestExecutionLifecycleBoundsOpenActivityAndIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	start := time.Date(2026, 8, 3, 1, 0, 0, 0, time.UTC)
	end := start.Add(10 * time.Minute)

	if err := s.CreateExecution(ctx, executionAt("exec-1", "task-1", start)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordExecutionActivityAt(ctx, "exec-1", ActivityWorking, "/repo", start); err != nil {
		t.Fatal(err)
	}
	if err := s.TerminalizeExecutionAt(ctx, "exec-1", ExecutionSucceeded, end); err != nil {
		t.Fatal(err)
	}
	if err := s.TerminalizeExecutionAt(ctx, "exec-1", ExecutionFailed, end.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetExecution(ctx, "exec-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != ExecutionSucceeded || got.EndedAt == nil || !got.EndedAt.Equal(end) {
		t.Fatalf("terminal execution = %#v", got)
	}
	if err := s.RecordExecutionActivityAt(ctx, "exec-1", ActivityWaiting, "", end.Add(time.Hour)); err != ErrExecutionTerminal {
		t.Fatalf("late activity error = %v, want ErrExecutionTerminal", err)
	}

	durations, err := s.GetExecutionDurations(ctx, "exec-1")
	if err != nil {
		t.Fatal(err)
	}
	if durations[ActivityWorking] != 10*time.Minute {
		t.Fatalf("working duration = %v, want 10m", durations[ActivityWorking])
	}
}

func TestExecutionActivityTransitionsAllowOnlyOneOpenSpan(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	start := time.Date(2026, 8, 3, 1, 0, 0, 0, time.UTC)
	if err := s.CreateExecution(ctx, executionAt("exec-1", "task-1", start)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordExecutionActivityAt(ctx, "exec-1", ActivityWorking, "/one", start); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordExecutionActivityAt(ctx, "exec-1", ActivityWorking, "/two", start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordExecutionActivityAt(ctx, "exec-1", ActivityWaiting, "/two", start.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordExecutionActivityAt(ctx, "exec-1", "", "", start.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}

	var open, total int
	if err := s.db.Get(&open, `SELECT COUNT(*) FROM execution_activity WHERE execution_id='exec-1' AND ended_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Get(&total, `SELECT COUNT(*) FROM execution_activity WHERE execution_id='exec-1'`); err != nil {
		t.Fatal(err)
	}
	if open != 0 || total != 2 {
		t.Fatalf("activity rows open=%d total=%d, want 0/2", open, total)
	}
	durations, err := s.GetExecutionDurations(ctx, "exec-1")
	if err != nil {
		t.Fatal(err)
	}
	if durations[ActivityWorking] != 2*time.Minute || durations[ActivityWaiting] != 3*time.Minute {
		t.Fatalf("durations = %#v", durations)
	}
}

func TestExecutionActivityCanEnterAndRecoverFromError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	start := time.Date(2026, 8, 3, 1, 0, 0, 0, time.UTC)
	if err := s.CreateExecution(ctx, executionAt("exec-error", "task-error", start)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordExecutionActivityAt(ctx, "exec-error", ActivityWorking, "/repo", start); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordExecutionActivityAt(ctx, "exec-error", "error", "/repo", start.Add(time.Minute)); err != nil {
		t.Fatalf("entering error activity: %v", err)
	}
	if err := s.RecordExecutionActivityAt(ctx, "exec-error", ActivityWorking, "/repo", start.Add(3*time.Minute)); err != nil {
		t.Fatalf("recovering from error activity: %v", err)
	}
	if err := s.RecordExecutionActivityAt(ctx, "exec-error", "", "/repo", start.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}

	var open int
	if err := s.db.Get(&open, `SELECT COUNT(*) FROM execution_activity WHERE execution_id='exec-error' AND ended_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	if open != 0 {
		t.Fatalf("open activity rows = %d, want 0", open)
	}
	durations, err := s.GetExecutionDurations(ctx, "exec-error")
	if err != nil {
		t.Fatal(err)
	}
	if durations["error"] != 2*time.Minute {
		t.Fatalf("error duration = %v, want 2m", durations["error"])
	}
}

func TestConcurrentExecutionsForOneTaskRemainIndependent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	start := time.Date(2026, 8, 3, 1, 0, 0, 0, time.UTC)
	for _, id := range []string{"exec-a", "exec-b"} {
		if err := s.CreateExecution(ctx, executionAt(id, "task-1", start)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordExecutionActivityAt(ctx, "exec-a", ActivityWorking, "", start); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordExecutionActivityAt(ctx, "exec-b", ActivityWaiting, "", start); err != nil {
		t.Fatal(err)
	}
	if err := s.TerminalizeExecutionAt(ctx, "exec-a", ExecutionSucceeded, start.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}

	second, err := s.GetExecution(ctx, "exec-b")
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != ExecutionRunning {
		t.Fatalf("second execution status = %s", second.Status)
	}
	var state string
	if err := s.db.Get(&state, `SELECT state FROM execution_activity WHERE execution_id='exec-b' AND ended_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	if state != ActivityWaiting {
		t.Fatalf("second execution activity = %s", state)
	}
}

func TestExecutionHistorySurvivesTaskDeletion(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task-1")
	start := time.Date(2026, 8, 3, 1, 0, 0, 0, time.UTC)
	if err := s.CreateExecution(ctx, executionAt("exec-1", "task-1", start)); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteTask(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetExecution(ctx, "exec-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.TaskID != "task-1" || got.TaskTitle == "" {
		t.Fatalf("execution attribution lost: %#v", got)
	}
}

func TestExecutionValidationRejectsInvalidLifecycleData(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	start := time.Now().UTC()
	invalid := executionAt("bad", "task", start)
	invalid.Status = ExecutionSucceeded
	if err := s.CreateExecution(ctx, invalid); err == nil {
		t.Fatal("created terminal execution without end time")
	}
	if err := s.RecordExecutionActivityAt(ctx, "missing", ActivityWorking, "", start); err != ErrNotFound {
		t.Fatalf("missing execution error = %v", err)
	}
	if err := s.CreateExecution(ctx, executionAt("exec", "task", start)); err != nil {
		t.Fatal(err)
	}
	if err := s.TerminalizeExecutionAt(ctx, "exec", ExecutionRunning, start); err == nil {
		t.Fatal("terminalized execution with running status")
	}
	if err := s.RecordExecutionActivityAt(ctx, "exec", "idle", "", start); err == nil {
		t.Fatal("recorded invalid activity state")
	}
}
