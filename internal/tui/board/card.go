package board

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// CardData holds the data needed to render a card.
type CardData struct {
	Key         string
	Title       string
	Priority    string
	IssueType   string
	Provider    string // "jira", "github", or "" for local
	Warning     bool
	AgentActive bool   // true if any agent session is running for this task
	AgentState  string // "working", "waiting", or "" (idle/no agent)
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

	// Agent activity indicator
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

	// Build metadata line: priority + dot + issue type
	dotStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
	var metaParts []string
	if card.Priority != "" {
		metaParts = append(metaParts, renderPriorityBadge(card.Priority))
	}
	if card.IssueType != "" {
		metaParts = append(metaParts, theme.TypeBadge.Render(card.IssueType))
	}
	metaLine := ""
	if len(metaParts) > 0 {
		metaLine = lipgloss.JoinHorizontal(lipgloss.Left, joinWithDot(metaParts, dotStyle)...)
	}

	// Apply done-column muted styling
	if isDone {
		keyLine := theme.DoneMuted.Render(card.Key)
		title = theme.DoneMuted.Render(title)
		metaLine = theme.DoneMuted.Render(card.IssueType)
		content := keyLine + "\n" + title + "\n" + metaLine

		style := theme.CardBase.
			Width(contentWidth).
			BorderLeft(true).
			BorderForeground(theme.ColDone)
		return style.Render(content)
	}

	// Selected cards need dark-on-light colors
	if selected {
		darkText := lipgloss.Color("#1E1E2E")
		dimText := lipgloss.Color("#585878")
		sKeyStyle := lipgloss.NewStyle().Foreground(dimText)
		sTitleStyle := lipgloss.NewStyle().Foreground(darkText).Bold(true)
		sTypeStyle := lipgloss.NewStyle().Foreground(dimText)

		sMetaLine := ""
		if card.Priority != "" {
			sMetaLine = sTypeStyle.Render(card.Priority)
			if card.IssueType != "" {
				sMetaLine += " " + sTypeStyle.Render(card.IssueType)
			}
		} else if card.IssueType != "" {
			sMetaLine = sTypeStyle.Render(card.IssueType)
		}

		content := providerIcon + agentPrefix + warningPrefix + sKeyStyle.Render(card.Key) + "\n" +
			sTitleStyle.Render(title) + "\n" + sMetaLine

		style := theme.CardSelected.Width(contentWidth)
		return style.Render(content)
	}

	keyLine := providerIcon + agentPrefix + warningPrefix + keyStyle.Render(card.Key)
	titleLine := titleStyle.Render(title)
	content := keyLine + "\n" + titleLine + "\n" + metaLine

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
