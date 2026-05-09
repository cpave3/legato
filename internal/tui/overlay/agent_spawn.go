package overlay

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// AgentSpawnSubmitMsg is sent when the user confirms the spawn.
type AgentSpawnSubmitMsg struct {
	TaskID     string // empty for ephemeral agents
	Title      string // used when TaskID is empty
	AgentKind  string
	WorkingDir string
}

// AgentSpawnCancelledMsg is sent when the user cancels.
type AgentSpawnCancelledMsg struct{}

// spawnField tracks which input is active in the overlay.
type spawnField int

const (
	spawnFocusTitle spawnField = iota
	spawnFocusAgent
	spawnFocusCwd
	spawnFieldCount = 3
)

// AgentSpawnOverlay lets the user configure and spawn an agent.
type AgentSpawnOverlay struct {
	taskID       string   // empty for ephemeral agents
	title        string
	agentIndex   int      // index into agentOptions
	agentOptions []string // display labels
	agentValues  []string // corresponding agent kind values ("", "shell", adapter names)
	cwd          string
	focus        spawnField
	width        int
	height       int
}

// NewAgentSpawn creates an agent spawn overlay.
// adapters is the list of registered adapter names (excluding the default).
// defaultAdapter is the name shown for the default option.
// defaultCWD pre-fills the working directory field.
// taskID is empty for ephemeral agents; non-empty spawns an agent for an existing task.
// title pre-fills the title field; for ephemeral agents this is editable, for task-bound it is shown as context.
func NewAgentSpawn(adapters []string, defaultAdapter, defaultCWD, taskID, title string) AgentSpawnOverlay {
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

	focus := spawnFocusTitle
	if taskID != "" {
		// Task-bound: focus the agent selector since title is just context
		focus = spawnFocusAgent
	}

	return AgentSpawnOverlay{
		taskID:       taskID,
		agentIndex:   0,
		agentOptions: options,
		agentValues:  values,
		cwd:          defaultCWD,
		title:        title,
		focus:        focus,
	}
}

func (m AgentSpawnOverlay) Init() tea.Cmd { return nil }

func (m AgentSpawnOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return AgentSpawnCancelledMsg{} }
		case "enter":
			title := m.title
			if m.taskID == "" && title == "" {
				title = "Ephemeral session"
			}
			return m, func() tea.Msg {
				return AgentSpawnSubmitMsg{
					TaskID:     m.taskID,
					Title:      title,
					AgentKind:  m.agentValues[m.agentIndex],
					WorkingDir: m.cwd,
				}
			}
		case "tab":
			for {
				m.focus = (m.focus + 1) % spawnFieldCount
				if m.taskID == "" || m.focus != spawnFocusTitle {
					break
				}
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
				if m.taskID == "" && len(m.title) > 0 {
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
					if m.taskID == "" {
						m.title += " "
					}
				case spawnFocusCwd:
					m.cwd += " "
				}
				return m, nil
			}
			if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
				// h/l cycle agent selector when focused
				if m.focus == spawnFocusAgent && len(msg.Runes) == 1 {
					switch msg.Runes[0] {
					case 'h':
						if m.agentIndex > 0 {
							m.agentIndex--
						}
						return m, nil
					case 'l':
						if m.agentIndex < len(m.agentOptions)-1 {
							m.agentIndex++
						}
						return m, nil
					}
				}
				switch m.focus {
				case spawnFocusTitle:
					if m.taskID == "" {
						m.title += string(msg.Runes)
					}
				case spawnFocusCwd:
					m.cwd += string(msg.Runes)
				}
			}
			return m, nil
		}
	}
	return m, nil
}

func (m AgentSpawnOverlay) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.TextPrimary)
	labelStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
	inputStyle := lipgloss.NewStyle().Foreground(theme.AccentPurple).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
	cursor := inputStyle.Render("█")

	var heading string
	if m.taskID == "" {
		heading = titleStyle.Render("Spawn Ephemeral Agent")
	} else {
		heading = titleStyle.Render("Spawn Agent")
	}

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

	hints := hintStyle.Render("tab field · h/l or ←→ change agent · enter spawn · esc cancel")

	lines := []string{heading, "", titleLine, agentLine, cwdLine, "", hints}
	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return RenderPanel(content, m.width, m.height)
}
