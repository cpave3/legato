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

	// Check if there's any activity
	if r.Summary.TotalWorking == 0 && r.Summary.TotalWaiting == 0 &&
		r.Summary.TasksCreated == 0 && r.Summary.TasksCompleted == 0 {
		b.WriteString("No activity recorded for this period.\n")
		return b.String()
	}

	// Summary table
	b.WriteString("## Summary\n\n")
	b.WriteString("| Metric | Value |\n")
	b.WriteString("|--------|-------|\n")
	b.WriteString(fmt.Sprintf("| Working Time | %s |\n", fmtDuration(r.Summary.TotalWorking)))
	b.WriteString(fmt.Sprintf("| Waiting Time | %s |\n", fmtDuration(r.Summary.TotalWaiting)))
	if r.Summary.TotalWorking > 0 || r.Summary.TotalWaiting > 0 {
		total := r.Summary.TotalWorking + r.Summary.TotalWaiting
		ratio := float64(r.Summary.TotalWorking) / float64(total) * 100
		b.WriteString(fmt.Sprintf("| Working Ratio | %.0f%% |\n", ratio))
	}
	b.WriteString(fmt.Sprintf("| Tasks Created | %d |\n", r.Summary.TasksCreated))
	b.WriteString(fmt.Sprintf("| Tasks Completed | %d |\n", r.Summary.TasksCompleted))
	b.WriteString(fmt.Sprintf("| Agent Sessions | %d |\n", r.Summary.AgentSessions))
	b.WriteString(fmt.Sprintf("| PRs Merged | %d |\n", r.Summary.PRsMerged))
	b.WriteString("\n")

	// Daily breakdown
	if len(r.ByDay) > 0 {
		b.WriteString("## Daily Breakdown\n\n")
		b.WriteString("| Day | Working | Waiting |\n")
		b.WriteString("|-----|---------|--------|\n")
		for _, d := range r.ByDay {
			dayLabel := d.Date.Format("Mon Jan 2")
			b.WriteString(fmt.Sprintf("| %s | %s | %s |\n",
				dayLabel, fmtDuration(d.Working), fmtDuration(d.Waiting)))
		}
		b.WriteString("\n")
	}

	// Task breakdown
	if len(r.ByTask) > 0 {
		b.WriteString("## Tasks\n\n")
		b.WriteString("| ID | Title | Working | Waiting |\n")
		b.WriteString("|----|-------|---------|--------|\n")
		for _, t := range r.ByTask {
			title := t.Title
			if len(title) > 50 {
				title = title[:47] + "..."
			}
			b.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
				t.DisplayID, title, fmtDuration(t.Working), fmtDuration(t.Waiting)))
		}
		b.WriteString("\n")
	}

	// Workspace breakdown
	if len(r.ByWorkspace) > 0 {
		b.WriteString("## Workspaces\n\n")
		b.WriteString("| Workspace | Working | Waiting | Tasks |\n")
		b.WriteString("|-----------|---------|---------|-------|\n")
		for _, ws := range r.ByWorkspace {
			b.WriteString(fmt.Sprintf("| %s | %s | %s | %d |\n",
				ws.WorkspaceName, fmtDuration(ws.Working), fmtDuration(ws.Waiting), ws.TaskCount))
		}
		b.WriteString("\n")
	}

	return b.String()
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
