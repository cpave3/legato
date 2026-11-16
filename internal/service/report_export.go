package service

import (
	"fmt"
	"strings"
	"time"
)

// ExportReportMarkdown converts a Report to a formatted markdown string.
func ExportReportMarkdown(r *Report) string {
	if r == nil {
		return ""
	}

	var b strings.Builder

	b.WriteString(fmt.Sprintf("# %s Report\n\n", r.Period.Label))
	if !r.ActivityEpoch.IsZero() {
		b.WriteString(fmt.Sprintf("_Agent activity is recorded from %s. Working time is agent-time: it adds up agents that work at once, sub-agents included in Aggregate. Elapsed is wall-clock time with any agent active, counted once._\n\n", r.ActivityEpoch.Format(time.RFC3339)))
	}

	// Check if there's any activity
	if r.Summary.TotalWorking == 0 && r.Summary.TotalWaiting == 0 && r.Summary.TotalError == 0 && r.Summary.AggregateWorking == 0 &&
		r.Summary.TasksCreated == 0 && r.Summary.TasksCompleted == 0 {
		b.WriteString("No activity recorded for this period.\n")
		return b.String()
	}

	// Summary table
	b.WriteString("## Summary\n\n")
	b.WriteString("| Metric | Value |\n")
	b.WriteString("|--------|-------|\n")
	b.WriteString(fmt.Sprintf("| Agent Active Time | %s |\n", fmtDuration(r.Summary.TotalWorking)))
	b.WriteString(fmt.Sprintf("| Waiting Time | %s |\n", fmtDuration(r.Summary.TotalWaiting)))
	b.WriteString(fmt.Sprintf("| Error Time | %s |\n", fmtDuration(r.Summary.TotalError)))
	b.WriteString(fmt.Sprintf("| Sub-agent Working | %s |\n", fmtDuration(r.Summary.SubagentWorking)))
	b.WriteString(fmt.Sprintf("| Aggregate Agent Working | %s |\n", fmtTreeDuration(r.Summary.AggregateWorking, r.Summary.SubagentCoverage)))
	b.WriteString(fmt.Sprintf("| Elapsed (wall clock) | %s |\n", fmtDuration(r.Summary.Elapsed)))
	if r.Summary.TotalWorking > 0 || r.Summary.TotalWaiting > 0 || r.Summary.TotalError > 0 {
		total := r.Summary.TotalWorking + r.Summary.TotalWaiting + r.Summary.TotalError
		ratio := float64(r.Summary.TotalWorking) / float64(total) * 100
		b.WriteString(fmt.Sprintf("| Active Ratio | %.0f%% |\n", ratio))
	}
	b.WriteString(fmt.Sprintf("| Tasks Created | %d |\n", r.Summary.TasksCreated))
	b.WriteString(fmt.Sprintf("| Tasks Completed | %d |\n", r.Summary.TasksCompleted))
	b.WriteString(fmt.Sprintf("| Agent Sessions | %d |\n", r.Summary.AgentSessions))
	b.WriteString(fmt.Sprintf("| PRs Merged | %d |\n", r.Summary.PRsMerged))
	b.WriteString("\n")

	// Daily breakdown
	if len(r.ByDay) > 0 {
		b.WriteString("## Daily Breakdown\n\n")
		b.WriteString("| Day | Working | Waiting | Error |\n")
		b.WriteString("|-----|---------|---------|-------|\n")
		for _, d := range r.ByDay {
			dayLabel := d.Date.Format("Mon Jan 2")
			b.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
				dayLabel, fmtDuration(d.Working), fmtDuration(d.Waiting), fmtDuration(d.Error)))
		}
		b.WriteString("\n")
	}

	// Task breakdown
	if len(r.ByTask) > 0 {
		b.WriteString("## Tasks\n\n")
		b.WriteString("| ID | Title | Own Working | Sub-agent Working | Aggregate Working | Elapsed | Waiting | Error |\n")
		b.WriteString("|----|-------|-------------|-------------------|-------------------|---------|---------|-------|\n")
		for _, t := range r.ByTask {
			title := t.Title
			if len(title) > 50 {
				title = title[:47] + "..."
			}
			b.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %s | %s |\n",
				t.DisplayID, title, fmtDuration(t.Working), fmtDuration(t.SubagentWorking), fmtTreeDuration(t.AggregateWorking, t.SubagentCoverage),
				fmtDuration(t.Elapsed), fmtDuration(t.Waiting), fmtDuration(t.Error)))
		}
		b.WriteString("\n")
	}

	if len(r.ByExecution) > 0 {
		b.WriteString("## Executions\n\n")
		b.WriteString("| Execution | Kind | Status | Task | Engram | Active | Waiting | Error | Wall Clock |\n")
		b.WriteString("|-----------|------|--------|------|--------|--------|---------|-------|------------|\n")
		for _, execution := range r.ByExecution {
			task := execution.TaskTitle
			if task == "" {
				task = execution.TaskID
			}
			b.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", execution.ID, execution.Kind, execution.Status, task, execution.Engram, fmtDuration(execution.Working), fmtDuration(execution.Waiting), fmtDuration(execution.Error), fmtDuration(execution.WallClock)))
		}
		b.WriteString("\n")
	}

	// Workspace breakdown
	if len(r.ByWorkspace) > 0 {
		b.WriteString("## Workspaces\n\n")
		b.WriteString("| Workspace | Working | Waiting | Error | Tasks |\n")
		b.WriteString("|-----------|---------|---------|-------|-------|\n")
		for _, ws := range r.ByWorkspace {
			b.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %d |\n",
				ws.WorkspaceName, fmtDuration(ws.Working), fmtDuration(ws.Waiting), fmtDuration(ws.Error), ws.TaskCount))
		}
		b.WriteString("\n")
	}

	// Swarm breakdown
	if len(r.BySwarm) > 0 {
		b.WriteString("## Swarms\n\n")
		b.WriteString("| Swarm | Working | Waiting | Error | Clock | Ratio | Workers |\n")
		b.WriteString("|-------|---------|---------|-------|-------|-------|--------|\n")
		for _, s := range r.BySwarm {
			title := s.Title
			if len(title) > 50 {
				title = title[:47] + "..."
			}
			var ratio string
			if s.ParallelRatio > 0 {
				ratio = fmt.Sprintf("%.1fx", s.ParallelRatio)
			} else {
				ratio = "-"
			}
			b.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %d/%d |\n",
				title, fmtDuration(s.Working), fmtDuration(s.Waiting), fmtDuration(s.Error), fmtDuration(s.WallClock), ratio, s.WorkerCount, s.SubtaskCount))
		}
		b.WriteString("\n")
	}

	// Directory breakdown
	if len(r.ByDirectory) > 0 {
		b.WriteString("## Directories\n\n")
		b.WriteString("| Directory | Working | Waiting | Error | Tasks |\n")
		b.WriteString("|-----------|---------|---------|-------|-------|\n")
		for _, d := range r.ByDirectory {
			b.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %d |\n",
				d.Directory, fmtDuration(d.Working), fmtDuration(d.Waiting), fmtDuration(d.Error), d.TaskCount))
		}
		b.WriteString("\n")
	}

	if incompleteTree(r) {
		b.WriteString("_" + incompleteTreeMark + " Sub-agent time is not recorded for some or all of this: agents from before Legato recorded sub-agents, and agent tools that do not report them (Codex, Chimera without its Legato mod). Those totals are the top-level agents' own time and a lower bound on the tree's._\n")
	}

	return b.String()
}

// incompleteTreeMark follows an aggregate whose sub-agent time is incomplete.
const incompleteTreeMark = "†"

// fmtTreeDuration is an aggregate, marked when its sub-agent time is
// incomplete.
func fmtTreeDuration(d time.Duration, coverage string) string {
	if (TreeTime{SubagentCoverage: coverage}).Incomplete() {
		return fmtDuration(d) + " " + incompleteTreeMark
	}
	return fmtDuration(d)
}

func incompleteTree(r *Report) bool {
	if r.Summary.Incomplete() {
		return true
	}
	for _, t := range r.ByTask {
		if t.Incomplete() {
			return true
		}
	}
	return false
}

func fmtDuration(d time.Duration) string {
	if d == 0 {
		return "-"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm", m)
	}
	return "<1m"
}
