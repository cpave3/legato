package overlay

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// EphemeralSpawnSubmitMsg is sent when the user confirms the ephemeral spawn.
type EphemeralSpawnSubmitMsg struct {
	Title      string
	AgentKind  string
	WorkingDir string
}

// EphemeralSpawnCancelledMsg is sent when the user cancels.
type EphemeralSpawnCancelledMsg struct{}

// spawnField tracks which input is active in the overlay.
type spawnField int

const (
	spawnFocusTitle spawnField = iota
	spawnFocusAgent
	spawnFocusCwd
)

// EphemeralSpawnOverlay lets the user configure and spawn an ephemeral agent.
type EphemeralSpawnOverlay struct {
	title          string
	agentIndex     int      // index into agentOptions
	agentOptions   []string // display labels
	agentValues    []string // corresponding agent kind values ("", "shell", adapter names)
	cwd            string
	focus          spawnField
	width          int
	height         int
}

// NewEphemeralSpawn creates an ephemeral spawn overlay.
// adapters is the list of registered adapter names (excluding the default).
// defaultAdapter is the name shown for the default option.
// defaultCWD pre-fills the working directory field.
func NewEphemeralSpawn(adapters []string, defaultAdapter, defaultCWD string) EphemeralSpawnOverlay {
	options := []string{}
	values := []string{}

	if defaultAdapter != "" {
		options = append(options, fmt.Sprintf("default (%s)", defaultAdapter))
	} else {
		options = append(options, "default")
	}
	values = append(values, "")

	options = append(options, "shell")
	values = append(values, "shell")

	for _, a := range adapters {
		options = append(options, a)
		values = append(values, a)
	}

	return EphemeralSpawnOverlay{
		agentIndex:   0,
		agentOptions: options,
		agentValues:  values,
		cwd:          defaultCWD,
		focus:        spawnFocusTitle,
	}
}

func (m EphemeralSpawnOverlay) Init() tea.Cmd { return nil }

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
			return m, func() tea.Msg {
				return EphemeralSpawnSubmitMsg{
					Title:      title,
					AgentKind:  m.agentValues[m.agentIndex],
					WorkingDir: m.cwd,
				}
			}
		case "tab":
			m.focus = (m.focus + 1) % 3
			return m, nil
		case "shift+tab":
			m.focus = (m.focus + 2) % 3
			return m, nil
		case "up":
			if m.focus > 0 {
				m.focus--
			}
			return m, nil
		case "down":
			if m.focus < 2 {
				m.focus++
			}
			return m, nil
		case "left":
			if m.focus == spawnFocusAgent && m.agentIndex > 0 {
				m.agentIndex--
			}
			return m, nil
		case "right":
			if m.focus == spawnFocusAgent && m.agentIndex < len(m.agentOptions)-1 {
				m.agentIndex++
			}
			return m, nil
		case "backspace":
			switch m.focus {
			case spawnFocusTitle:
				if len(m.title) > 0 {
					m.title = m.title[:len(m.title)-1]
				}
			case spawnFocusCwd:
				if len(m.cwd) > 0 {
					m.cwd = m.cwd[:len(m.cwd)-1]
				}
			}
			return m, nil
		default:
			if msg.Type == tea.KeySpace {
				switch m.focus {
				case spawnFocusTitle:
					m.title += " "
				case spawnFocusCwd:
					m.cwd += " "
				}
				return m, nil
			}
			if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
				switch m.focus {
				case spawnFocusTitle:
					m.title += string(msg.Runes)
				case spawnFocusCwd:
					m.cwd += string(msg.Runes)
				}
			}
			return m, nil
		}
	}
	return m, nil
}

func (m EphemeralSpawnOverlay) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.TextPrimary)
	labelStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
	inputStyle := lipgloss.NewStyle().Foreground(theme.AccentPurple).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
	cursor := inputStyle.Render("█")

	heading := titleStyle.Render("Spawn Ephemeral Agent")

	renderLine := func(label, value string, focused bool) string {
		prefix := "  "
		if focused {
			prefix = "> "
		}
		val := value
		if focused {
			val += cursor
		}
		return prefix + labelStyle.Render(label+": ") + inputStyle.Render(val)
	}

	titleLine := renderLine("Title", m.title, m.focus == spawnFocusTitle)

	agentVal := m.agentOptions[m.agentIndex]
	if m.focus == spawnFocusAgent {
		agentVal = "◄ " + agentVal + " ►"
	}
	agentLine := renderLine("Agent", agentVal, m.focus == spawnFocusAgent)

	cwdVal := m.cwd
	if cwdVal == "" {
		cwdVal = " "
	}
	cwdLine := renderLine("CWD", cwdVal, m.focus == spawnFocusCwd)

	hints := hintStyle.Render("↑↓ move · ←→ change agent · enter spawn · esc cancel")

	lines := []string{heading, "", titleLine, agentLine, cwdLine, "", hints}
	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return RenderPanel(content, m.width, m.height)
}
