package overlay

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// panelStyle is the shared style for overlay panels.
var panelStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(theme.ColReview).
	Padding(1, 2)

// RenderPanel renders content inside a centered overlay panel.
// If width/height are zero, it returns just the bordered box.
func RenderPanel(content string, width, height int) string {
	box := panelStyle.Render(content)
	if width > 0 && height > 0 {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
	}
	return box
}
