package overlay

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// AgentMessageSentMsg is sent when the user confirms sending a message.
type AgentMessageSentMsg struct {
	TaskID       string
	ParentTaskID string
	Role         string
	Text         string
}

// AgentCloseConfirmedMsg is sent when the user confirms closing a worker.
type AgentCloseConfirmedMsg struct {
	TaskID string
}

// SwarmFinishConfirmedMsg is sent when the user confirms finishing a swarm.
type SwarmFinishConfirmedMsg struct {
	ParentTaskID string
	Summary      string
}

// AgentActionCancelledMsg is sent when the user dismisses the action overlay.
type AgentActionCancelledMsg struct{}

type agentActionMode int

const (
	agentActionMenu agentActionMode = iota
	agentActionMessageInput
	agentActionConfirm
	agentActionFinishSummary
)

// menuOption represents a selectable action in the menu.
type menuOption struct {
	label    string
	isDanger bool
}

// AgentActionOverlay shows role-aware actions for a swarm agent.
type AgentActionOverlay struct {
	taskID       string
	parentTaskID string
	role         string
	mode         agentActionMode
	options      []menuOption
	cursor       int
	text         string // input text for message or summary
	width        int
	height       int
}

// NewAgentAction creates an action overlay for the given agent.
// parentTaskID must be non-empty; role controls which options appear.
func NewAgentAction(taskID, parentTaskID, role string) AgentActionOverlay {
	opts := []menuOption{{label: "Send message"}}
	if role == "conductor" {
		opts = append(opts, menuOption{label: "Finish swarm"})
	} else {
		opts = append(opts, menuOption{label: "Close worker", isDanger: true})
	}
	return AgentActionOverlay{
		taskID:       taskID,
		parentTaskID: parentTaskID,
		role:         role,
		mode:         agentActionMenu,
		options:      opts,
	}
}

// Init returns no command.
func (m AgentActionOverlay) Init() tea.Cmd { return nil }

// Update handles messages.
func (m AgentActionOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m AgentActionOverlay) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case agentActionMenu:
		return m.handleMenuKey(msg)
	case agentActionMessageInput:
		return m.handleMessageInputKey(msg)
	case agentActionConfirm:
		return m.handleConfirmKey(msg)
	case agentActionFinishSummary:
		return m.handleFinishSummaryKey(msg)
	}
	return m, nil
}

func (m AgentActionOverlay) handleMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		return m, func() tea.Msg { return AgentActionCancelledMsg{} }
	case "j", "down":
		if m.cursor < len(m.options)-1 {
			m.cursor++
		}
		return m, nil
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case "enter":
		if m.cursor >= len(m.options) {
			return m, nil
		}
		opt := m.options[m.cursor]
		switch opt.label {
		case "Send message":
			m.mode = agentActionMessageInput
			m.text = ""
			return m, nil
		case "Close worker":
			m.mode = agentActionConfirm
			return m, nil
		case "Finish swarm":
			m.mode = agentActionFinishSummary
			m.text = ""
			return m, nil
		}
	}
	return m, nil
}

func (m AgentActionOverlay) handleMessageInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = agentActionMenu
		m.text = ""
		return m, nil
	case "enter":
		if m.text != "" {
			return m, func() tea.Msg {
				return AgentMessageSentMsg{
					TaskID:       m.taskID,
					ParentTaskID: m.parentTaskID,
					Role:         m.role,
					Text:         m.text,
				}
			}
		}
		return m, nil
	case "backspace":
		if len(m.text) > 0 {
			m.text = m.text[:len(m.text)-1]
		}
		return m, nil
	default:
		if msg.Type == tea.KeySpace {
			m.text += " "
			return m, nil
		}
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
			var clean []rune
			for _, r := range msg.Runes {
				if r >= 32 {
					clean = append(clean, r)
				}
			}
			if len(clean) > 0 {
				m.text += string(clean)
			}
			return m, nil
		}
		return m, nil
	}
}

func (m AgentActionOverlay) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		return m, func() tea.Msg {
			return AgentCloseConfirmedMsg{TaskID: m.taskID}
		}
	case "n", "esc", "q":
		m.mode = agentActionMenu
		return m, nil
	}
	return m, nil
}

func (m AgentActionOverlay) handleFinishSummaryKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = agentActionMenu
		m.text = ""
		return m, nil
	case "enter":
		if m.text != "" {
			return m, func() tea.Msg {
				return SwarmFinishConfirmedMsg{
					ParentTaskID: m.parentTaskID,
					Summary:      m.text,
				}
			}
		}
		return m, nil
	case "backspace":
		if len(m.text) > 0 {
			m.text = m.text[:len(m.text)-1]
		}
		return m, nil
	default:
		if msg.Type == tea.KeySpace {
			m.text += " "
			return m, nil
		}
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
			m.text += string(msg.Runes)
			return m, nil
		}
		return m, nil
	}
}

// View renders the overlay.
func (m AgentActionOverlay) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.TextPrimary).Padding(0, 1)
	hintStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary).Padding(0, 1)

	switch m.mode {
	case agentActionMenu:
		return m.viewMenu(titleStyle, hintStyle)
	case agentActionMessageInput:
		return m.viewMessageInput(titleStyle, hintStyle)
	case agentActionConfirm:
		return m.viewConfirm(titleStyle, hintStyle)
	case agentActionFinishSummary:
		return m.viewFinishSummary(titleStyle, hintStyle)
	}
	return ""
}

func (m AgentActionOverlay) viewMenu(titleStyle, hintStyle lipgloss.Style) string {
	var lines []string
	lines = append(lines, titleStyle.Render("Agent Actions"))
	lines = append(lines, "")

	for i, opt := range m.options {
		prefix := "  "
		style := lipgloss.NewStyle().Foreground(theme.TextSecondary)
		if i == m.cursor {
			prefix = "> "
			style = lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true)
		}
		if opt.isDanger {
			style = lipgloss.NewStyle().Foreground(theme.SyncError)
			if i == m.cursor {
				style = lipgloss.NewStyle().Foreground(theme.SyncError).Bold(true)
			}
		}
		lines = append(lines, style.Render(prefix+opt.label))
	}

	lines = append(lines, "")
	lines = append(lines, hintStyle.Render("j/k navigate · ↵ select · esc cancel"))
	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return RenderPanel(content, m.width, m.height)
}

func (m AgentActionOverlay) viewMessageInput(titleStyle, hintStyle lipgloss.Style) string {
	lines := []string{
		titleStyle.Render("Send Message"),
		"",
		lipgloss.NewStyle().Foreground(theme.AccentPurple).Render(m.text + "█"),
		"",
		hintStyle.Render("enter send · esc cancel"),
	}
	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return RenderPanel(content, m.width, m.height)
}

func (m AgentActionOverlay) viewConfirm(titleStyle, hintStyle lipgloss.Style) string {
	label := "Close worker?"
	if m.role == "conductor" {
		label = "Finish swarm?"
	}
	lines := []string{
		titleStyle.Render("Confirm"),
		"",
		lipgloss.NewStyle().Foreground(theme.TextSecondary).Padding(0, 1).Render(label),
		"",
		hintStyle.Render("y confirm · n/esc cancel"),
	}
	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return RenderPanel(content, m.width, m.height)
}

func (m AgentActionOverlay) viewFinishSummary(titleStyle, hintStyle lipgloss.Style) string {
	lines := []string{
		titleStyle.Render("Finish Swarm"),
		"",
		lipgloss.NewStyle().Foreground(theme.TextSecondary).Padding(0, 1).Render("Summary:"),
		lipgloss.NewStyle().Foreground(theme.AccentPurple).Render(m.text + "█"),
		"",
		hintStyle.Render("enter finish · esc cancel"),
	}
	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return RenderPanel(content, m.width, m.height)
}
