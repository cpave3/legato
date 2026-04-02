package overlay

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// EphemeralSpawnSubmitMsg is sent when the user confirms the ephemeral spawn.
type EphemeralSpawnSubmitMsg struct {
	Title string
}

// EphemeralSpawnCancelledMsg is sent when the user cancels.
type EphemeralSpawnCancelledMsg struct{}

// EphemeralSpawnOverlay lets the user name a new ephemeral agent session.
type EphemeralSpawnOverlay struct {
	title  string
	width  int
	height int
}

// NewEphemeralSpawn creates an ephemeral spawn overlay.
func NewEphemeralSpawn() EphemeralSpawnOverlay {
	return EphemeralSpawnOverlay{}
}

func (m EphemeralSpawnOverlay) Init() tea.Cmd {
	return nil
}

func (m EphemeralSpawnOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return EphemeralSpawnCancelledMsg{} }
		case "enter":
			title := m.title
			if title == "" {
				title = "Ephemeral session"
			}
			return m, func() tea.Msg { return EphemeralSpawnSubmitMsg{Title: title} }
		case "backspace":
			if len(m.title) > 0 {
				m.title = m.title[:len(m.title)-1]
			}
			return m, nil
		default:
			if msg.Type == tea.KeySpace {
				m.title += " "
			} else if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
				m.title += string(msg.Runes)
			}
		}
	}
	return m, nil
}

func (m EphemeralSpawnOverlay) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.TextPrimary)

	labelStyle := lipgloss.NewStyle().
		Foreground(theme.TextTertiary)

	inputStyle := lipgloss.NewStyle().
		Foreground(theme.AccentPurple).
		Bold(true)

	hintStyle := lipgloss.NewStyle().
		Foreground(theme.TextTertiary)

	heading := titleStyle.Render("Spawn Ephemeral Agent")
	titleLine := labelStyle.Render("Title: ") + inputStyle.Render(m.title+"█")
	hints := hintStyle.Render("enter spawn · esc cancel")

	content := lipgloss.JoinVertical(lipgloss.Left, heading, "", titleLine, "", hints)
	return RenderPanel(content, m.width, m.height)
}
