package service_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/analytics"
	"github.com/cpave3/legato/internal/service"
)

func TestExportReportMarkdown_Full(t *testing.T) {
	report := &service.Report{
		Period: analytics.TimeRange{Label: "This Week"},
		Summary: service.ReportSummary{
			TotalWorking:   6*time.Hour + 30*time.Minute,
			TotalWaiting:   2 * time.Hour,
			TasksCompleted: 4,
			TasksCreated:   7,
			AgentSessions:  12,
			PRsMerged:      2,
		},
		ByDay: []service.DayBreakdown{
			{Date: time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC), Working: 2 * time.Hour, Waiting: 30 * time.Minute},
			{Date: time.Date(2026, 3, 17, 0, 0, 0, 0, time.UTC), Working: 4*time.Hour + 30*time.Minute, Waiting: 1*time.Hour + 30*time.Minute},
		},
		ByTask: []service.TaskStats{
			{TaskID: "t1", DisplayID: "t1", Title: "Build analytics", Working: 4 * time.Hour, Waiting: 1 * time.Hour},
			{TaskID: "t2", DisplayID: "REX-123", Title: "Fix auth", Provider: "jira", Working: 2 * time.Hour, Waiting: 30 * time.Minute},
		},
		ByWorkspace: []service.WorkspaceStats{
			{WorkspaceName: "frontend", Working: 3 * time.Hour, Waiting: 1 * time.Hour, TaskCount: 2},
		},
	}

	md := service.ExportReportMarkdown(report)

	if !strings.Contains(md, "# This Week Report") {
		t.Error("missing header")
	}
	if !strings.Contains(md, "6h 30m") {
		t.Error("missing working duration")
	}
	if !strings.Contains(md, "| Tasks Created | 7 |") {
		t.Error("missing tasks created")
	}
	if !strings.Contains(md, "REX-123") {
		t.Error("missing Jira display ID")
	}
	if !strings.Contains(md, "frontend") {
		t.Error("missing workspace")
	}
	if !strings.Contains(md, "76%") {
		t.Error("missing working ratio")
	}
}

func TestExportReportMarkdown_Empty(t *testing.T) {
	report := &service.Report{
		Period: analytics.TimeRange{Label: "Today"},
	}

	md := service.ExportReportMarkdown(report)

	if !strings.Contains(md, "No activity recorded") {
		t.Error("expected 'No activity recorded' message")
	}
}

func TestExportReportMarkdown_Nil(t *testing.T) {
	md := service.ExportReportMarkdown(nil)
	if md != "" {
		t.Errorf("expected empty string for nil report, got %q", md)
	}
}
