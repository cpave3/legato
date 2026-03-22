package service_test

import (
	"context"
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

func TestReportService_GenerateReport(t *testing.T) {
	s := newTestStoreForReport(t)
	ctx := context.Background()
	db := s.DB()

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

	// Seed intervals
	_, err = db.Exec(
		"INSERT INTO state_intervals (task_id, state, started_at, ended_at) VALUES (?, ?, ?, ?)",
		"t1", "working", base.Format("2006-01-02 15:04:05"),
		base.Add(2*time.Hour).Format("2006-01-02 15:04:05"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(
		"INSERT INTO state_intervals (task_id, state, started_at, ended_at) VALUES (?, ?, ?, ?)",
		"t2", "working", base.Format("2006-01-02 15:04:05"),
		base.Add(1*time.Hour).Format("2006-01-02 15:04:05"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(
		"INSERT INTO state_intervals (task_id, state, started_at, ended_at) VALUES (?, ?, ?, ?)",
		"t2", "waiting", base.Add(1*time.Hour).Format("2006-01-02 15:04:05"),
		base.Add(2*time.Hour).Format("2006-01-02 15:04:05"))
	if err != nil {
		t.Fatal(err)
	}

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
	db := s.DB()

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
	err = s.CreateTask(ctx, store.Task{
		ID: "t1", Title: "Build UI", Status: "Doing",
		CreatedAt: base.Format("2006-01-02 15:04:05"),
		UpdatedAt: base.Format("2006-01-02 15:04:05"),
	})
	if err != nil {
		t.Fatal(err)
	}
	err = s.UpdateTaskWorkspace(ctx, "t1", &wsID)
	if err != nil {
		t.Fatal(err)
	}

	// Seed interval
	_, err = db.Exec(
		"INSERT INTO state_intervals (task_id, state, started_at, ended_at) VALUES (?, ?, ?, ?)",
		"t1", "working", base.Format("2006-01-02 15:04:05"),
		base.Add(2*time.Hour).Format("2006-01-02 15:04:05"))
	if err != nil {
		t.Fatal(err)
	}

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
}

func strPtr(s string) *string { return &s }

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
