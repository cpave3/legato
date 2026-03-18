package board

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// CardData holds the data needed to render a card.
type CardData struct {
	Key       string
	Summary   string
	Priority  string
	IssueType string
}

// RenderCard renders a single card with the given parameters.
func RenderCard(card CardData, width int, selected bool, column string) string {
	isDone := column == "Done"

	// Content width = total width minus border (2) minus padding (2)
	contentWidth := width - 4
	if contentWidth < 4 {
		contentWidth = 4
	}

	// Truncate summary
	summary := truncateSummary(card.Summary, contentWidth)

	// Apply done-column muted styling
	keyLine := card.Key
	typeLine := card.IssueType
	if isDone {
		keyLine = theme.DoneMuted.Render(card.Key)
		summary = theme.DoneMuted.Render(summary)
		typeLine = theme.DoneMuted.Render(card.IssueType)
	}

	content := keyLine + "\n" + summary + "\n" + typeLine

	// Pick style
	var style lipgloss.Style
	if selected {
		style = theme.CardSelected.
			Width(contentWidth)
	} else {
		borderColor := theme.PriorityBorderColor(card.Priority)
		if isDone {
			borderColor = theme.ColDone
		}
		style = theme.CardBase.
			Width(contentWidth).
			BorderLeft(true).
			BorderForeground(borderColor)
	}

	return style.Render(content)
}

func truncateSummary(s string, maxWidth int) string {
	if len(s) <= maxWidth {
		return s
	}
	if maxWidth <= 3 {
		return s[:maxWidth]
	}
	return s[:maxWidth-3] + "..."
}
