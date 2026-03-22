package service

import (
	"time"

	"github.com/cpave3/legato/internal/engine/analytics"
)

// Report is the complete analytics report for a time period.
// It contains only plain data types — no rendering logic.
type Report struct {
	Period     analytics.TimeRange
	Summary    ReportSummary
	ByDay      []DayBreakdown
	ByTask     []TaskStats
	ByWorkspace []WorkspaceStats
}

// ReportSummary holds top-level aggregate metrics.
type ReportSummary struct {
	TotalWorking  time.Duration
	TotalWaiting  time.Duration
	TasksCompleted int
	TasksCreated   int
	AgentSessions  int
	PRsMerged      int
}

// DayBreakdown holds duration totals for a single day.
type DayBreakdown struct {
	Date    time.Time
	Working time.Duration
	Waiting time.Duration
}

// TaskStats holds per-task analytics for the report.
type TaskStats struct {
	TaskID        string
	DisplayID     string // remote_id if synced, else task ID
	Title         string
	WorkspaceName string
	Provider      string // "" for local, "jira" for synced, etc.
	Working       time.Duration
	Waiting       time.Duration
}

// WorkspaceStats holds per-workspace analytics.
type WorkspaceStats struct {
	WorkspaceID   *int
	WorkspaceName string
	WorkspaceColor string
	Working       time.Duration
	Waiting       time.Duration
	TaskCount     int
}
