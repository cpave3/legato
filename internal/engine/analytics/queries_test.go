package analytics_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/analytics"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/jmoiron/sqlx"
)

func newTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s.DB()
}

// seedTask creates a task with the given id and created_at timestamp.
func seedTask(t *testing.T, db *sqlx.DB, id string, createdAt time.Time) {
	t.Helper()
	_, err := db.Exec(
		"INSERT INTO tasks (id, title, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		id, "Task "+id, "doing", createdAt.UTC().Format("2006-01-02 15:04:05"),
		createdAt.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		t.Fatal(err)
	}
}

// seedInterval creates a state_interval with explicit timestamps.
func seedInterval(t *testing.T, db *sqlx.DB, taskID, state string, start, end time.Time) {
	t.Helper()
	_, err := db.Exec(
		"INSERT INTO state_intervals (task_id, state, started_at, ended_at) VALUES (?, ?, ?, ?)",
		taskID, state,
		start.UTC().Format("2006-01-02 15:04:05"),
		end.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		t.Fatal(err)
	}
}

// seedOpenInterval creates an open (ended_at=NULL) state_interval.
func seedOpenInterval(t *testing.T, db *sqlx.DB, taskID, state string, start time.Time) {
	t.Helper()
	_, err := db.Exec(
		"INSERT INTO state_intervals (task_id, state, started_at) VALUES (?, ?, ?)",
		taskID, state,
		start.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestQueryDurations_FullInRange(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	base := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	seedTask(t, db, "t1", base)
	seedInterval(t, db, "t1", "working", base, base.Add(2*time.Hour))
	seedInterval(t, db, "t1", "waiting", base.Add(2*time.Hour), base.Add(3*time.Hour))

	tr := analytics.TimeRange{
		Start:  time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC),
		End:    time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC),
		Period: analytics.PeriodDay,
	}
	result, err := analytics.QueryDurations(ctx, db, tr)
	if err != nil {
		t.Fatal(err)
	}
	if result.Working != 2*time.Hour {
		t.Errorf("expected 2h working, got %v", result.Working)
	}
	if result.Waiting != 1*time.Hour {
		t.Errorf("expected 1h waiting, got %v", result.Waiting)
	}
}

func TestQueryDurations_PartialOverlap(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// Interval from 22:00 on the 19th to 02:00 on the 20th (4h total)
	// Querying only the 20th should give 2h
	start := time.Date(2026, 3, 19, 22, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 20, 2, 0, 0, 0, time.UTC)
	seedTask(t, db, "t1", start)
	seedInterval(t, db, "t1", "working", start, end)

	tr := analytics.TimeRange{
		Start:  time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC),
		End:    time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC),
		Period: analytics.PeriodDay,
	}
	result, err := analytics.QueryDurations(ctx, db, tr)
	if err != nil {
		t.Fatal(err)
	}
	if result.Working != 2*time.Hour {
		t.Errorf("expected 2h working (clipped), got %v", result.Working)
	}
}

func TestQueryDurations_OpenInterval(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// Open interval started 30 minutes ago — should contribute >0 working time
	start := time.Now().UTC().Add(-30 * time.Minute)
	seedTask(t, db, "t1", start)
	seedOpenInterval(t, db, "t1", "working", start)

	tr := analytics.TimeRange{
		Start:  start.Add(-1 * time.Hour),
		End:    time.Now().UTC().Add(1 * time.Hour),
		Period: analytics.PeriodDay,
	}
	result, err := analytics.QueryDurations(ctx, db, tr)
	if err != nil {
		t.Fatal(err)
	}
	if result.Working < 25*time.Minute {
		t.Errorf("expected at least 25m working for open interval, got %v", result.Working)
	}
}

func TestQueryDurations_EmptyRange(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	tr := analytics.TimeRange{
		Start:  time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC),
		End:    time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC),
		Period: analytics.PeriodDay,
	}
	result, err := analytics.QueryDurations(ctx, db, tr)
	if err != nil {
		t.Fatal(err)
	}
	if result.Working != 0 || result.Waiting != 0 {
		t.Errorf("expected zero durations, got working=%v waiting=%v", result.Working, result.Waiting)
	}
}

func TestQueryDailyBreakdown_7Days(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	base := time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC) // Monday
	seedTask(t, db, "t1", base)
	// Only add activity on Mon and Wed
	seedInterval(t, db, "t1", "working",
		base.Add(9*time.Hour), base.Add(11*time.Hour)) // Mon 9-11
	seedInterval(t, db, "t1", "working",
		base.Add(2*24*time.Hour+9*time.Hour), base.Add(2*24*time.Hour+10*time.Hour)) // Wed 9-10

	tr := analytics.TimeRange{
		Start:  base,
		End:    base.AddDate(0, 0, 7),
		Period: analytics.PeriodWeek,
	}
	days, err := analytics.QueryDailyBreakdown(ctx, db, tr)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 7 {
		t.Fatalf("expected 7 day entries, got %d", len(days))
	}
	// Monday should have 2h
	if days[0].Working != 2*time.Hour {
		t.Errorf("Monday: expected 2h working, got %v", days[0].Working)
	}
	// Tuesday should be zero
	if days[1].Working != 0 {
		t.Errorf("Tuesday: expected 0 working, got %v", days[1].Working)
	}
	// Wednesday should have 1h
	if days[2].Working != 1*time.Hour {
		t.Errorf("Wednesday: expected 1h working, got %v", days[2].Working)
	}
}

func TestQueryTaskBreakdown(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	base := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	seedTask(t, db, "t1", base)
	seedTask(t, db, "t2", base)
	seedTask(t, db, "t3", base)
	seedInterval(t, db, "t1", "working", base, base.Add(1*time.Hour))
	seedInterval(t, db, "t2", "working", base, base.Add(2*time.Hour))
	seedInterval(t, db, "t2", "waiting", base.Add(2*time.Hour), base.Add(3*time.Hour))
	// t3 interval is outside the range
	seedInterval(t, db, "t3", "working",
		time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC))

	tr := analytics.TimeRange{
		Start:  time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC),
		End:    time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC),
		Period: analytics.PeriodDay,
	}
	tasks, err := analytics.QueryTaskBreakdown(ctx, db, tr)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	taskMap := make(map[string]analytics.TaskDuration)
	for _, td := range tasks {
		taskMap[td.TaskID] = td
	}
	if taskMap["t1"].Working != 1*time.Hour {
		t.Errorf("t1: expected 1h working, got %v", taskMap["t1"].Working)
	}
	if taskMap["t2"].Working != 2*time.Hour {
		t.Errorf("t2: expected 2h working, got %v", taskMap["t2"].Working)
	}
	if taskMap["t2"].Waiting != 1*time.Hour {
		t.Errorf("t2: expected 1h waiting, got %v", taskMap["t2"].Waiting)
	}
	if _, ok := taskMap["t3"]; ok {
		t.Error("t3 should not appear (outside range)")
	}
}

func TestQueryThroughput(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	rangeStart := time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)
	rangeEnd := time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC)

	// 2 tasks created in range
	seedTask(t, db, "t1", rangeStart.Add(1*time.Hour))
	seedTask(t, db, "t2", rangeStart.Add(2*time.Hour))
	// 1 task created outside range
	seedTask(t, db, "t3", rangeStart.AddDate(0, 0, -5))

	// 1 task archived in range
	_, err := db.Exec("UPDATE tasks SET archived_at = ? WHERE id = ?",
		rangeStart.Add(5*time.Hour).Format("2006-01-02 15:04:05"), "t1")
	if err != nil {
		t.Fatal(err)
	}

	// 2 agent sessions in range
	_, err = db.Exec("INSERT INTO agent_sessions (task_id, tmux_session, started_at) VALUES (?, ?, ?)",
		"t1", "legato-t1", rangeStart.Add(1*time.Hour).Format("2006-01-02 15:04:05"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("INSERT INTO agent_sessions (task_id, tmux_session, started_at) VALUES (?, ?, ?)",
		"t2", "legato-t2", rangeStart.Add(2*time.Hour).Format("2006-01-02 15:04:05"))
	if err != nil {
		t.Fatal(err)
	}

	// 1 merged PR in range
	prMeta, _ := json.Marshal(map[string]interface{}{
		"state":      "MERGED",
		"updated_at": rangeStart.Add(3 * time.Hour).Format(time.RFC3339),
	})
	_, err = db.Exec("UPDATE tasks SET pr_meta = ? WHERE id = ?", string(prMeta), "t2")
	if err != nil {
		t.Fatal(err)
	}

	tr := analytics.TimeRange{Start: rangeStart, End: rangeEnd, Period: analytics.PeriodDay}
	result, err := analytics.QueryThroughput(ctx, db, tr)
	if err != nil {
		t.Fatal(err)
	}
	if result.TasksCreated != 2 {
		t.Errorf("expected 2 created, got %d", result.TasksCreated)
	}
	if result.TasksCompleted != 1 {
		t.Errorf("expected 1 completed, got %d", result.TasksCompleted)
	}
	if result.AgentSessions != 2 {
		t.Errorf("expected 2 sessions, got %d", result.AgentSessions)
	}
	if result.PRsMerged != 1 {
		t.Errorf("expected 1 merged PR, got %d", result.PRsMerged)
	}
}

func TestQueryWorkspaceBreakdown(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	base := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)

	// Create workspaces
	_, err := db.Exec("INSERT INTO workspaces (id, name, color, sort_order) VALUES (1, 'frontend', '#4A9EEF', 0)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("INSERT INTO workspaces (id, name, color, sort_order) VALUES (2, 'backend', '#2ECC71', 1)")
	if err != nil {
		t.Fatal(err)
	}

	// Tasks with workspaces
	seedTask(t, db, "t1", base)
	_, err = db.Exec("UPDATE tasks SET workspace_id = 1 WHERE id = 't1'")
	if err != nil {
		t.Fatal(err)
	}
	seedTask(t, db, "t2", base)
	_, err = db.Exec("UPDATE tasks SET workspace_id = 2 WHERE id = 't2'")
	if err != nil {
		t.Fatal(err)
	}
	seedTask(t, db, "t3", base) // no workspace

	seedInterval(t, db, "t1", "working", base, base.Add(2*time.Hour))
	seedInterval(t, db, "t2", "working", base, base.Add(1*time.Hour))
	seedInterval(t, db, "t3", "working", base, base.Add(30*time.Minute))

	tr := analytics.TimeRange{
		Start:  time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC),
		End:    time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC),
		Period: analytics.PeriodDay,
	}
	results, err := analytics.QueryWorkspaceBreakdown(ctx, db, tr)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 workspace groups, got %d", len(results))
	}

	wsMap := make(map[string]analytics.WorkspaceBreakdown)
	for _, ws := range results {
		wsMap[ws.WorkspaceName] = ws
	}
	if wsMap["frontend"].Working != 2*time.Hour {
		t.Errorf("frontend: expected 2h, got %v", wsMap["frontend"].Working)
	}
	if wsMap["backend"].Working != 1*time.Hour {
		t.Errorf("backend: expected 1h, got %v", wsMap["backend"].Working)
	}
	if wsMap["Unassigned"].Working != 30*time.Minute {
		t.Errorf("unassigned: expected 30m, got %v", wsMap["Unassigned"].Working)
	}
	if wsMap["frontend"].TaskCount != 1 {
		t.Errorf("frontend: expected 1 task, got %d", wsMap["frontend"].TaskCount)
	}
}
