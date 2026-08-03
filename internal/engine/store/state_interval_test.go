package store

import (
	"context"
	"testing"
	"time"
)

func seedExecutionActivity(t *testing.T, s *Store, executionID, taskID, state, dir string, start, end time.Time) {
	t.Helper()
	if err := s.CreateExecution(context.Background(), executionAt(executionID, taskID, start)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordExecutionActivityAt(context.Background(), executionID, state, dir, start); err != nil {
		t.Fatal(err)
	}
	if err := s.TerminalizeExecutionAt(context.Background(), executionID, ExecutionSucceeded, end); err != nil {
		t.Fatal(err)
	}
}

func TestGetStateDurationsBatchUsesAttributedExecutions(t *testing.T) {
	s := newTestStore(t)
	start := time.Now().UTC().Add(-10 * time.Minute)
	seedExecutionActivity(t, s, "exec-1", "task-1", ActivityWorking, "/repo/one", start, start.Add(3*time.Minute))
	seedExecutionActivity(t, s, "exec-2", "task-1", ActivityWaiting, "/repo/two", start.Add(3*time.Minute), start.Add(5*time.Minute))
	seedExecutionActivity(t, s, "exec-3", "task-2", ActivityWorking, "/repo/other", start, start.Add(time.Minute))

	got, err := s.GetStateDurationsBatch(context.Background(), []string{"task-1", "task-2", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if got["task-1"][ActivityWorking] != 3*time.Minute || got["task-1"][ActivityWaiting] != 2*time.Minute {
		t.Fatalf("task-1 durations = %#v", got["task-1"])
	}
	if got["task-2"][ActivityWorking] != time.Minute {
		t.Fatalf("task-2 durations = %#v", got["task-2"])
	}
	if got["missing"] != nil {
		t.Fatalf("missing durations = %#v", got["missing"])
	}
}

func TestGetStateDurationsBatchEmpty(t *testing.T) {
	got, err := newTestStore(t).GetStateDurationsBatch(context.Background(), nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("durations=%#v err=%v", got, err)
	}
}

func TestLatestTaskWorkingDirUsesNewestExecutionActivity(t *testing.T) {
	s := newTestStore(t)
	start := time.Now().UTC().Add(-10 * time.Minute)
	seedExecutionActivity(t, s, "exec-1", "task-1", ActivityWorking, "/repo/old", start, start.Add(time.Minute))
	seedExecutionActivity(t, s, "exec-2", "task-1", ActivityWorking, "/repo/new", start.Add(2*time.Minute), start.Add(3*time.Minute))
	got, err := s.LatestTaskWorkingDir(context.Background(), "task-1")
	if err != nil || got != "/repo/new" {
		t.Fatalf("dir=%q err=%v", got, err)
	}
}

func TestGetStateTimelineUsesExecutionBoundedActivity(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()
	seedExecutionActivity(t, s, "exec-1", "task-1", ActivityWorking, "", now.Add(-6*time.Minute), now.Add(-4*time.Minute))
	seedExecutionActivity(t, s, "exec-2", "task-1", ActivityWaiting, "", now.Add(-4*time.Minute), now.Add(-2*time.Minute))
	got, err := s.GetStateTimeline(context.Background(), "task-1", 6*time.Minute, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != ActivityWorking || got[1] != ActivityWaiting || got[2] != "" {
		t.Fatalf("timeline = %#v", got)
	}
}
