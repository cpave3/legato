package overlay

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

type AgentSessionChoiceMsg struct {
	Spawn            AgentSpawnSubmitMsg
	SessionID        string
	ResumeWorkingDir string
	Resume           bool
}

type AgentSessionCancelledMsg struct{}

type AgentSessionOverlay struct {
	ResumeWorkingDir string
	agentKind        string
	spawn            AgentSpawnSubmitMsg
	id               string
	width            int
	height           int
}

func NewAgentSession(spawn AgentSpawnSubmitMsg, id, agentKind string) AgentSessionOverlay {
	return AgentSessionOverlay{spawn: spawn, id: id, agentKind: agentKind}
}

func (m AgentSessionOverlay) Init() tea.Cmd { return nil }

func (m AgentSessionOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "r", "enter":
			return m, func() tea.Msg {
				return AgentSessionChoiceMsg{Spawn: m.spawn, SessionID: m.id, ResumeWorkingDir: m.ResumeWorkingDir, Resume: true}
			}
		case "n":
			return m, func() tea.Msg { return AgentSessionChoiceMsg{Spawn: m.spawn, SessionID: m.id} }
		case "esc", "q":
			return m, func() tea.Msg { return AgentSessionCancelledMsg{} }
		}
	}
	return m, nil
}

func (m AgentSessionOverlay) View() string {
	label := m.agentKind
	switch m.agentKind {
	case "claude-code":
		label = "Claude Code"
	case "codex":
		label = "Codex"
	case "chimera":
		label = "Chimera"
	}
	title := lipgloss.NewStyle().Bold(true).Foreground(theme.TextPrimary).Render(fmt.Sprintf("Existing %s session", label))
	text := lipgloss.NewStyle().Foreground(theme.TextSecondary).Render(fmt.Sprintf("Task %s is linked to %s", m.spawn.TaskID, m.id))
	hint := lipgloss.NewStyle().Foreground(theme.TextTertiary).Render("enter/r = resume  n = new  esc = cancel")
	return RenderPanel(lipgloss.JoinVertical(lipgloss.Left, title, text, "", hint), m.width, m.height)
}
