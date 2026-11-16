package service

import (
	"time"

	"github.com/cpave3/legato/internal/engine/analytics"
	"github.com/cpave3/legato/internal/engine/store"
)

// Report is the complete analytics report for a time period.
// It contains only plain data types — no rendering logic.
type Report struct {
	Period            analytics.TimeRange
	ActivityEpoch     time.Time
	ActivityAvailable bool
	Summary           ReportSummary
	ByDay             []DayBreakdown
	ByTask            []TaskStats
	ByWorkspace       []WorkspaceStats
	BySwarm           []SwarmStats
	ByDirectory       []DirectoryStats
	ByExecution       []ExecutionStats
}

// ReportSummary holds top-level aggregate metrics.
//
// TotalWorking, TotalWaiting and TotalError are the top-level agents' own
// time (one per execution). The Subagent fields are their sub-agents' time,
// AggregateWorking the two added together, and Elapsed the wall-clock time
// during which any of them was active. See TreeTime.
type ReportSummary struct {
	TotalWorking time.Duration
	TotalWaiting time.Duration
	TotalError   time.Duration
	TreeTime
	TasksCompleted int
	TasksCreated   int
	AgentSessions  int
	PRsMerged      int
}

// DayBreakdown holds duration totals for a single day. Working, Waiting and
// Error are the top-level agents' own; SubagentWorking is their sub-agents'
// and AggregateWorking the two together.
type DayBreakdown struct {
	Date             time.Time
	Working          time.Duration
	Waiting          time.Duration
	Error            time.Duration
	SubagentWorking  time.Duration
	AggregateWorking time.Duration
}

// Sub-agent coverage of a TreeTime (see its SubagentCoverage).
const (
	SubagentCoverageComplete = store.SubagentCoverageComplete
	SubagentCoveragePartial  = store.SubagentCoveragePartial
	SubagentCoverageNone     = store.SubagentCoverageNone
)

// Incomplete is whether the sub-agent time is missing work: some or all of
// the executions counted did not report their sub-agents.
func (t TreeTime) Incomplete() bool {
	return t.SubagentCoverage == SubagentCoveragePartial || t.SubagentCoverage == SubagentCoverageNone
}

// TreeTime is the time of whole agent trees: the sub-agents the top-level
// agents ran, at any depth, and wall-clock time. Its definitions:
//
//   - SubagentWorking and SubagentWaiting: each sub-agent's own working and
//     waiting spans, counted once, whatever its parent was doing meanwhile.
//   - AggregateWorking: every agent's working time added together, the
//     top-level agents' own and their sub-agents': agent-time, in which
//     agents working at once each count. Five sub-agents working the same
//     10 minutes make 50 minutes.
//   - Elapsed: wall-clock time during which any agent of the tree was
//     working, waiting or in error, agents active at once counted once
//     (those five make 10 minutes). It is not the operator's own time.
//   - SubagentCoverage: SubagentCoverageComplete when every execution
//     counted reported its sub-agents, Partial when some did, None when none
//     did (Codex, or any execution from before sub-agents were recorded):
//     then the sub-agent figures miss work and AggregateWorking is a floor.
//     "" when nothing ran.
type TreeTime struct {
	SubagentWorking  time.Duration
	SubagentWaiting  time.Duration
	AggregateWorking time.Duration
	Elapsed          time.Duration
	SubagentCount    int
	SubagentCoverage string
}

// TaskStats holds per-task analytics for the report.
type TaskStats struct {
	TaskID        string
	DisplayID     string // remote_id if synced, else task ID
	Title         string
	WorkspaceName string
	Provider      string // "" for local, "jira" for synced, etc.
	// Working, Waiting and Error are the task's top-level agents' own time.
	Working time.Duration
	Waiting time.Duration
	Error   time.Duration
	TreeTime
}

// WorkspaceStats holds per-workspace analytics.
type WorkspaceStats struct {
	WorkspaceID    *int
	WorkspaceName  string
	WorkspaceColor string
	Working        time.Duration
	Waiting        time.Duration
	Error          time.Duration
	TaskCount      int
}

// DirectoryStats holds per-directory analytics.
type DirectoryStats struct {
	Directory string
	Working   time.Duration
	Waiting   time.Duration
	Error     time.Duration
	TaskCount int
}

type ExecutionStats struct {
	ID            string
	Kind          string
	Status        string
	TaskID        string
	TaskTitle     string
	WorkflowRunID string
	// Engram is the engram the execution ran as, "" for none.
	Engram    string
	Working   time.Duration
	Waiting   time.Duration
	Error     time.Duration
	WallClock time.Duration
	StartedAt time.Time
	EndedAt   *time.Time
	TreeTime
}

// SwarmStats holds per-swarm analytics.
type SwarmStats struct {
	ParentTaskID  string
	Title         string
	Working       time.Duration
	Waiting       time.Duration
	Error         time.Duration
	WallClock     time.Duration // earliest effective_start → latest effective_end across intervals
	ParallelRatio float64       // (Working+Waiting) / WallClock; 0 if WallClock==0
	WorkerCount   int
	SubtaskCount  int
}
