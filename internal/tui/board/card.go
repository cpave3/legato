package board

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// CardData holds the data needed to render a card.
type CardData struct {
	Key             string
	Title           string
	Priority        string
	IssueType       string
	Provider        string        // "jira", "github", or "" for local
	Warning         bool
	AgentActive     bool          // true if any agent session is running for this task
	AgentState      string        // "working", "waiting", or "" (idle/no agent)
	WorkingDuration time.Duration // cumulative working time
	WaitingDuration time.Duration // cumulative waiting time
	WorkspaceName   string        // workspace name (populated in "All" view)
	WorkspaceColor  string        // workspace color hex (populated in "All" view)
}

// formatDuration formats a duration as a human-readable string.
// Returns "", "<1m", "Xm", or "Xh Ym".
func formatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	totalMinutes := int(d.Minutes())
	if totalMinutes < 1 {
		return "<1m"
	}
	hours := totalMinutes / 60
	minutes := totalMinutes % 60
	if hours == 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	if minutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, minutes)
}

// renderDurationLine builds a duration summary line showing both working and waiting times.
// Format: "⟳ 45m · ◆ 10m" using the agent state icons.
// Returns empty string if no durations to show.
func renderDurationLine(card CardData, icons theme.Icons, selected bool) string {
	if card.WorkingDuration <= 0 && card.WaitingDuration <= 0 {
		return ""
	}

	dimColor := theme.TextTertiary
	workingColor := theme.SyncOK
	waitingColor := theme.ColReady
	bg := lipgloss.Color("#252540") // CardBase background
	if selected {
		dimColor = lipgloss.Color("#585878")
		workingColor = lipgloss.Color("#287828")
		waitingColor = lipgloss.Color("#285878")
		bg = lipgloss.Color("#EEEDFE") // CardSelected background
	}

	var parts []string
	if d := formatDuration(card.WorkingDuration); d != "" {
		wStyle := lipgloss.NewStyle().Foreground(workingColor).Background(bg)
		dStyle := lipgloss.NewStyle().Foreground(dimColor).Background(bg)
		parts = append(parts, wStyle.Render(icons.AgentWorking+" ")+dStyle.Render(d))
	}
	if d := formatDuration(card.WaitingDuration); d != "" {
		wStyle := lipgloss.NewStyle().Foreground(waitingColor).Background(bg)
		dStyle := lipgloss.NewStyle().Foreground(dimColor).Background(bg)
		parts = append(parts, wStyle.Render(icons.AgentWaiting+" ")+dStyle.Render(d))
	}

	dotStyle := lipgloss.NewStyle().Foreground(dimColor).Background(bg)
	return strings.Join(parts, dotStyle.Render(" · "))
}

// RenderCard renders a single card with the given parameters.
func RenderCard(card CardData, width int, selected bool, column string, icons theme.Icons) string {
	isDone := column == "Done"

	// Content width = total width minus border (2) minus padding (2)
	contentWidth := width - 4
	if contentWidth < 4 {
		contentWidth = 4
	}

	// Truncate title
	title := truncateTitle(card.Title, contentWidth)

	// Provider icon
	providerIcon := ""
	{
		iconStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
		switch card.Provider {
		case "jira":
			providerIcon = iconStyle.Foreground(theme.ColReady).Render(icons.Jira) + " "
		case "github":
			providerIcon = iconStyle.Foreground(theme.TextPrimary).Render(icons.GitHub) + " "
		default:
			providerIcon = iconStyle.Render(icons.Local) + " "
		}
	}

	// Warning indicator for failed transitions
	warningPrefix := ""
	if card.Warning {
		warningStyle := lipgloss.NewStyle().Foreground(theme.SyncError).Bold(true)
		warningPrefix = warningStyle.Render(icons.Warning) + " "
	}

	// Agent activity indicator (back on key line)
	agentPrefix := ""
	switch card.AgentState {
	case "working":
		agentStyle := lipgloss.NewStyle().Foreground(theme.SyncOK).Bold(true)
		agentPrefix = agentStyle.Render(icons.AgentWorking+" RUNNING") + " "
	case "waiting":
		agentStyle := lipgloss.NewStyle().Foreground(theme.ColReady).Bold(true)
		agentPrefix = agentStyle.Render(icons.AgentWaiting+" WAITING") + " "
	default:
		if card.AgentActive {
			agentStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
			agentPrefix = agentStyle.Render(icons.Terminal+" IDLE") + " "
		}
	}

	// Key line: styled dimmer than title for hierarchy
	keyStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
	titleStyle := lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true)

	// Build metadata line: priority + dot + issue type + workspace tag
	dotStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
	var metaParts []string
	if card.Priority != "" {
		metaParts = append(metaParts, renderPriorityBadge(card.Priority))
	}
	if card.IssueType != "" {
		metaParts = append(metaParts, theme.TypeBadge.Render(card.IssueType))
	}
	if card.WorkspaceName != "" {
		wsColor := lipgloss.Color(theme.TextTertiary)
		if card.WorkspaceColor != "" {
			wsColor = lipgloss.Color(card.WorkspaceColor)
		}
		wsStyle := lipgloss.NewStyle().Foreground(wsColor)
		metaParts = append(metaParts, wsStyle.Render(card.WorkspaceName))
	}
	metaLine := ""
	if len(metaParts) > 0 {
		metaLine = lipgloss.JoinHorizontal(lipgloss.Left, joinWithDot(metaParts, dotStyle)...)
	}

	// Duration line (bottom row, always shown when durations exist)
	durationLine := renderDurationLine(card, icons, selected)

	// Apply done-column muted styling
	if isDone {
		keyLine := theme.DoneMuted.Render(card.Key)
		title = theme.DoneMuted.Render(title)
		metaLine = theme.DoneMuted.Render(card.IssueType)
		content := keyLine + "\n" + title + "\n" + metaLine
		if durationLine != "" {
			content += "\n" + durationLine
		}

		style := theme.CardBase.
			Width(contentWidth).
			BorderLeft(true).
			BorderForeground(theme.ColDone)
		return style.Render(content)
	}

	// Selected cards need dark-on-light colors
	if selected {
		selectedBg := lipgloss.Color("#EEEDFE")
		darkText := lipgloss.Color("#1E1E2E")
		dimText := lipgloss.Color("#585878")
		s := func(fg lipgloss.Color) lipgloss.Style {
			return lipgloss.NewStyle().Foreground(fg).Background(selectedBg)
		}

		// Provider icon with selected background
		sProviderIcon := s(dimText).Render(icons.Local + " ")
		switch card.Provider {
		case "jira":
			sProviderIcon = s(lipgloss.Color("#285878")).Render(icons.Jira + " ")
		case "github":
			sProviderIcon = s(darkText).Render(icons.GitHub + " ")
		}

		// Agent prefix
		sAgentPrefix := ""
		switch card.AgentState {
		case "working":
			sAgentPrefix = s(lipgloss.Color("#287828")).Bold(true).Render(icons.AgentWorking+" RUNNING ")
		case "waiting":
			sAgentPrefix = s(lipgloss.Color("#285878")).Bold(true).Render(icons.AgentWaiting+" WAITING ")
		default:
			if card.AgentActive {
				sAgentPrefix = s(dimText).Render(icons.Terminal+" IDLE ")
			}
		}

		// Warning
		sWarningPrefix := ""
		if card.Warning {
			sWarningPrefix = s(theme.SyncError).Bold(true).Render(icons.Warning + " ")
		}

		var sMetaParts []string
		if card.Priority != "" {
			sMetaParts = append(sMetaParts, s(dimText).Render(card.Priority))
		}
		if card.IssueType != "" {
			sMetaParts = append(sMetaParts, s(dimText).Render(card.IssueType))
		}
		if card.WorkspaceName != "" {
			wsColor := dimText
			if card.WorkspaceColor != "" {
				wsColor = lipgloss.Color(card.WorkspaceColor)
			}
			sMetaParts = append(sMetaParts, s(wsColor).Render(card.WorkspaceName))
		}
		sMetaLine := ""
		if len(sMetaParts) > 0 {
			sMetaLine = strings.Join(sMetaParts, s(dimText).Render(" · "))
		}

		content := sProviderIcon + sAgentPrefix + sWarningPrefix + s(dimText).Render(card.Key) + "\n" +
			s(darkText).Bold(true).Render(title) + "\n" + sMetaLine
		if durationLine != "" {
			content += "\n" + durationLine
		}

		style := theme.CardSelected.Width(contentWidth)
		return style.Render(content)
	}

	keyLine := providerIcon + agentPrefix + warningPrefix + keyStyle.Render(card.Key)
	titleLine := titleStyle.Render(title)
	content := keyLine + "\n" + titleLine + "\n" + metaLine
	if durationLine != "" {
		content += "\n" + durationLine
	}

	var style lipgloss.Style
	{
		borderColor := theme.PriorityBorderColor(card.Priority)
		style = theme.CardBase.
			Width(contentWidth).
			BorderLeft(true).
			BorderForeground(borderColor)
	}

	return style.Render(content)
}

func joinWithDot(parts []string, dotStyle lipgloss.Style) []string {
	if len(parts) == 0 {
		return nil
	}
	var result []string
	for i, p := range parts {
		if i > 0 {
			result = append(result, dotStyle.Render(" · "))
		}
		result = append(result, p)
	}
	return result
}

func renderPriorityBadge(priority string) string {
	switch priority {
	case "High", "Highest":
		return theme.PriorityBadgeHigh.Render(priority)
	case "Medium":
		return theme.PriorityBadgeMed.Render(priority)
	case "Low", "Lowest":
		return theme.PriorityBadgeLow.Render(priority)
	default:
		return ""
	}
}

func truncateTitle(s string, maxWidth int) string {
	if len(s) <= maxWidth {
		return s
	}
	if maxWidth <= 3 {
		return s[:maxWidth]
	}
	return s[:maxWidth-3] + "..."
}
