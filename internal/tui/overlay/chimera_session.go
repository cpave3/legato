package overlay

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

type ChimeraSessionChoiceMsg struct {
	Spawn     AgentSpawnSubmitMsg
	SessionID string
	Resume    bool
}

type ChimeraSessionCancelledMsg struct{}

type ChimeraSessionOverlay struct {
	spawn  AgentSpawnSubmitMsg
	id     string
	width  int
	height int
}

func NewChimeraSession(spawn AgentSpawnSubmitMsg, id string) ChimeraSessionOverlay {
	return ChimeraSessionOverlay{spawn: spawn, id: id}
}

func (m ChimeraSessionOverlay) Init() tea.Cmd { return nil }

func (m ChimeraSessionOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "r":
			return m, func() tea.Msg { return ChimeraSessionChoiceMsg{Spawn: m.spawn, SessionID: m.id, Resume: true} }
		case "n":
			return m, func() tea.Msg { return ChimeraSessionChoiceMsg{Spawn: m.spawn, SessionID: m.id} }
		case "esc", "q":
			return m, func() tea.Msg { return ChimeraSessionCancelledMsg{} }
		}
	}
	return m, nil
}

func (m ChimeraSessionOverlay) View() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(theme.TextPrimary).Render("Existing Chimera session")
	text := lipgloss.NewStyle().Foreground(theme.TextSecondary).Render(fmt.Sprintf("Task %s is linked to %s", m.spawn.TaskID, m.id))
	hint := lipgloss.NewStyle().Foreground(theme.TextTertiary).Render("r = resume  n = new  esc = cancel")
	return RenderPanel(lipgloss.JoinVertical(lipgloss.Left, title, text, "", hint), m.width, m.height)
}
