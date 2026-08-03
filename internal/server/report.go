package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/cpave3/legato/internal/engine/analytics"
	"github.com/cpave3/legato/internal/service"
)

const minimumReportOffset = -120

type reportPeriodResponse struct {
	Kind   string `json:"kind"`
	Offset int    `json:"offset"`
	Label  string `json:"label"`
	Start  string `json:"start"`
	End    string `json:"end"`
}

type reportSummaryResponse struct {
	WorkingSeconds int64 `json:"working_seconds"`
	WaitingSeconds int64 `json:"waiting_seconds"`
	TasksCreated   int   `json:"tasks_created"`
	TasksCompleted int   `json:"tasks_completed"`
	AgentSessions  int   `json:"agent_sessions"`
	PRsMerged      int   `json:"prs_merged"`
}

type reportDayResponse struct {
	Date           string `json:"date"`
	WorkingSeconds int64  `json:"working_seconds"`
	WaitingSeconds int64  `json:"waiting_seconds"`
}

type reportTaskResponse struct {
	TaskID         string `json:"task_id"`
	DisplayID      string `json:"display_id"`
	Title          string `json:"title"`
	WorkspaceName  string `json:"workspace_name"`
	Provider       string `json:"provider"`
	WorkingSeconds int64  `json:"working_seconds"`
	WaitingSeconds int64  `json:"waiting_seconds"`
}

type reportWorkspaceResponse struct {
	WorkspaceID    *int   `json:"workspace_id"`
	WorkspaceName  string `json:"workspace_name"`
	WorkspaceColor string `json:"workspace_color"`
	WorkingSeconds int64  `json:"working_seconds"`
	WaitingSeconds int64  `json:"waiting_seconds"`
	TaskCount      int    `json:"task_count"`
}

type reportDirectoryResponse struct {
	Directory      string `json:"directory"`
	WorkingSeconds int64  `json:"working_seconds"`
	WaitingSeconds int64  `json:"waiting_seconds"`
	TaskCount      int    `json:"task_count"`
}

type reportSwarmResponse struct {
	ParentTaskID     string  `json:"parent_task_id"`
	Title            string  `json:"title"`
	WorkingSeconds   int64   `json:"working_seconds"`
	WaitingSeconds   int64   `json:"waiting_seconds"`
	WallClockSeconds int64   `json:"wall_clock_seconds"`
	ParallelRatio    float64 `json:"parallel_ratio"`
	WorkerCount      int     `json:"worker_count"`
	SubtaskCount     int     `json:"subtask_count"`
}

type reportResponse struct {
	Period      reportPeriodResponse      `json:"period"`
	Summary     reportSummaryResponse     `json:"summary"`
	ByDay       []reportDayResponse       `json:"by_day"`
	ByTask      []reportTaskResponse      `json:"by_task"`
	ByWorkspace []reportWorkspaceResponse `json:"by_workspace"`
	ByDirectory []reportDirectoryResponse `json:"by_directory"`
	BySwarm     []reportSwarmResponse     `json:"by_swarm"`
}

type reportComparisonResponse struct {
	Period  reportPeriodResponse  `json:"period"`
	Summary reportSummaryResponse `json:"summary"`
}

type reportEnvelopeResponse struct {
	Report     reportResponse           `json:"report"`
	Comparison reportComparisonResponse `json:"comparison"`
}

func (s *Server) reportHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.report == nil {
			s.writeError(w, http.StatusServiceUnavailable, "report service not available")
			return
		}

		periodKind := r.URL.Query().Get("period")
		if periodKind == "" {
			periodKind = "week"
		}
		selected, ok := currentReportRange(periodKind)
		if !ok {
			s.writeError(w, http.StatusBadRequest, "invalid report period")
			return
		}

		offset := 0
		if raw := r.URL.Query().Get("offset"); raw != "" {
			var err error
			offset, err = strconv.Atoi(raw)
			if err != nil || offset > 0 || offset < minimumReportOffset {
				s.writeError(w, http.StatusBadRequest, "invalid report offset")
				return
			}
		}
		for i := 0; i > offset; i-- {
			selected = analytics.Previous(selected)
		}

		format := r.URL.Query().Get("format")
		if format == "" {
			format = "json"
		}
		if format != "json" && format != "markdown" {
			s.writeError(w, http.StatusBadRequest, "invalid report format")
			return
		}

		report, err := s.report.GenerateReport(r.Context(), selected)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		if format == "markdown" {
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			_, _ = w.Write([]byte(service.ExportReportMarkdown(report)))
			return
		}

		comparisonRange := analytics.Previous(selected)
		comparison, err := s.report.GenerateReport(r.Context(), comparisonRange)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		s.writeJSON(w, http.StatusOK, reportEnvelopeResponse{
			Report: mapReportResponse(report, periodKind, offset),
			Comparison: reportComparisonResponse{
				Period:  mapReportPeriod(comparisonRange, periodKind, offset-1),
				Summary: mapReportSummary(comparison.Summary),
			},
		})
	}
}

func currentReportRange(kind string) (analytics.TimeRange, bool) {
	switch kind {
	case "day":
		return analytics.Today(), true
	case "week":
		return analytics.ThisWeek(), true
	case "month":
		return analytics.ThisMonth(), true
	default:
		return analytics.TimeRange{}, false
	}
}

func mapReportResponse(report *service.Report, kind string, offset int) reportResponse {
	response := reportResponse{
		Period:      mapReportPeriod(report.Period, kind, offset),
		Summary:     mapReportSummary(report.Summary),
		ByDay:       make([]reportDayResponse, len(report.ByDay)),
		ByTask:      make([]reportTaskResponse, len(report.ByTask)),
		ByWorkspace: make([]reportWorkspaceResponse, len(report.ByWorkspace)),
		ByDirectory: make([]reportDirectoryResponse, len(report.ByDirectory)),
		BySwarm:     make([]reportSwarmResponse, len(report.BySwarm)),
	}
	for i, day := range report.ByDay {
		response.ByDay[i] = reportDayResponse{Date: day.Date.Format("2006-01-02"), WorkingSeconds: durationSeconds(day.Working), WaitingSeconds: durationSeconds(day.Waiting)}
	}
	for i, task := range report.ByTask {
		response.ByTask[i] = reportTaskResponse{TaskID: task.TaskID, DisplayID: task.DisplayID, Title: task.Title, WorkspaceName: task.WorkspaceName, Provider: task.Provider, WorkingSeconds: durationSeconds(task.Working), WaitingSeconds: durationSeconds(task.Waiting)}
	}
	for i, workspace := range report.ByWorkspace {
		response.ByWorkspace[i] = reportWorkspaceResponse{WorkspaceID: workspace.WorkspaceID, WorkspaceName: workspace.WorkspaceName, WorkspaceColor: workspace.WorkspaceColor, WorkingSeconds: durationSeconds(workspace.Working), WaitingSeconds: durationSeconds(workspace.Waiting), TaskCount: workspace.TaskCount}
	}
	for i, directory := range report.ByDirectory {
		response.ByDirectory[i] = reportDirectoryResponse{Directory: directory.Directory, WorkingSeconds: durationSeconds(directory.Working), WaitingSeconds: durationSeconds(directory.Waiting), TaskCount: directory.TaskCount}
	}
	for i, swarm := range report.BySwarm {
		response.BySwarm[i] = reportSwarmResponse{ParentTaskID: swarm.ParentTaskID, Title: swarm.Title, WorkingSeconds: durationSeconds(swarm.Working), WaitingSeconds: durationSeconds(swarm.Waiting), WallClockSeconds: durationSeconds(swarm.WallClock), ParallelRatio: swarm.ParallelRatio, WorkerCount: swarm.WorkerCount, SubtaskCount: swarm.SubtaskCount}
	}
	return response
}

func mapReportPeriod(period analytics.TimeRange, kind string, offset int) reportPeriodResponse {
	return reportPeriodResponse{Kind: kind, Offset: offset, Label: period.Label, Start: period.Start.Format(time.RFC3339Nano), End: period.End.Format(time.RFC3339Nano)}
}

func mapReportSummary(summary service.ReportSummary) reportSummaryResponse {
	return reportSummaryResponse{WorkingSeconds: durationSeconds(summary.TotalWorking), WaitingSeconds: durationSeconds(summary.TotalWaiting), TasksCreated: summary.TasksCreated, TasksCompleted: summary.TasksCompleted, AgentSessions: summary.AgentSessions, PRsMerged: summary.PRsMerged}
}

func durationSeconds(duration time.Duration) int64 {
	return int64(duration / time.Second)
}
