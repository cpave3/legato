package overlay

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// SwarmStartMsg is emitted when the user submits a working directory to start a swarm.
type SwarmStartMsg struct {
	ParentTaskID string
	WorkingDir   string
}

// SwarmInitCancelledMsg is emitted when the user cancels the init overlay.
type SwarmInitCancelledMsg struct{}

// SwarmInitOverlay collects the working directory for a new swarm.
type SwarmInitOverlay struct {
	parentTaskID string
	parentTitle  string
	workingDir   string
	err          string
	width        int
	height       int
}

// NewSwarmInit creates the overlay, pre-filled with the suggested working dir.
func NewSwarmInit(parentTaskID, parentTitle, suggested string) SwarmInitOverlay {
	return SwarmInitOverlay{
		parentTaskID: parentTaskID,
		parentTitle:  parentTitle,
		workingDir:   suggested,
	}
}

func (m SwarmInitOverlay) Init() tea.Cmd { return nil }

func (m SwarmInitOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return SwarmInitCancelledMsg{} }
		case "enter":
			if err := validateWorkingDir(m.workingDir); err != nil {
				m.err = err.Error()
				return m, nil
			}
			parent, dir := m.parentTaskID, m.workingDir
			return m, func() tea.Msg {
				return SwarmStartMsg{ParentTaskID: parent, WorkingDir: dir}
			}
		case "backspace":
			if len(m.workingDir) > 0 {
				m.workingDir = m.workingDir[:len(m.workingDir)-1]
				m.err = ""
			}
			return m, nil
		default:
			if msg.Type == tea.KeySpace {
				m.workingDir += " "
			} else if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
				m.workingDir += string(msg.Runes)
				m.err = ""
			}
			return m, nil
		}
	}
	return m, nil
}

func (m SwarmInitOverlay) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.AccentPurple)
	labelStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
	inputStyle := lipgloss.NewStyle().Foreground(theme.AccentPurple)
	errStyle := lipgloss.NewStyle().Foreground(theme.SyncError)
	hintStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)

	heading := titleStyle.Render(fmt.Sprintf("Start swarm — %s: %s", m.parentTaskID, m.parentTitle))
	prompt := labelStyle.Render("Working directory: ") + inputStyle.Render(m.workingDir+"█")

	lines := []string{heading, "", prompt}
	if m.err != "" {
		lines = append(lines, "", errStyle.Render(m.err))
	}
	lines = append(lines, "", hintStyle.Render("enter start · esc cancel"))
	return RenderPanel(lipgloss.JoinVertical(lipgloss.Left, lines...), m.width, m.height)
}

func validateWorkingDir(path string) error {
	if path == "" {
		return fmt.Errorf("working directory is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("path not accessible: %v", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory")
	}
	return nil
}
