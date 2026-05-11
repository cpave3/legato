package report

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/engine/analytics"
	"github.com/cpave3/legato/internal/service"
)

// mockReportService implements service.ReportService for testing.
type mockReportService struct {
	report *service.Report
}

func (m *mockReportService) GenerateReport(_ context.Context, period analytics.TimeRange) (*service.Report, error) {
	if m.report != nil {
		r := *m.report
		r.Period = period
		return &r, nil
	}
	return &service.Report{Period: period}, nil
}

func sampleReport() *service.Report {
	return &service.Report{
		Period: analytics.TimeRange{Label: "Today", Period: analytics.PeriodDay},
		Summary: service.ReportSummary{
			TotalWorking: 3 * time.Hour,
			TotalWaiting: 1 * time.Hour,
			TasksCreated: 2,
		},
		ByTask: []service.TaskStats{
			{TaskID: "t1", Title: "Task 1", Working: 2 * time.Hour},
			{TaskID: "t2", Title: "Task 2", Working: 1 * time.Hour},
		},
	}
}

func TestModel_PeriodCycling(t *testing.T) {
	svc := &mockReportService{report: sampleReport()}
	m := New(svc)
	m.width = 120
	m.height = 40

	if m.periodType != analytics.PeriodDay {
		t.Errorf("expected PeriodDay, got %v", m.periodType)
	}

	tabMsg := tea.KeyMsg{Type: tea.KeyTab}

	// Tab cycles to Week
	m, _ = m.Update(tabMsg)
	if m.periodType != analytics.PeriodWeek {
		t.Errorf("expected PeriodWeek after tab, got %v", m.periodType)
	}

	// Tab again cycles to Month
	m, _ = m.Update(tabMsg)
	if m.periodType != analytics.PeriodMonth {
		t.Errorf("expected PeriodMonth, got %v", m.periodType)
	}

	// Tab wraps to Day
	m, _ = m.Update(tabMsg)
	if m.periodType != analytics.PeriodDay {
		t.Errorf("expected PeriodDay after wrap, got %v", m.periodType)
	}
}

func TestModel_TaskScrolling(t *testing.T) {
	svc := &mockReportService{report: sampleReport()}
	m := New(svc)
	m.width = 120
	m.height = 40

	// Load report
	m, _ = m.Update(ReportLoadedMsg{Report: sampleReport()})

	if m.taskScroll != 0 {
		t.Errorf("expected scroll 0, got %d", m.taskScroll)
	}

	// j scrolls down
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.taskScroll != 1 {
		t.Errorf("expected scroll 1, got %d", m.taskScroll)
	}

	// Can't scroll past end
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.taskScroll != 1 {
		t.Errorf("expected scroll 1 (clamped), got %d", m.taskScroll)
	}

	// k scrolls up
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m.taskScroll != 0 {
		t.Errorf("expected scroll 0, got %d", m.taskScroll)
	}
}

func TestModel_EscReturnToBoard(t *testing.T) {
	svc := &mockReportService{}
	m := New(svc)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected ReturnToBoardMsg command")
	}

	msg := cmd()
	if _, ok := msg.(ReturnToBoardMsg); !ok {
		t.Errorf("expected ReturnToBoardMsg, got %T", msg)
	}
}

func TestModel_CopyReport(t *testing.T) {
	svc := &mockReportService{report: sampleReport()}
	m := New(svc)
	m.width = 120
	m.height = 40

	// Load report first
	m, _ = m.Update(ReportLoadedMsg{Report: sampleReport()})

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}})
	if cmd == nil {
		t.Fatal("expected CopyReportMsg command")
	}

	msg := cmd()
	copyMsg, ok := msg.(CopyReportMsg)
	if !ok {
		t.Fatalf("expected CopyReportMsg, got %T", msg)
	}
	if copyMsg.Markdown == "" {
		t.Error("expected non-empty markdown")
	}
}

func TestModel_ReportLoaded(t *testing.T) {
	svc := &mockReportService{}
	m := New(svc)
	m.loading = true

	r := sampleReport()
	m, _ = m.Update(ReportLoadedMsg{Report: r})

	if m.loading {
		t.Error("expected loading=false after ReportLoadedMsg")
	}
	if m.report == nil {
		t.Fatal("expected report to be set")
	}
	if m.report.Summary.TotalWorking != 3*time.Hour {
		t.Errorf("expected 3h working, got %v", m.report.Summary.TotalWorking)
	}
}

func TestModel_ViewNotPanicking(t *testing.T) {
	svc := &mockReportService{report: sampleReport()}
	m := New(svc)
	m.width = 120
	m.height = 40

	// View with no report loaded
	_ = m.View()

	// View with report loaded
	m, _ = m.Update(ReportLoadedMsg{Report: sampleReport()})
	out := m.View()
	if out == "" {
		t.Error("expected non-empty view")
	}
}

func TestModel_WindowResize(t *testing.T) {
	svc := &mockReportService{}
	m := New(svc)

	m, _ = m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	if m.width != 200 || m.height != 50 {
		t.Errorf("expected 200x50, got %dx%d", m.width, m.height)
	}
}

func TestModel_SwarmSectionRendered(t *testing.T) {
	report := sampleReport()
	report.BySwarm = []service.SwarmStats{
		{ParentTaskID: "swarm-1", Title: "Fix auth flow", Working: 2 * time.Hour, Waiting: 30 * time.Minute, WorkerCount: 3, SubtaskCount: 5},
		{ParentTaskID: "swarm-2", Title: "Refactor API", Working: 1 * time.Hour, Waiting: 0, WorkerCount: 1, SubtaskCount: 2},
	}
	svc := &mockReportService{report: report}
	m := New(svc)
	m.width = 120
	m.height = 40
	m, _ = m.Update(ReportLoadedMsg{Report: report})

	out := m.View()
	if !strings.Contains(out, "SWARMS") {
		t.Error("expected SWARMS section in view")
	}
	if !strings.Contains(out, "Fix auth flow") {
		t.Error("expected Fix auth flow in view")
	}
	if !strings.Contains(out, "Refactor API") {
		t.Error("expected Refactor API in view")
	}
	if !strings.Contains(out, "3/5") {
		t.Error("expected 3/5 workers in view")
	}
}

func TestModel_SwarmSectionSkippedWhenEmpty(t *testing.T) {
	report := sampleReport()
	// BySwarm is nil/empty by default
	svc := &mockReportService{report: report}
	m := New(svc)
	m.width = 120
	m.height = 40
	m, _ = m.Update(ReportLoadedMsg{Report: report})

	out := m.View()
	if strings.Contains(out, "SWARMS") {
		t.Error("expected no SWARMS section when BySwarm is empty")
	}
}

func TestModel_DirectorySectionRendered(t *testing.T) {
	report := sampleReport()
	report.ByDirectory = []service.DirectoryStats{
		{Directory: "/projects/app", Working: 2 * time.Hour, Waiting: 30 * time.Minute, TaskCount: 2},
		{Directory: "/projects/api", Working: 1 * time.Hour, Waiting: 0, TaskCount: 1},
	}
	svc := &mockReportService{report: report}
	m := New(svc)
	m.width = 120
	m.height = 40
	m, _ = m.Update(ReportLoadedMsg{Report: report})

	out := m.View()
	if !strings.Contains(out, "DIRECTORIES") {
		t.Error("expected DIRECTORIES section in view")
	}
	if !strings.Contains(out, "/projects/app") {
		t.Error("expected /projects/app in view")
	}
	if !strings.Contains(out, "/projects/api") {
		t.Error("expected /projects/api in view")
	}
}
