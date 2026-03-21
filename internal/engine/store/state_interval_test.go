package store

import (
	"context"
	"testing"
	"time"
)

func TestStateIntervalsTableExists(t *testing.T) {
	s := newTestStore(t)

	var tables []string
	err := s.db.Select(&tables, "SELECT name FROM sqlite_master WHERE type='table' AND name='state_intervals'")
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 {
		t.Fatal("state_intervals table does not exist after migration")
	}
}

func TestRecordStateTransitionOpensInterval(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task1")

	if err := s.RecordStateTransition(ctx, "task1", "working"); err != nil {
		t.Fatal(err)
	}

	var intervals []StateInterval
	if err := s.db.SelectContext(ctx, &intervals, "SELECT * FROM state_intervals WHERE task_id = ?", "task1"); err != nil {
		t.Fatal(err)
	}
	if len(intervals) != 1 {
		t.Fatalf("got %d intervals, want 1", len(intervals))
	}
	if intervals[0].State != "working" {
		t.Errorf("state = %q, want %q", intervals[0].State, "working")
	}
	if intervals[0].EndedAt != nil {
		t.Error("ended_at should be nil for open interval")
	}
}

func TestRecordStateTransitionClosesAndOpens(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task1")

	if err := s.RecordStateTransition(ctx, "task1", "working"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordStateTransition(ctx, "task1", "waiting"); err != nil {
		t.Fatal(err)
	}

	var intervals []StateInterval
	if err := s.db.SelectContext(ctx, &intervals, "SELECT * FROM state_intervals WHERE task_id = ? ORDER BY id", "task1"); err != nil {
		t.Fatal(err)
	}
	if len(intervals) != 2 {
		t.Fatalf("got %d intervals, want 2", len(intervals))
	}
	// First interval should be closed
	if intervals[0].EndedAt == nil {
		t.Error("first interval should be closed")
	}
	// Second interval should be open
	if intervals[1].State != "waiting" {
		t.Errorf("second interval state = %q, want %q", intervals[1].State, "waiting")
	}
	if intervals[1].EndedAt != nil {
		t.Error("second interval should be open")
	}
}

func TestRecordStateTransitionIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task1")

	if err := s.RecordStateTransition(ctx, "task1", "working"); err != nil {
		t.Fatal(err)
	}
	// Same state again — should be a no-op
	if err := s.RecordStateTransition(ctx, "task1", "working"); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := s.db.GetContext(ctx, &count, "SELECT COUNT(*) FROM state_intervals WHERE task_id = ?", "task1"); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("got %d intervals, want 1 (idempotent)", count)
	}
}

func TestRecordStateTransitionClearClosesOnly(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task1")

	if err := s.RecordStateTransition(ctx, "task1", "working"); err != nil {
		t.Fatal(err)
	}
	// Clear state — should close but not open new
	if err := s.RecordStateTransition(ctx, "task1", ""); err != nil {
		t.Fatal(err)
	}

	var intervals []StateInterval
	if err := s.db.SelectContext(ctx, &intervals, "SELECT * FROM state_intervals WHERE task_id = ?", "task1"); err != nil {
		t.Fatal(err)
	}
	if len(intervals) != 1 {
		t.Fatalf("got %d intervals, want 1", len(intervals))
	}
	if intervals[0].EndedAt == nil {
		t.Error("interval should be closed after clear")
	}
}

func TestRecordStateTransitionNoOpenInterval(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task1")

	// No open interval — clear should be a no-op
	if err := s.RecordStateTransition(ctx, "task1", ""); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := s.db.GetContext(ctx, &count, "SELECT COUNT(*) FROM state_intervals WHERE task_id = ?", "task1"); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("got %d intervals, want 0", count)
	}
}

func TestGetStateDurationsCompletedIntervals(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task1")

	// Insert completed intervals manually with known durations
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO state_intervals (task_id, state, started_at, ended_at) VALUES
		(?, 'working', datetime('now', '-120 seconds'), datetime('now', '-60 seconds')),
		(?, 'working', datetime('now', '-50 seconds'), datetime('now', '-20 seconds')),
		(?, 'waiting', datetime('now', '-20 seconds'), datetime('now', '-5 seconds'))`,
		"task1", "task1", "task1")
	if err != nil {
		t.Fatal(err)
	}

	durations, err := s.GetStateDurations(ctx, "task1")
	if err != nil {
		t.Fatal(err)
	}

	// working: 60s + 30s = 90s (allow ±5s for SQLite rounding)
	if d := durations["working"]; d < 85*time.Second || d > 95*time.Second {
		t.Errorf("working duration = %v, want ~90s", d)
	}
	// waiting: 15s
	if d := durations["waiting"]; d < 12*time.Second || d > 18*time.Second {
		t.Errorf("waiting duration = %v, want ~15s", d)
	}
}

func TestGetStateDurationsOpenInterval(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task1")

	// Insert an open interval started 30 seconds ago
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO state_intervals (task_id, state, started_at) VALUES (?, 'working', datetime('now', '-30 seconds'))",
		"task1")
	if err != nil {
		t.Fatal(err)
	}

	durations, err := s.GetStateDurations(ctx, "task1")
	if err != nil {
		t.Fatal(err)
	}

	// Should be approximately 30 seconds
	if d := durations["working"]; d < 28*time.Second || d > 35*time.Second {
		t.Errorf("working duration = %v, want ~30s", d)
	}
}

func TestGetStateDurationsNoIntervals(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task1")

	durations, err := s.GetStateDurations(ctx, "task1")
	if err != nil {
		t.Fatal(err)
	}
	if len(durations) != 0 {
		t.Errorf("got %d entries, want 0", len(durations))
	}
}

func TestGetStateDurationsBatch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task1")
	createTestTask(t, s, "task2")
	createTestTask(t, s, "task3")

	// task1: 60s working
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO state_intervals (task_id, state, started_at, ended_at) VALUES (?, 'working', datetime('now', '-120 seconds'), datetime('now', '-60 seconds'))",
		"task1")
	if err != nil {
		t.Fatal(err)
	}

	// task2: 30s waiting
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO state_intervals (task_id, state, started_at, ended_at) VALUES (?, 'waiting', datetime('now', '-60 seconds'), datetime('now', '-30 seconds'))",
		"task2")
	if err != nil {
		t.Fatal(err)
	}

	// task3: no intervals

	result, err := s.GetStateDurationsBatch(ctx, []string{"task1", "task2", "task3"})
	if err != nil {
		t.Fatal(err)
	}

	if d := result["task1"]["working"]; d < 59*time.Second || d > 61*time.Second {
		t.Errorf("task1 working = %v, want ~60s", d)
	}
	if d := result["task2"]["waiting"]; d < 29*time.Second || d > 31*time.Second {
		t.Errorf("task2 waiting = %v, want ~30s", d)
	}
	if _, ok := result["task3"]; ok {
		t.Error("task3 should not be in results (no intervals)")
	}
}

func TestGetStateDurationsBatchEmpty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	result, err := s.GetStateDurationsBatch(ctx, []string{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Errorf("got %d entries, want 0", len(result))
	}
}
