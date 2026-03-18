package overlay

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// HelpClosedMsg is sent when the help overlay is dismissed.
type HelpClosedMsg struct{}

type keybinding struct {
	key  string
	desc string
}

var (
	navigationBindings = []keybinding{
		{"h/l", "Move between columns"},
		{"j/k", "Move up/down within column"},
		{"g/G", "Jump to first/last card"},
		{"1-5", "Jump to column by number"},
	}
	actionBindings = []keybinding{
		{"enter", "Open ticket detail"},
		{"m", "Move ticket to column"},
		{"y", "Copy description"},
		{"Y", "Copy full context"},
		{"r", "Force sync"},
		{"/", "Search/filter tickets"},
		{"esc", "Back / close overlay"},
	}
	generalBindings = []keybinding{
		{"?", "Toggle this help screen"},
		{"q", "Quit"},
	}
)

// HelpOverlay displays a keyboard reference screen.
type HelpOverlay struct {
	width  int
	height int
}

// NewHelp creates a new help overlay.
func NewHelp(width, height int) HelpOverlay {
	return HelpOverlay{width: width, height: height}
}

// Init returns no command.
func (m HelpOverlay) Init() tea.Cmd {
	return nil
}

// Update handles messages — only esc and ? dismiss, all else ignored.
func (m HelpOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "?":
			return m, func() tea.Msg { return HelpClosedMsg{} }
		}
	}
	return m, nil
}

// View renders the help overlay.
func (m HelpOverlay) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.TextPrimary)

	sectionStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.AccentPurple).
		MarginTop(1)

	keyStyle := lipgloss.NewStyle().
		Foreground(theme.AccentPurple).
		Bold(true).
		Width(10)

	descStyle := lipgloss.NewStyle().
		Foreground(theme.TextSecondary)

	var lines []string
	lines = append(lines, titleStyle.Render("Legato -- Keyboard Reference"))

	renderSection := func(name string, bindings []keybinding) {
		lines = append(lines, sectionStyle.Render(name))
		for _, b := range bindings {
			lines = append(lines, fmt.Sprintf("  %s%s", keyStyle.Render(b.key), descStyle.Render(b.desc)))
		}
	}

	renderSection("Navigation", navigationBindings)
	renderSection("Actions", actionBindings)
	renderSection("General", generalBindings)

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return RenderPanel(content, m.width, m.height)
}
