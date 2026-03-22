package report

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/engine/analytics"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

// Model is the report view Bubbletea model.
type Model struct {
	reportSvc  service.ReportService
	report     *service.Report
	period     analytics.TimeRange
	periodType analytics.PeriodType
	loading    bool
	taskScroll int
	width      int
	height     int
}

// New creates a new report view model.
func New(reportSvc service.ReportService) Model {
	return Model{
		reportSvc:  reportSvc,
		periodType: analytics.PeriodDay,
	}
}

// SetSize updates the dimensions.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Init returns a command to load the initial report.
func (m Model) Init() tea.Cmd {
	return m.loadReport(analytics.Today())
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case ReportLoadedMsg:
		m.loading = false
		if msg.Err == nil && msg.Report != nil {
			m.report = msg.Report
			m.period = msg.Report.Period
			m.taskScroll = 0
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "tab":
		m.periodType = (m.periodType + 1) % 3
		return m, m.loadCurrentPeriod()
	case "h", "left":
		prev := analytics.Previous(m.period)
		return m, m.loadReport(prev)
	case "l", "right":
		next := analytics.Next(m.period)
		if next.Start.Equal(m.period.Start) {
			return m, nil // already at current
		}
		return m, m.loadReport(next)
	case "j", "down":
		if m.report != nil && m.taskScroll < len(m.report.ByTask)-1 {
			m.taskScroll++
		}
		return m, nil
	case "k", "up":
		if m.taskScroll > 0 {
			m.taskScroll--
		}
		return m, nil
	case "R":
		if m.report != nil {
			md := service.ExportReportMarkdown(m.report)
			return m, func() tea.Msg { return CopyReportMsg{Markdown: md} }
		}
		return m, nil
	case "esc":
		return m, func() tea.Msg { return ReturnToBoardMsg{} }
	}
	return m, nil
}

func (m Model) loadCurrentPeriod() tea.Cmd {
	switch m.periodType {
	case analytics.PeriodDay:
		return m.loadReport(analytics.Today())
	case analytics.PeriodWeek:
		return m.loadReport(analytics.ThisWeek())
	case analytics.PeriodMonth:
		return m.loadReport(analytics.ThisMonth())
	}
	return nil
}

func (m *Model) loadReport(tr analytics.TimeRange) tea.Cmd {
	m.loading = true
	svc := m.reportSvc
	return func() tea.Msg {
		report, err := svc.GenerateReport(context.Background(), tr)
		return ReportLoadedMsg{Report: report, Err: err}
	}
}

// View renders the report view.
func (m Model) View() string {
	if m.width == 0 {
		return ""
	}

	if m.loading && m.report == nil {
		return lipgloss.NewStyle().
			Width(m.width).
			Height(m.height).
			Foreground(theme.TextTertiary).
			Padding(2, 4).
			Render("Loading report...")
	}

	if m.report == nil {
		return lipgloss.NewStyle().
			Width(m.width).
			Height(m.height).
			Foreground(theme.TextTertiary).
			Padding(2, 4).
			Render("No report data")
	}

	contentWidth := m.width - 4 // padding

	// Header with period nav
	header := m.renderHeader(contentWidth)

	// Summary panel
	summary := m.renderSummary(contentWidth)

	// Daily chart
	chart := m.renderDailyChart(contentWidth)

	// Task table
	taskTable := m.renderTaskTable(contentWidth)

	// Workspace breakdown
	workspace := m.renderWorkspaces(contentWidth)

	// Hints
	hints := m.renderHints(contentWidth)

	sections := []string{header, summary}
	if chart != "" {
		sections = append(sections, chart)
	}
	if taskTable != "" {
		sections = append(sections, taskTable)
	}
	if workspace != "" {
		sections = append(sections, workspace)
	}
	sections = append(sections, hints)

	content := strings.Join(sections, "\n\n")

	return lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Padding(1, 2).
		Render(content)
}

func (m Model) renderHeader(width int) string {
	titleStyle := lipgloss.NewStyle().Foreground(theme.AccentPurple).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
	activeStyle := lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true)

	// Period type tabs
	types := []string{"Day", "Week", "Month"}
	var tabs []string
	for i, t := range types {
		if analytics.PeriodType(i) == m.periodType {
			tabs = append(tabs, activeStyle.Render("["+t+"]"))
		} else {
			tabs = append(tabs, dimStyle.Render(" "+t+" "))
		}
	}

	left := titleStyle.Render("REPORT") + "  " + strings.Join(tabs, " ")
	nav := dimStyle.Render("◀ h") + "  " + titleStyle.Render(m.period.Label) + "  " + dimStyle.Render("l ▶")

	gap := width - lipgloss.Width(left) - lipgloss.Width(nav)
	if gap < 1 {
		gap = 1
	}

	return left + strings.Repeat(" ", gap) + nav
}

func (m Model) renderSummary(width int) string {
	s := m.report.Summary
	sectionStyle := lipgloss.NewStyle().Foreground(theme.TextSecondary).Bold(true)

	var lines []string
	lines = append(lines, sectionStyle.Render("SUMMARY"))

	metricStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
	valueStyle := lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true)
	workingStyle := lipgloss.NewStyle().Foreground(theme.SyncOK).Bold(true)
	waitingStyle := lipgloss.NewStyle().Foreground(theme.ColReady).Bold(true)

	total := s.TotalWorking + s.TotalWaiting
	var ratioStr string
	if total > 0 {
		ratio := float64(s.TotalWorking) / float64(total) * 100
		ratioStr = fmt.Sprintf("%.0f%%", ratio)
	} else {
		ratioStr = "-"
	}

	row1 := fmt.Sprintf("%s %s   %s %s   %s %s",
		metricStyle.Render("Working:"), workingStyle.Render(fmtDuration(s.TotalWorking)),
		metricStyle.Render("Waiting:"), waitingStyle.Render(fmtDuration(s.TotalWaiting)),
		metricStyle.Render("Ratio:"), valueStyle.Render(ratioStr),
	)
	row2 := fmt.Sprintf("%s %s   %s %s   %s %s   %s %s",
		metricStyle.Render("Created:"), valueStyle.Render(fmt.Sprintf("%d", s.TasksCreated)),
		metricStyle.Render("Completed:"), valueStyle.Render(fmt.Sprintf("%d", s.TasksCompleted)),
		metricStyle.Render("Sessions:"), valueStyle.Render(fmt.Sprintf("%d", s.AgentSessions)),
		metricStyle.Render("PRs Merged:"), valueStyle.Render(fmt.Sprintf("%d", s.PRsMerged)),
	)

	lines = append(lines, row1, row2)
	return strings.Join(lines, "\n")
}

func (m Model) renderDailyChart(width int) string {
	days := m.report.ByDay
	if len(days) == 0 {
		return ""
	}

	sectionStyle := lipgloss.NewStyle().Foreground(theme.TextSecondary).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
	workingBarStyle := lipgloss.NewStyle().Foreground(theme.SyncOK)
	waitingBarStyle := lipgloss.NewStyle().Foreground(theme.ColReady)

	var lines []string
	lines = append(lines, sectionStyle.Render("DAILY ACTIVITY"))

	// Find max duration for scaling
	var maxDur time.Duration
	for _, d := range days {
		total := d.Working + d.Waiting
		if total > maxDur {
			maxDur = total
		}
	}

	labelWidth := 10 // "Mon Mar 16"
	durationWidth := 20
	barWidth := width - labelWidth - durationWidth - 4
	if barWidth < 10 {
		barWidth = 10
	}

	for _, d := range days {
		label := d.Date.Format("Mon Jan _2")
		label = fmt.Sprintf("%-*s", labelWidth, label)

		var workingLen, waitingLen int
		if maxDur > 0 {
			workingLen = int(math.Round(float64(d.Working) / float64(maxDur) * float64(barWidth)))
			waitingLen = int(math.Round(float64(d.Waiting) / float64(maxDur) * float64(barWidth)))
		}

		bar := workingBarStyle.Render(strings.Repeat("█", workingLen)) +
			waitingBarStyle.Render(strings.Repeat("░", waitingLen))

		durLabel := ""
		if d.Working > 0 || d.Waiting > 0 {
			durLabel = dimStyle.Render(fmt.Sprintf(" %s / %s",
				fmtDuration(d.Working), fmtDuration(d.Waiting)))
		}

		lines = append(lines, dimStyle.Render(label)+" "+bar+durLabel)
	}

	return strings.Join(lines, "\n")
}

func (m Model) renderTaskTable(width int) string {
	tasks := m.report.ByTask
	if len(tasks) == 0 {
		return ""
	}

	sectionStyle := lipgloss.NewStyle().Foreground(theme.TextSecondary).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
	idStyle := lipgloss.NewStyle().Foreground(theme.AccentPurple)
	selectedStyle := lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true)

	var lines []string
	lines = append(lines, sectionStyle.Render("TASKS"))

	// Column widths
	idWidth := 12
	workingWidth := 10
	waitingWidth := 10
	titleWidth := width - idWidth - workingWidth - waitingWidth - 6
	if titleWidth < 20 {
		titleWidth = 20
	}

	// Header
	hdr := fmt.Sprintf("%-*s %-*s %*s %*s",
		idWidth, "ID", titleWidth, "Title", workingWidth, "Working", waitingWidth, "Waiting")
	lines = append(lines, dimStyle.Render(hdr))

	// Visible rows
	maxVisible := m.height/3 - 8 // rough estimate of available lines
	if maxVisible < 3 {
		maxVisible = 3
	}

	start := m.taskScroll
	if start > len(tasks)-maxVisible {
		start = len(tasks) - maxVisible
	}
	if start < 0 {
		start = 0
	}
	end := start + maxVisible
	if end > len(tasks) {
		end = len(tasks)
	}

	for i := start; i < end; i++ {
		t := tasks[i]
		id := t.DisplayID
		if len(id) > idWidth {
			id = id[:idWidth-1] + "…"
		}
		title := t.Title
		if len(title) > titleWidth {
			title = title[:titleWidth-1] + "…"
		}

		if i == m.taskScroll {
			row := fmt.Sprintf("%-*s %-*s %*s %*s",
				idWidth, id, titleWidth, title,
				workingWidth, fmtDuration(t.Working),
				waitingWidth, fmtDuration(t.Waiting))
			lines = append(lines, selectedStyle.Render("▸ ")+selectedStyle.Render(row))
		} else {
			row := fmt.Sprintf("  %s %-*s %*s %*s",
				idStyle.Render(fmt.Sprintf("%-*s", idWidth, id)),
				titleWidth, title,
				workingWidth, fmtDuration(t.Working),
				waitingWidth, fmtDuration(t.Waiting))
			lines = append(lines, row)
		}
	}

	if len(tasks) > maxVisible {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("  ... %d tasks total (j/k to scroll)", len(tasks))))
	}

	return strings.Join(lines, "\n")
}

func (m Model) renderWorkspaces(width int) string {
	wss := m.report.ByWorkspace
	if len(wss) == 0 {
		return ""
	}

	sectionStyle := lipgloss.NewStyle().Foreground(theme.TextSecondary).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)

	var lines []string
	lines = append(lines, sectionStyle.Render("WORKSPACES"))

	for _, ws := range wss {
		dot := "●"
		if ws.WorkspaceColor != "" {
			dot = lipgloss.NewStyle().Foreground(lipgloss.Color(ws.WorkspaceColor)).Render("●")
		} else {
			dot = dimStyle.Render("●")
		}

		line := fmt.Sprintf("%s %-15s  Working: %-8s  Waiting: %-8s  Tasks: %d",
			dot, ws.WorkspaceName,
			fmtDuration(ws.Working), fmtDuration(ws.Waiting), ws.TaskCount)
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func (m Model) renderHints(width int) string {
	dimStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
	keyStyle := lipgloss.NewStyle().Foreground(theme.AccentPurple)

	return dimStyle.Render(
		keyStyle.Render("tab") + " period  " +
			keyStyle.Render("h/l") + " navigate  " +
			keyStyle.Render("j/k") + " scroll  " +
			keyStyle.Render("R") + " copy markdown  " +
			keyStyle.Render("esc") + " back")
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
