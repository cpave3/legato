package store

import (
	"context"
	"testing"
	"time"
)

func TestReconcileLostExecutionsTerminalizesOrphans(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	start := time.Now().UTC().Add(-time.Hour)
	if err := s.CreateExecution(ctx, executionAt("orphan", "task", start)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordExecutionActivityAt(ctx, "orphan", ActivityWorking, "", start); err != nil {
		t.Fatal(err)
	}
	count, err := s.ReconcileLostExecutions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("reconciled = %d, want 1", count)
	}
	got, err := s.GetExecution(ctx, "orphan")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != ExecutionLost || got.EndedAt == nil {
		t.Fatalf("execution = %#v", got)
	}
}

func TestExecutionHealthReportsRunningCountAndOldest(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	start := time.Now().UTC().Add(-time.Hour)
	if err := s.CreateExecution(ctx, executionAt("running", "task", start)); err != nil {
		t.Fatal(err)
	}
	count, oldest, err := s.ExecutionHealth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || oldest == nil || oldest.Sub(start).Abs() > time.Second {
		t.Fatalf("health count=%d oldest=%v", count, oldest)
	}
}
