package board

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// RenderColumn renders a column with header and cards.
// width is the column width in characters.
// selectedIdx is the selected card index (-1 for no selection).
func RenderColumn(name string, cards []CardData, width int, active bool, selectedIdx int, icons theme.Icons) string {
	colColor := theme.ColumnBorderColor(name)

	// Colored top bar
	topBar := lipgloss.NewStyle().
		Background(colColor).
		Foreground(lipgloss.Color("#1E1E2E")).
		Bold(true).
		Width(width).
		Align(lipgloss.Center).
		Render(fmt.Sprintf(" %s  %d ", strings.ToUpper(name), len(cards)))

	// Thin separator under the header
	var separatorColor lipgloss.Color
	if active {
		separatorColor = colColor
	} else {
		separatorColor = theme.TextTertiary
	}
	separator := lipgloss.NewStyle().
		Foreground(separatorColor).
		Width(width).
		Render(strings.Repeat("─", width))

	// Cards with spacing
	var cardLines []string
	for i, card := range cards {
		selected := active && i == selectedIdx
		rendered := RenderCard(card, width-1, selected, name, icons)
		cardLines = append(cardLines, rendered)
	}

	body := strings.Join(cardLines, "\n")

	return topBar + "\n" + separator + "\n" + body
}
