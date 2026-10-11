package board

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/cpave3/legato/internal/tui/theme"
)

// RailWidth is the width of a collapsed column.
const RailWidth = 5

// columnNameColor is the color of a column's name. The column where work is
// in progress carries the accent; the others are plain.
func columnNameColor(name string) lipgloss.Color {
	if theme.ColumnBorderColor(name) == theme.Accent {
		return theme.Accent
	}
	return theme.TextPrimary
}

// RenderColumn renders a column with header and cards.
// width is the column width in characters.
// selectedIdx is the selected card index (-1 for no selection).
// totalCount is the total cards in the column (for the header, which may differ from len(cards) when windowed).
func RenderColumn(name string, cards []CardData, width int, active bool, selectedIdx int, totalCount int, icons theme.Icons) string {
	// A column keeps one column clear at each side, where the frame of its
	// selected card is drawn.
	inner := width - 2
	if inner < 4 {
		inner = 4
	}

	title := lipgloss.NewStyle().Foreground(columnNameColor(name)).Bold(true).Render(strings.ToUpper(name))
	count := colored(theme.TextSecondary, fmt.Sprintf("%d", totalCount))
	header := " " + theme.Spread(title, count, inner) + " "
	rule := " " + colored(theme.ColumnBorderColor(name), strings.Repeat("━", inner)) + " "

	lines := []string{header, rule}
	if len(cards) == 0 {
		note := colored(theme.TextFaint, "no tasks")
		if active {
			note = colored(theme.TextSecondary, "no tasks")
		}
		lines = append(lines, "", "  "+note)
	}
	for i, card := range cards {
		block := strings.Split(RenderCard(card, width, active && i == selectedIdx, name, icons), "\n")
		if i == 0 {
			lines = append(lines, block...)
			continue
		}
		// The frame of one card is drawn in the line that separates it from
		// its neighbour, so the two cards share that line.
		last := len(lines) - 1
		if strings.TrimSpace(ansi.Strip(lines[last])) == "" {
			lines[last] = block[0]
		}
		lines = append(lines, block[1:]...)
	}

	return theme.Fill(strings.Join(lines, "\n"), width, theme.Background)
}

// RenderRail renders a column that is collapsed because it has no cards: its
// rule, its count, and its name written down its length.
func RenderRail(name string, count int, active bool, height int) string {
	if height < 1 {
		return ""
	}
	textColor := theme.TextFaint
	if active {
		textColor = theme.TextPrimary
	}
	text := lipgloss.NewStyle().Foreground(textColor).Bold(active)

	lines := []string{
		" " + colored(theme.ColumnBorderColor(name), "━━━") + " ",
		lipgloss.PlaceHorizontal(RailWidth, lipgloss.Center, text.Render(fmt.Sprintf("%d", count))),
		"",
	}
	for _, letter := range strings.ToUpper(name) {
		lines = append(lines, "  "+text.Render(string(letter)))
	}
	return theme.Canvas(strings.Join(lines, "\n"), RailWidth, height, theme.Background)
}
