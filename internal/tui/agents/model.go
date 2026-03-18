package agents

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

const pollInterval = 200 * time.Millisecond

// Model is the agent view Bubbletea model.
type Model struct {
	agents      []service.AgentSession
	selected    int
	termContent string
	width       int
	height      int
	polling     bool
}

// New creates a new agent view model.
func New() Model {
	return Model{}
}

// SetAgents updates the agent list.
func (m *Model) SetAgents(agents []service.AgentSession) {
	m.agents = agents
	if m.selected >= len(agents) {
		m.selected = max(0, len(agents)-1)
	}
}

// SelectByTicketID moves selection to the agent with the given ticket ID.
func (m *Model) SelectByTicketID(ticketID string) {
	for i, a := range m.agents {
		if a.TicketID == ticketID {
			m.selected = i
			return
		}
	}
}

// SetSize updates the dimensions.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SelectedAgent returns the currently selected agent, or nil.
func (m Model) SelectedAgent() *service.AgentSession {
	if len(m.agents) == 0 || m.selected >= len(m.agents) {
		return nil
	}
	a := m.agents[m.selected]
	return &a
}

// StartPolling enables capture polling.
func (m *Model) StartPolling() {
	m.polling = true
}

// StopPolling disables capture polling.
func (m *Model) StopPolling() {
	m.polling = false
}

// Init returns nil — polling is started externally.
func (m Model) Init() tea.Cmd {
	return nil
}

// TickCmd returns a tick command for capture polling.
func TickCmd() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

type tickMsg time.Time

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case AgentsRefreshedMsg:
		m.agents = msg.Agents
		if m.selected >= len(m.agents) {
			m.selected = max(0, len(m.agents)-1)
		}
		if msg.SelectTicket != "" {
			m.SelectByTicketID(msg.SelectTicket)
		}
		return m, nil

	case CaptureOutputMsg:
		m.termContent = msg.Output
		return m, nil

	case tickMsg:
		if !m.polling {
			return m, nil
		}
		return m, TickCmd()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "j":
		if m.selected < len(m.agents)-1 {
			m.selected++
			m.termContent = "" // clear stale content while new capture loads
		}
		return m, nil
	case "k":
		if m.selected > 0 {
			m.selected--
			m.termContent = ""
		}
		return m, nil
	case "s":
		return m, func() tea.Msg { return SpawnAgentMsg{} }
	case "X":
		if a := m.SelectedAgent(); a != nil {
			return m, func() tea.Msg { return KillAgentMsg{TicketID: a.TicketID} }
		}
		return m, nil
	case "enter", "tab":
		if a := m.SelectedAgent(); a != nil {
			session := a.TmuxSession
			return m, func() tea.Msg { return AttachSessionMsg{TmuxSession: session} }
		}
		return m, nil
	case "esc":
		return m, func() tea.Msg { return ReturnToBoardMsg{} }
	}
	return m, nil
}

// View renders the agent view: header bar on top, full-width terminal below.
func (m Model) View() string {
	if m.width == 0 {
		return ""
	}

	header := m.renderHeader()
	headerHeight := lipgloss.Height(header)
	termHeight := m.height - headerHeight
	if termHeight < 1 {
		termHeight = 1
	}
	terminal := m.renderTerminal(termHeight)

	return lipgloss.JoinVertical(lipgloss.Left, header, terminal)
}

func (m Model) renderHeader() string {
	if len(m.agents) == 0 {
		left := lipgloss.NewStyle().Foreground(theme.TextTertiary).
			Render("No agents running — press s to spawn or esc to return")
		return lipgloss.NewStyle().
			Width(m.width).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(theme.TextTertiary).
			Padding(0, 1).
			Render(left)
	}

	a := m.agents[m.selected]

	// Status dot
	var statusDot string
	if a.Status == "running" {
		statusDot = lipgloss.NewStyle().Foreground(theme.SyncOK).Render("●")
	} else {
		statusDot = lipgloss.NewStyle().Foreground(theme.SyncError).Render("●")
	}

	elapsed := formatDuration(time.Since(a.StartedAt).Truncate(time.Second))

	// Left side: agent info
	ticketStyle := lipgloss.NewStyle().Foreground(theme.AccentPurple).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)

	left := fmt.Sprintf("%s %s %s %s %s %s",
		statusDot,
		ticketStyle.Render(a.TicketID),
		dimStyle.Render("·"),
		a.Command,
		dimStyle.Render("·"),
		elapsed,
	)

	// Agent count (if multiple)
	if len(m.agents) > 1 {
		counter := fmt.Sprintf("[%d/%d]", m.selected+1, len(m.agents))
		left += "  " + dimStyle.Render(counter)
	}

	// Right side: keybindings
	keys := []struct{ key, desc string }{
		{"j/k", "prev/next"},
		{"↵", "attach"},
		{"X", "kill"},
		{"s", "spawn"},
		{"esc", "board"},
	}
	var hints []string
	for _, k := range keys {
		key := lipgloss.NewStyle().Foreground(theme.AccentPurple).Bold(true).Render(k.key)
		hints = append(hints, key+" "+dimStyle.Render(k.desc))
	}
	right := strings.Join(hints, "  ")

	// Compose header line
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2 // -2 for padding
	if gap < 1 {
		gap = 1
	}
	headerLine := left + strings.Repeat(" ", gap) + right

	return lipgloss.NewStyle().
		Width(m.width).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(theme.TextTertiary).
		Padding(0, 1).
		Render(headerLine)
}

func (m Model) renderTerminal(height int) string {
	if height < 1 {
		return ""
	}

	a := m.SelectedAgent()
	if a == nil {
		return lipgloss.NewStyle().
			Width(m.width).
			Height(height).
			Foreground(theme.TextTertiary).
			Padding(1, 2).
			Render("No agent selected")
	}

	content := m.termContent
	lines := strings.Split(content, "\n")

	// Strip trailing empty lines (tmux pads the pane to full height)
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}

	// Show bottom of content (most recent output), strictly clipped to height
	if len(lines) > height {
		lines = lines[len(lines)-height:]
	}

	// Pad to exactly `height` lines so the layout is stable
	for len(lines) < height {
		lines = append(lines, "")
	}

	return lipgloss.NewStyle().
		Width(m.width).
		MaxHeight(height).
		Render(strings.Join(lines, "\n"))
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		return "0s"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %02dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
