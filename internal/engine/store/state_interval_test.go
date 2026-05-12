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

	if err := s.RecordStateTransition(ctx, "task1", "working", ""); err != nil {
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

	if err := s.RecordStateTransition(ctx, "task1", "working", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordStateTransition(ctx, "task1", "waiting", ""); err != nil {
		t.Fatal(err)
	}

	var intervals []StateInterval
	if err := s.db.SelectContext(ctx, &intervals, "SELECT * FROM state_intervals WHERE task_id = ? ORDER BY id", "task1"); err != nil {
		t.Fatal(err)
	}
	if len(intervals) != 2 {
		t.Fatalf("got %d intervals, want 2", len(intervals))
	}
	if intervals[0].EndedAt == nil {
		t.Error("first interval should be closed")
	}
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

	if err := s.RecordStateTransition(ctx, "task1", "working", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordStateTransition(ctx, "task1", "working", ""); err != nil {
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

	if err := s.RecordStateTransition(ctx, "task1", "working", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordStateTransition(ctx, "task1", "", ""); err != nil {
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

	if err := s.RecordStateTransition(ctx, "task1", "", ""); err != nil {
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
	if d := durations["working"]; d < 85*time.Second || d > 95*time.Second {
		t.Errorf("working duration = %v, want ~90s", d)
	}
	if d := durations["waiting"]; d < 12*time.Second || d > 18*time.Second {
		t.Errorf("waiting duration = %v, want ~15s", d)
	}
}

func TestGetStateDurationsOpenInterval(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task1")

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
	if d := durations["working"]; d < 25*time.Second || d > 38*time.Second {
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

	_, err := s.db.ExecContext(ctx,
		"INSERT INTO state_intervals (task_id, state, started_at, ended_at) VALUES (?, 'working', datetime('now', '-120 seconds'), datetime('now', '-60 seconds'))",
		"task1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO state_intervals (task_id, state, started_at, ended_at) VALUES (?, 'waiting', datetime('now', '-60 seconds'), datetime('now', '-30 seconds'))",
		"task2")
	if err != nil {
		t.Fatal(err)
	}

	result, err := s.GetStateDurationsBatch(ctx, []string{"task1", "task2", "task3"})
	if err != nil {
		t.Fatal(err)
	}
	if d := result["task1"]["working"]; d < 55*time.Second || d > 65*time.Second {
		t.Errorf("task1 working = %v, want ~60s", d)
	}
	if d := result["task2"]["waiting"]; d < 25*time.Second || d > 35*time.Second {
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

func TestRecordStateTransitionStoresWorkingDir(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task1")

	wd := "/projects/frontend"
	if err := s.RecordStateTransition(ctx, "task1", "working", wd); err != nil {
		t.Fatal(err)
	}

	var intervals []StateInterval
	if err := s.db.SelectContext(ctx, &intervals, "SELECT * FROM state_intervals WHERE task_id = ?", "task1"); err != nil {
		t.Fatal(err)
	}
	if len(intervals) != 1 {
		t.Fatalf("got %d intervals, want 1", len(intervals))
	}
	if intervals[0].WorkingDir == nil || *intervals[0].WorkingDir != wd {
		t.Errorf("working_dir = %v, want %q", intervals[0].WorkingDir, wd)
	}
}

func TestRecordStateTransitionNullWorkingDir(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task1")

	if err := s.RecordStateTransition(ctx, "task1", "working", ""); err != nil {
		t.Fatal(err)
	}

	var intervals []StateInterval
	if err := s.db.SelectContext(ctx, &intervals, "SELECT * FROM state_intervals WHERE task_id = ?", "task1"); err != nil {
		t.Fatal(err)
	}
	if len(intervals) != 1 {
		t.Fatalf("got %d intervals, want 1", len(intervals))
	}
	if intervals[0].WorkingDir != nil {
		t.Errorf("working_dir should be nil, got %v", intervals[0].WorkingDir)
	}
}

func TestGetStateTimelineBucketAssignment(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task1")

	// Insert via SQLite datetime('now', '...') so UTC semantics match Go time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO state_intervals (task_id, state, started_at, ended_at) VALUES
		(?, 'working', datetime('now', '-6 minutes'), datetime('now', '-4 minutes')),
		(?, 'waiting', datetime('now', '-4 minutes'), datetime('now', '-2 minutes'))`,
		"task1", "task1")
	if err != nil {
		t.Fatal(err)
	}

	timeline, err := s.GetStateTimeline(ctx, "task1", 6*time.Minute, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(timeline) != 3 {
		t.Fatalf("got %d buckets, want 3", len(timeline))
	}
	// The timestamps may jitter by a few seconds; use >=1s intervals.
	if timeline[0] != "working" {
		t.Errorf("bucket[0] = %q, want working", timeline[0])
	}
	if timeline[1] != "waiting" {
		t.Errorf("bucket[1] = %q, want waiting", timeline[1])
	}
	if timeline[2] != "" {
		t.Errorf("bucket[2] = %q, want idle (empty string)", timeline[2])
	}
}

func TestGetStateTimelineMajority(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task1")

	// Window = 6 minutes, buckets = 3, each bucket = 2 minutes.
	// Use explicit second-level offsets to fill each bucket predictably.
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO state_intervals (task_id, state, started_at, ended_at) VALUES
		(?, 'working', datetime('now', '-5 minutes', '-50 seconds'), datetime('now', '-4 minutes', '-10 seconds')),
		(?, 'waiting', datetime('now', '-4 minutes', '-10 seconds'), datetime('now', '-3 minutes', '-40 seconds')),
		(?, 'working', datetime('now', '-90 seconds'), NULL)`,
		"task1", "task1", "task1")
	if err != nil {
		t.Fatal(err)
	}

	timeline, err := s.GetStateTimeline(ctx, "task1", 6*time.Minute, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(timeline) != 3 {
		t.Fatalf("got %d buckets, want 3", len(timeline))
	}
	if timeline[0] != "working" {
		t.Errorf("bucket[0] = %q, want working (majority)", timeline[0])
	}
	if timeline[1] != "waiting" {
		t.Errorf("bucket[1] = %q, want waiting (within -4m to -2m bucket)", timeline[1])
	}
	if timeline[2] != "working" {
		t.Errorf("bucket[2] = %q, want working (open interval)", timeline[2])
	}
}

func TestGetStateTimelineSubtaskID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	createTestTask(t, s, "parent-1")
	st := Subtask{
		ID:           "st-abcd1234",
		ParentTaskID: "parent-1",
		Title:        "Worker task",
		Role:         "builder",
		Status:       "queued",
		ScopeGlobs:   "[]",
	}
	if err := s.CreateSubtask(ctx, st); err != nil {
		t.Fatal(err)
	}

	_, err := s.db.ExecContext(ctx,
		"INSERT INTO state_intervals (task_id, state, started_at, ended_at) VALUES (?, 'working', datetime('now', '-2 minutes'), datetime('now', '-1 minutes'))",
		"st-abcd1234")
	if err != nil {
		t.Fatal(err)
	}

	timeline, err := s.GetStateTimeline(ctx, "st-abcd1234", 3*time.Minute, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(timeline) != 3 {
		t.Fatalf("got %d buckets, want 3", len(timeline))
	}
	if timeline[1] != "working" {
		t.Errorf("bucket[1] = %q, want working", timeline[1])
	}
	if timeline[0] != "" {
		t.Errorf("bucket[0] = %q, want idle", timeline[0])
	}
	if timeline[2] != "" {
		t.Errorf("bucket[2] = %q, want idle", timeline[2])
	}
}

func TestRecordStateTransitionSubtaskIDNoFKRegression(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	createTestTask(t, s, "parent-1")
	st := Subtask{
		ID:           "st-abcd1234",
		ParentTaskID: "parent-1",
		Title:        "Worker task",
		Role:         "builder",
		Status:       "queued",
		ScopeGlobs:   "[]",
	}
	if err := s.CreateSubtask(ctx, st); err != nil {
		t.Fatal(err)
	}

	if err := s.RecordStateTransition(ctx, "st-abcd1234", "working", ""); err != nil {
		t.Fatalf("recording state for sub-task ID should succeed after FK drop: %v", err)
	}

	var count int
	if err := s.db.GetContext(ctx, &count,
		"SELECT COUNT(*) FROM state_intervals WHERE task_id = ?", "st-abcd1234"); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 interval for sub-task, got %d", count)
	}
}
