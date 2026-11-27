package overlay

import (
	"fmt"
	"github.com/cpave3/legato/config"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// MacroSelectedMsg is sent when the user picks a macro.
type MacroSelectedMsg struct {
	Macro config.Macro
}

// MacroCancelledMsg is sent when the user dismisses the picker.
type MacroCancelledMsg struct{}

// MacroPickerOverlay lets the user select a macro: the favourites, and
// the rest after a Show all row.
type MacroPickerOverlay struct {
	macros []config.Macro
	// hidden is the macros Show all adds; none once it has.
	hidden []config.Macro
	cursor int
	width  int
	height int
}

// NewMacroPicker creates a macro picker overlay.
func NewMacroPicker(macrosList []config.Macro) MacroPickerOverlay {
	favorites, rest := config.SplitFavoriteMacros(macrosList)
	return MacroPickerOverlay{macros: favorites, hidden: rest}
}

// rows is how many rows the cursor moves over: the macros, and Show all.
func (m MacroPickerOverlay) rows() int {
	if len(m.hidden) > 0 {
		return len(m.macros) + 1
	}
	return len(m.macros)
}

// Init returns no command.
func (m MacroPickerOverlay) Init() tea.Cmd {
	return nil
}

// Update handles messages.
func (m MacroPickerOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q":
			return m, func() tea.Msg { return MacroCancelledMsg{} }
		case "j", "down":
			if m.cursor < m.rows()-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "enter", "tab":
			if m.cursor == len(m.macros) && len(m.hidden) > 0 {
				m.macros = append(append([]config.Macro(nil), m.macros...), m.hidden...)
				m.hidden = nil
				return m, nil
			}
			if len(m.macros) > 0 && m.cursor < len(m.macros) {
				macro := m.macros[m.cursor]
				return m, func() tea.Msg { return MacroSelectedMsg{Macro: macro} }
			}
		}
	}
	return m, nil
}

// View renders the overlay.
func (m MacroPickerOverlay) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.TextPrimary).
		Padding(0, 1)

	title := titleStyle.Render("Macros")

	var lines []string
	lines = append(lines, title, "")

	if len(m.macros) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.TextTertiary).Render("(no macros configured)"))
	} else {
		for i, macro := range m.macros {
			prefix := "  "
			style := lipgloss.NewStyle().Foreground(theme.TextSecondary)
			if i == m.cursor {
				prefix = "> "
				style = lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true)
			}
			name := style.Render(prefix + macro.Name)
			escaped := strings.ReplaceAll(macro.Keys, "\n", "\\n")
			keys := lipgloss.NewStyle().Foreground(theme.TextTertiary).Render("  " + escaped)
			lines = append(lines, name+"\n"+keys)
		}
		if len(m.hidden) > 0 {
			label := fmt.Sprintf("Show all (%d more)", len(m.hidden))
			style := lipgloss.NewStyle().Foreground(theme.TextTertiary)
			prefix := "  "
			if m.cursor == len(m.macros) {
				prefix = "> "
				style = lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true)
			}
			lines = append(lines, style.Render(prefix+label))
		}
	}

	lines = append(lines, "", lipgloss.NewStyle().Foreground(theme.TextTertiary).Padding(0, 1).Render("j/k navigate · ↵ send · esc cancel"))

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return RenderPanel(content, m.width, m.height)
}
