package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestABrokerTokenHashResolvesToItsRunningExecution(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	start := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	if err := s.CreateExecution(ctx, executionAt("ex-a", "task-a", start)); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateExecution(ctx, executionAt("ex-b", "task-b", start)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetExecutionBrokerToken(ctx, "ex-a", "hash-a"); err != nil {
		t.Fatal(err)
	}
	got, err := s.ExecutionByBrokerToken(ctx, "hash-a")
	if err != nil || got.ID != "ex-a" || got.TaskID != "task-a" {
		t.Fatalf("resolved = %+v, %v", got, err)
	}
	if _, err := s.ExecutionByBrokerToken(ctx, "hash-b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown hash: %v", err)
	}
	if _, err := s.ExecutionByBrokerToken(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty hash: %v", err)
	}
	if err := s.TerminalizeExecutionAt(ctx, "ex-a", ExecutionCancelled, start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExecutionByBrokerToken(ctx, "hash-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a terminal execution still resolves: %v", err)
	}
}
