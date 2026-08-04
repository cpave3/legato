package service_test

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/analytics"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func newTestStoreForReport(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

var reportExecutionID int

func seedReportExecution(t *testing.T, s *store.Store, executionTaskID, state string, start, end time.Time) {
	t.Helper()
	reportExecutionID++
	ctx := context.Background()
	kind, taskID, swarmID, subtaskID := store.ExecutionInteractive, executionTaskID, "", ""
	if subtask, err := s.GetSubtask(ctx, executionTaskID); err == nil {
		kind, taskID, swarmID, subtaskID = store.ExecutionSwarmWorker, subtask.ParentTaskID, subtask.ParentTaskID, executionTaskID
	} else {
		var subtaskCount int
		if err := s.DB().Get(&subtaskCount, `SELECT COUNT(*) FROM swarm_subtasks WHERE parent_task_id = ?`, executionTaskID); err == nil && subtaskCount > 0 {
			kind, swarmID = store.ExecutionSwarmConductor, executionTaskID
		}
	}
	workingDir := ""
	if task, err := s.GetTask(ctx, executionTaskID); err == nil && task.SwarmWorkingDir != nil {
		workingDir = *task.SwarmWorkingDir
	}
	id := fmt.Sprintf("report-exec-%d", reportExecutionID)
	if err := s.CreateExecution(ctx, store.Execution{ID: id, Kind: kind, Status: store.ExecutionRunning, TaskID: taskID, ExecutionTaskID: executionTaskID, TaskTitle: taskID, SwarmID: swarmID, SwarmSubtaskID: subtaskID, WorkingDir: workingDir, StartedAt: start}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordExecutionActivityAt(ctx, id, state, "", start); err != nil {
		t.Fatal(err)
	}
	if err := s.TerminalizeExecutionAt(ctx, id, store.ExecutionSucceeded, end); err != nil {
		t.Fatal(err)
	}
}

func TestReportService_GenerateReport(t *testing.T) {
	s := newTestStoreForReport(t)
	ctx := context.Background()

	base := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)

	// Seed columns (required for task creation)
	err := s.CreateColumnMapping(ctx, store.ColumnMapping{ColumnName: "Doing", SortOrder: 0})
	if err != nil {
		t.Fatal(err)
	}

	// Create tasks
	err = s.CreateTask(ctx, store.Task{
		ID: "t1", Title: "Build report feature", Status: "Doing",
		CreatedAt: base.Format("2006-01-02 15:04:05"),
		UpdatedAt: base.Format("2006-01-02 15:04:05"),
	})
	if err != nil {
		t.Fatal(err)
	}
	err = s.CreateTask(ctx, store.Task{
		ID: "t2", Title: "Fix auth bug", Status: "Doing",
		CreatedAt: base.Format("2006-01-02 15:04:05"),
		UpdatedAt: base.Format("2006-01-02 15:04:05"),
	})
	if err != nil {
		t.Fatal(err)
	}

	seedReportExecution(t, s, "t1", store.ActivityWorking, base, base.Add(2*time.Hour))
	seedReportExecution(t, s, "t2", store.ActivityWorking, base, base.Add(time.Hour))
	seedReportExecution(t, s, "t2", store.ActivityWaiting, base.Add(time.Hour), base.Add(2*time.Hour))
	seedReportExecution(t, s, "t2", store.ActivityError, base.Add(2*time.Hour), base.Add(3*time.Hour))

	period := analytics.TimeRange{
		Start:  time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC),
		End:    time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC),
		Period: analytics.PeriodDay,
		Label:  "Test Day",
	}

	svc := service.NewReportService(s)
	report, err := svc.GenerateReport(ctx, period)
	if err != nil {
		t.Fatal(err)
	}

	// Summary
	if report.Summary.TotalWorking != 3*time.Hour {
		t.Errorf("expected 3h working, got %v", report.Summary.TotalWorking)
	}
	if report.Summary.TotalWaiting != 1*time.Hour {
		t.Errorf("expected 1h waiting, got %v", report.Summary.TotalWaiting)
	}
	if report.Summary.TotalError != 1*time.Hour {
		t.Errorf("expected 1h error, got %v", report.Summary.TotalError)
	}
	if report.Summary.TasksCreated != 2 {
		t.Errorf("expected 2 tasks created, got %d", report.Summary.TasksCreated)
	}

	// Task stats sorted by working desc
	if len(report.ByTask) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(report.ByTask))
	}
	if report.ByTask[0].TaskID != "t1" {
		t.Errorf("expected t1 first (most working), got %s", report.ByTask[0].TaskID)
	}
	if report.ByTask[0].Title != "Build report feature" {
		t.Errorf("expected enriched title, got %q", report.ByTask[0].Title)
	}

	// Daily breakdown
	if len(report.ByDay) != 1 {
		t.Errorf("expected 1 day, got %d", len(report.ByDay))
	}
}

func TestReportService_WithWorkspace(t *testing.T) {
	s := newTestStoreForReport(t)
	ctx := context.Background()

	base := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)

	// Seed column
	err := s.CreateColumnMapping(ctx, store.ColumnMapping{ColumnName: "Doing", SortOrder: 0})
	if err != nil {
		t.Fatal(err)
	}

	// Seed workspace
	wsID, err := s.CreateWorkspace(ctx, store.Workspace{Name: "frontend", Color: strPtr("#4A9EEF"), SortOrder: 0})
	if err != nil {
		t.Fatal(err)
	}

	// Create task with workspace
	wd := "/tmp/work"
	err = s.CreateTask(ctx, store.Task{
		ID:              "t1",
		Title:           "Build UI",
		Status:          "Doing",
		SwarmWorkingDir: &wd,
		CreatedAt:       base.Format("2006-01-02 15:04:05"),
		UpdatedAt:       base.Format("2006-01-02 15:04:05"),
	})
	if err != nil {
		t.Fatal(err)
	}
	err = s.UpdateTaskWorkspace(ctx, "t1", &wsID)
	if err != nil {
		t.Fatal(err)
	}

	seedReportExecution(t, s, "t1", store.ActivityWorking, base, base.Add(2*time.Hour))

	period := analytics.TimeRange{
		Start:  time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC),
		End:    time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC),
		Period: analytics.PeriodDay,
	}

	svc := service.NewReportService(s)
	report, err := svc.GenerateReport(ctx, period)
	if err != nil {
		t.Fatal(err)
	}

	// Verify workspace enrichment on task stats
	if len(report.ByTask) != 1 {
		t.Fatalf("expected 1 task, got %d", len(report.ByTask))
	}
	if report.ByTask[0].WorkspaceName != "frontend" {
		t.Errorf("expected workspace 'frontend', got %q", report.ByTask[0].WorkspaceName)
	}

	// Verify workspace breakdown
	if len(report.ByWorkspace) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(report.ByWorkspace))
	}
	if report.ByWorkspace[0].WorkspaceColor != "#4A9EEF" {
		t.Errorf("expected color '#4A9EEF', got %q", report.ByWorkspace[0].WorkspaceColor)
	}
	if report.ByWorkspace[0].Working != 2*time.Hour {
		t.Errorf("expected 2h working, got %v", report.ByWorkspace[0].Working)
	}

	// Directory breakdown falls back to swarm_working_dir
	if len(report.ByDirectory) != 1 {
		t.Fatalf("expected 1 directory, got %d", len(report.ByDirectory))
	}

	if report.ByDirectory[0].Directory != wd {
		t.Errorf("expected directory %q, got %q", wd, report.ByDirectory[0].Directory)
	}
	if report.ByDirectory[0].Working != 2*time.Hour {
		t.Errorf("expected 2h directory working, got %v", report.ByDirectory[0].Working)
	}
}

func strPtr(s string) *string { return &s }

func TestReportService_SwarmBreakdown(t *testing.T) {
	s := newTestStoreForReport(t)
	ctx := context.Background()

	base := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)

	// Seed column (required for task creation)
	err := s.CreateColumnMapping(ctx, store.ColumnMapping{ColumnName: "Doing", SortOrder: 0})
	if err != nil {
		t.Fatal(err)
	}

	// Parent task (conductor)
	err = s.CreateTask(ctx, store.Task{
		ID: "parent-1", Title: "Swarm Alpha", Status: "Doing",
		CreatedAt: base.Format("2006-01-02 15:04:05"),
		UpdatedAt: base.Format("2006-01-02 15:04:05"),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Subtasks (not in tasks table)
	err = s.CreateSubtask(ctx, store.Subtask{
		ID:           "st-w1",
		ParentTaskID: "parent-1",
		Title:        "Worker One",
		Role:         "builder",
		Status:       "queued",
		ScopeGlobs:   "[]",
	})
	if err != nil {
		t.Fatal(err)
	}
	err = s.CreateSubtask(ctx, store.Subtask{
		ID:           "st-w2",
		ParentTaskID: "parent-1",
		Title:        "Worker Two",
		Role:         "builder",
		Status:       "queued",
		ScopeGlobs:   "[]",
	})
	if err != nil {
		t.Fatal(err)
	}

	seedReportExecution(t, s, "parent-1", store.ActivityWorking, base, base.Add(time.Hour))
	seedReportExecution(t, s, "st-w1", store.ActivityWorking, base, base.Add(2*time.Hour))
	seedReportExecution(t, s, "st-w2", store.ActivityWaiting, base, base.Add(30*time.Minute))

	period := analytics.TimeRange{
		Start:  time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC),
		End:    time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC),
		Period: analytics.PeriodDay,
	}

	svc := service.NewReportService(s)
	report, err := svc.GenerateReport(ctx, period)
	if err != nil {
		t.Fatal(err)
	}

	if len(report.BySwarm) != 1 {
		t.Fatalf("expected 1 swarm, got %d", len(report.BySwarm))
	}
	ss := report.BySwarm[0]
	if ss.ParentTaskID != "parent-1" {
		t.Errorf("expected parent-1, got %q", ss.ParentTaskID)
	}
	if ss.Title != "Swarm Alpha" {
		t.Errorf("expected title 'Swarm Alpha', got %q", ss.Title)
	}
	if ss.Working != 3*time.Hour {
		t.Errorf("expected 3h working (1h conductor + 2h worker), got %v", ss.Working)
	}
	if ss.Waiting != 30*time.Minute {
		t.Errorf("expected 30m waiting, got %v", ss.Waiting)
	}
	if ss.WallClock != 2*time.Hour {
		t.Errorf("expected 2h wall-clock (conductor + worker1), got %v", ss.WallClock)
	}
	if math.Abs(ss.ParallelRatio-3.5/2.0) > 0.001 {
		t.Errorf("expected ParallelRatio %v (3.5h / 2h), got %v", 3.5/2.0, ss.ParallelRatio)
	}
	// Both workers count because one contributes via waiting time
	if ss.WorkerCount != 2 {
		t.Errorf("expected WorkerCount 2, got %d", ss.WorkerCount)
	}
	if ss.SubtaskCount != 2 {
		t.Errorf("expected SubtaskCount 2, got %d", ss.SubtaskCount)
	}
}

func TestReportService_EmptyRange(t *testing.T) {
	s := newTestStoreForReport(t)
	ctx := context.Background()

	period := analytics.TimeRange{
		Start:  time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC),
		End:    time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC),
		Period: analytics.PeriodDay,
	}

	svc := service.NewReportService(s)
	report, err := svc.GenerateReport(ctx, period)
	if err != nil {
		t.Fatal(err)
	}

	if report.Summary.TotalWorking != 0 {
		t.Errorf("expected 0 working, got %v", report.Summary.TotalWorking)
	}
	if len(report.ByTask) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(report.ByTask))
	}
}
