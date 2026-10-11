package theme

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// TitleBarHeight is the height of a title bar: its line and the rule under it.
const TitleBarHeight = 2

// titleBarMargin is the space at each end of a title bar.
const titleBarMargin = 2

var factSeparator = lipgloss.NewStyle().Foreground(TextFaint).Render(" · ")

// Brand is how the app names itself where a view begins.
func Brand() string {
	return lipgloss.NewStyle().Foreground(Accent).Bold(true).Render("♩ legato")
}

// TitleBar renders the line a view opens with and the rule under it: the
// brand, the view's name, a few facts about what the view shows, and at the
// far end what the view is set to. The trailing pieces are given up from the
// last when the line has no room for them.
func TitleBar(width int, view string, facts []string, trailing []string) string {
	inner := width - 2*titleBarMargin
	if inner < 1 {
		return Canvas("", max(width, 1), TitleBarHeight, Background)
	}

	left := Brand() + "  " + lipgloss.NewStyle().Foreground(TextSecondary).Render(view)
	if len(facts) > 0 {
		left += " " + lipgloss.NewStyle().Foreground(TextFaint).Render("│") + " " + strings.Join(facts, factSeparator)
	}

	right := ""
	for count := len(trailing); count > 0; count-- {
		candidate := strings.Join(trailing[:count], factSeparator)
		if lipgloss.Width(left)+2+lipgloss.Width(candidate) <= inner {
			right = candidate
			break
		}
	}

	line := strings.Repeat(" ", titleBarMargin) + Spread(left, right, inner)
	rule := lipgloss.NewStyle().Foreground(Border).Render(strings.Repeat("─", width))
	return Fill(line+"\n"+rule, width, Background)
}

// Chip renders a short label on a raised ground: a setting, or a filter.
func Chip(label string, color lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(color).Background(Bar).Padding(0, 1).Render(label)
}
