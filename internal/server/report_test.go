package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/analytics"
	"github.com/cpave3/legato/internal/service"
)

type fakeReportService struct {
	ranges  []analytics.TimeRange
	reports []*service.Report
	err     error
}

func (f *fakeReportService) GenerateReport(_ context.Context, period analytics.TimeRange) (*service.Report, error) {
	f.ranges = append(f.ranges, period)
	if f.err != nil {
		return nil, f.err
	}
	if len(f.reports) >= len(f.ranges) {
		report := *f.reports[len(f.ranges)-1]
		report.Period = period
		return &report, nil
	}
	return &service.Report{Period: period}, nil
}

func reportRequest(t *testing.T, srv *Server, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestReportAPIUsesAuthenticatedRouteAndDefaultsToCurrentAndPreviousWeek(t *testing.T) {
	fake := &fakeReportService{}
	srv := New(nil, nil, nil, "")
	srv.SetAuthToken("secret")
	srv.SetReportService(fake)

	unauthorized := httptest.NewRecorder()
	srv.Handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/report", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	rec := reportRequest(t, srv, http.MethodGet, "/api/report")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(fake.ranges) != 2 {
		t.Fatalf("GenerateReport calls = %d, want 2", len(fake.ranges))
	}
	current := fake.ranges[0]
	if current.Period != analytics.PeriodWeek || current.Label != "This Week" {
		t.Fatalf("current range = %+v", current)
	}
	previous := analytics.Previous(current)
	if !fake.ranges[1].Start.Equal(previous.Start) || !fake.ranges[1].End.Equal(previous.End) {
		t.Fatalf("comparison range = %+v, want %+v", fake.ranges[1], previous)
	}
}

func TestReportAPIPeriodsAndOffsetsUseAnalyticsRanges(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		base       func() analytics.TimeRange
		offset     int
		periodType analytics.PeriodType
	}{
		{name: "day current", target: "/api/report?period=day", base: analytics.Today, periodType: analytics.PeriodDay},
		{name: "week previous", target: "/api/report?period=week&offset=-2", base: analytics.ThisWeek, offset: -2, periodType: analytics.PeriodWeek},
		{name: "month previous", target: "/api/report?period=month&offset=-1", base: analytics.ThisMonth, offset: -1, periodType: analytics.PeriodMonth},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeReportService{}
			srv := New(nil, nil, nil, "")
			srv.SetReportService(fake)
			rec := reportRequest(t, srv, http.MethodGet, tt.target)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			want := tt.base()
			for range -tt.offset {
				want = analytics.Previous(want)
			}
			got := fake.ranges[0]
			endMatches := got.End.Equal(want.End)
			if tt.offset == 0 {
				endMatches = got.End.Sub(want.End).Abs() < time.Second
			}
			if got.Period != tt.periodType || !got.Start.Equal(want.Start) || !endMatches {
				t.Fatalf("selected range = %+v, want %+v", got, want)
			}
			comparison := analytics.Previous(got)
			if !fake.ranges[1].Start.Equal(comparison.Start) || !fake.ranges[1].End.Equal(comparison.End) {
				t.Fatalf("comparison range = %+v, want %+v", fake.ranges[1], comparison)
			}
		})
	}
}

func TestReportAPIEncodesStableSecondsDTOAndEmptyArrays(t *testing.T) {
	workspaceID := 7
	day := time.Date(2026, time.August, 3, 0, 0, 0, 0, time.Local)
	fake := &fakeReportService{reports: []*service.Report{
		{
			ActivityEpoch: time.Date(2026, time.August, 3, 9, 0, 0, 0, time.UTC), ActivityAvailable: true,
			Summary:     service.ReportSummary{TotalWorking: 90 * time.Minute, TotalWaiting: 15 * time.Minute, TotalError: 5 * time.Minute, TasksCreated: 4, TasksCompleted: 3, AgentSessions: 7, PRsMerged: 2},
			ByDay:       []service.DayBreakdown{{Date: day, Working: time.Hour, Waiting: 10 * time.Minute, Error: 3 * time.Minute}},
			ByTask:      []service.TaskStats{{TaskID: "task-1", DisplayID: "LEG-42", Title: "Build reports", WorkspaceName: "Legato", Provider: "jira", Working: time.Hour, Waiting: 5 * time.Minute, Error: 2 * time.Minute}},
			ByWorkspace: []service.WorkspaceStats{{WorkspaceID: &workspaceID, WorkspaceName: "Legato", WorkspaceColor: "#abc123", Working: time.Hour, Waiting: 5 * time.Minute, Error: 2 * time.Minute, TaskCount: 2}},
			ByDirectory: []service.DirectoryStats{{Directory: "/work/legato", Working: 45 * time.Minute, Waiting: 3 * time.Minute, Error: 1 * time.Minute, TaskCount: 1}},
			BySwarm:     []service.SwarmStats{{ParentTaskID: "task-1", Title: "Report swarm", Working: 2 * time.Hour, Waiting: 30 * time.Minute, Error: 10 * time.Minute, WallClock: time.Hour, ParallelRatio: 2.5, WorkerCount: 3, SubtaskCount: 4}},
			ByExecution: []service.ExecutionStats{{ID: "ex-1", Kind: "workflow_agent", Status: "succeeded", TaskID: "task-1", TaskTitle: "Build reports", WorkflowRunID: "run-1", Working: time.Hour, Waiting: 5 * time.Minute, Error: 2 * time.Minute, WallClock: 70 * time.Minute, StartedAt: day, EndedAt: func() *time.Time { ended := day.Add(70 * time.Minute); return &ended }()}},
		},
		{Summary: service.ReportSummary{TotalWorking: time.Hour, TotalWaiting: 20 * time.Minute, TasksCreated: 2, TasksCompleted: 1, AgentSessions: 4, PRsMerged: 1}},
	}}
	srv := New(nil, nil, nil, "")
	srv.SetReportService(fake)
	rec := reportRequest(t, srv, http.MethodGet, "/api/report?period=week")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	report := body["report"].(map[string]any)
	if report["activity_epoch"] == "" || report["activity_available"] != true {
		t.Fatalf("activity metadata = %#v", report)
	}
	period := report["period"].(map[string]any)
	if period["kind"] != "week" || period["offset"] != float64(0) || period["start"] == "" || period["end"] == "" {
		t.Fatalf("period = %#v", period)
	}
	if _, err := time.Parse(time.RFC3339Nano, period["start"].(string)); err != nil {
		t.Fatalf("start is not RFC3339: %v", err)
	}
	summary := report["summary"].(map[string]any)
	if summary["working_seconds"] != float64(5400) || summary["waiting_seconds"] != float64(900) || summary["error_seconds"] != float64(300) {
		t.Fatalf("summary durations = %#v", summary)
	}
	if report["by_day"].([]any)[0].(map[string]any)["date"] != "2026-08-03" {
		t.Fatalf("by_day = %#v", report["by_day"])
	}
	task := report["by_task"].([]any)[0].(map[string]any)
	if task["display_id"] != "LEG-42" || task["working_seconds"] != float64(3600) {
		t.Fatalf("task = %#v", task)
	}
	workspace := report["by_workspace"].([]any)[0].(map[string]any)
	if workspace["workspace_id"] != float64(7) || workspace["workspace_color"] != "#abc123" {
		t.Fatalf("workspace = %#v", workspace)
	}
	project := report["by_directory"].([]any)[0].(map[string]any)
	if project["directory"] != "/work/legato" || project["task_count"] != float64(1) {
		t.Fatalf("directory = %#v", project)
	}
	swarm := report["by_swarm"].([]any)[0].(map[string]any)
	if swarm["wall_clock_seconds"] != float64(3600) || swarm["parallel_ratio"] != 2.5 || swarm["worker_count"] != float64(3) {
		t.Fatalf("swarm = %#v", swarm)
	}
	execution := report["by_execution"].([]any)[0].(map[string]any)
	if execution["id"] != "ex-1" || execution["kind"] != "workflow_agent" || execution["wall_clock_seconds"] != float64(4200) {
		t.Fatalf("execution = %#v", execution)
	}
	comparison := body["comparison"].(map[string]any)
	if comparison["summary"].(map[string]any)["working_seconds"] != float64(3600) {
		t.Fatalf("comparison = %#v", comparison)
	}

	emptyFake := &fakeReportService{}
	emptyServer := New(nil, nil, nil, "")
	emptyServer.SetReportService(emptyFake)
	empty := reportRequest(t, emptyServer, http.MethodGet, "/api/report")
	var emptyBody struct {
		Report struct {
			ByDay       []any `json:"by_day"`
			ByTask      []any `json:"by_task"`
			ByWorkspace []any `json:"by_workspace"`
			ByDirectory []any `json:"by_directory"`
			BySwarm     []any `json:"by_swarm"`
			ByExecution []any `json:"by_execution"`
		} `json:"report"`
	}
	if err := json.Unmarshal(empty.Body.Bytes(), &emptyBody); err != nil {
		t.Fatal(err)
	}
	if emptyBody.Report.ByDay == nil || emptyBody.Report.ByTask == nil || emptyBody.Report.ByWorkspace == nil || emptyBody.Report.ByDirectory == nil || emptyBody.Report.BySwarm == nil || emptyBody.Report.ByExecution == nil {
		t.Fatalf("empty arrays encoded as null: %s", empty.Body.String())
	}
}

func TestReportAPIMarkdownUsesSelectedReportOnly(t *testing.T) {
	fake := &fakeReportService{reports: []*service.Report{{Summary: service.ReportSummary{TotalWorking: time.Hour}}}}
	srv := New(nil, nil, nil, "")
	srv.SetReportService(fake)
	rec := reportRequest(t, srv, http.MethodGet, "/api/report?period=day&offset=-1&format=markdown")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
	if len(fake.ranges) != 1 {
		t.Fatalf("GenerateReport calls = %d, want 1", len(fake.ranges))
	}
	if !strings.Contains(rec.Body.String(), "# "+fake.ranges[0].Label+" Report") || !strings.Contains(rec.Body.String(), "| Agent Active Time | 1h |") {
		t.Fatalf("markdown = %q", rec.Body.String())
	}
}

func TestReportAPIRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name   string
		method string
		target string
		status int
	}{
		{name: "method", method: http.MethodPost, target: "/api/report", status: http.StatusMethodNotAllowed},
		{name: "period", method: http.MethodGet, target: "/api/report?period=quarter", status: http.StatusBadRequest},
		{name: "malformed offset", method: http.MethodGet, target: "/api/report?offset=nope", status: http.StatusBadRequest},
		{name: "future offset", method: http.MethodGet, target: "/api/report?offset=1", status: http.StatusBadRequest},
		{name: "old offset", method: http.MethodGet, target: "/api/report?offset=-121", status: http.StatusBadRequest},
		{name: "format", method: http.MethodGet, target: "/api/report?format=csv", status: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeReportService{}
			srv := New(nil, nil, nil, "")
			srv.SetReportService(fake)
			rec := reportRequest(t, srv, tt.method, tt.target)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tt.status, rec.Body.String())
			}
			if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
			}
			if len(fake.ranges) != 0 {
				t.Fatalf("service called for invalid request")
			}
		})
	}
}

func TestReportAPIHandlesUnavailableAndFailedService(t *testing.T) {
	srv := New(nil, nil, nil, "")
	unavailable := reportRequest(t, srv, http.MethodGet, "/api/report")
	if unavailable.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status = %d, body = %s", unavailable.Code, unavailable.Body.String())
	}

	failed := New(nil, nil, nil, "")
	failed.SetReportService(&fakeReportService{err: errors.New("database password leaked")})
	rec := reportRequest(t, failed, http.MethodGet, "/api/report")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("failure status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "password") || !strings.Contains(rec.Body.String(), "internal server error") {
		t.Fatalf("unsafe error body = %q", rec.Body.String())
	}
}
