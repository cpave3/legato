package board

import (
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/tui/theme"
)

// RenderColumn renders a column with header and cards.
// width is the column width in characters.
// selectedIdx is the selected card index (-1 for no selection).
func RenderColumn(name string, cards []CardData, width int, active bool, selectedIdx int) string {
	// Header
	headerStyle := theme.ColumnHeader
	if active {
		headerStyle = theme.ColumnHeaderActive
	}
	header := headerStyle.Width(width).Render(fmt.Sprintf("%s  %d", strings.ToUpper(name), len(cards)))

	// Cards
	var cardLines []string
	for i, card := range cards {
		selected := active && i == selectedIdx
		rendered := RenderCard(card, width, selected, name)
		cardLines = append(cardLines, rendered)
	}

	body := strings.Join(cardLines, "\n")

	return header + "\n" + body
}
