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

// SidebarWidth is the fixed width of the agent sidebar in characters.
const SidebarWidth = 30

// Model is the agent view Bubbletea model.
type Model struct {
	agents      []service.AgentSession
	selected    int
	termContent string
	width       int
	height      int
	polling     bool
	icons       theme.Icons
}

// New creates a new agent view model.
func New(icons theme.Icons) Model {
	return Model{icons: icons}
}

// SetAgents updates the agent list.
func (m *Model) SetAgents(agents []service.AgentSession) {
	m.agents = agents
	if m.selected >= len(agents) {
		m.selected = max(0, len(agents)-1)
	}
}

// SelectByTaskID moves selection to the agent with the given ticket ID.
func (m *Model) SelectByTaskID(ticketID string) {
	for i, a := range m.agents {
		if a.TaskID == ticketID {
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
		if msg.SelectTask != "" {
			m.SelectByTaskID(msg.SelectTask)
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
		w, h := m.terminalDims()
		return m, func() tea.Msg { return SpawnAgentMsg{Width: w, Height: h} }
	case "X":
		if a := m.SelectedAgent(); a != nil {
			return m, func() tea.Msg { return KillAgentMsg{TaskID: a.TaskID} }
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

// View renders the agent view: sidebar on the left, terminal panel on the right.
func (m Model) View() string {
	if m.width == 0 {
		return ""
	}

	sidebar := m.renderSidebar()
	termPanel := m.renderTerminalPanel()

	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, termPanel)
}

func (m Model) renderSidebar() string {
	sidebarContentWidth := SidebarWidth - 1 // -1 for right border

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(theme.TextTertiary).
		PaddingLeft(1).
		PaddingBottom(1)
	header := headerStyle.Render("ACTIVE AGENTS")

	// Keybinding hints at the bottom
	hints := m.renderSidebarHints(sidebarContentWidth)
	hintsHeight := lipgloss.Height(hints)
	headerHeight := lipgloss.Height(header)

	// Available height for the agent list
	listHeight := m.height - headerHeight - hintsHeight
	if listHeight < 1 {
		listHeight = 1
	}

	// Agent entries (or empty state)
	var entries string
	if len(m.agents) == 0 {
		emptyStyle := lipgloss.NewStyle().
			Foreground(theme.TextTertiary).
			PaddingLeft(1).
			Width(sidebarContentWidth)
		entries = emptyStyle.Render("No agents\n\nPress s to spawn")
	} else {
		var entryLines []string
		for i, a := range m.agents {
			entryLines = append(entryLines, m.renderSidebarEntry(a, i == m.selected, sidebarContentWidth))
		}
		entries = strings.Join(entryLines, "\n")

		// Scroll if entries exceed listHeight
		entryRenderedHeight := lipgloss.Height(entries)
		if entryRenderedHeight > listHeight {
			lines := strings.Split(entries, "\n")
			// Find the selected entry position — approximate by card height
			cardHeight := 5 // each card is ~5 lines (status+id, command, title, padding/margin)
			selectedStart := m.selected * cardHeight
			scrollOffset := selectedStart - listHeight/2
			if scrollOffset < 0 {
				scrollOffset = 0
			}
			if scrollOffset+listHeight > len(lines) {
				scrollOffset = len(lines) - listHeight
			}
			if scrollOffset < 0 {
				scrollOffset = 0
			}
			end := scrollOffset + listHeight
			if end > len(lines) {
				end = len(lines)
			}
			entries = strings.Join(lines[scrollOffset:end], "\n")
		}
	}

	// Constrain list to available height
	listStyle := lipgloss.NewStyle().
		Width(sidebarContentWidth).
		Height(listHeight)
	list := listStyle.Render(entries)

	// Compose sidebar
	content := lipgloss.JoinVertical(lipgloss.Left, header, list, hints)

	return lipgloss.NewStyle().
		Width(SidebarWidth).
		Height(m.height).
		BorderRight(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(theme.TextTertiary).
		Render(content)
}

func (m Model) renderSidebarEntry(a service.AgentSession, selected bool, width int) string {
	// Card-like content width (minus border + padding)
	cardContentWidth := width - 4 // 2 border + 2 padding
	if cardContentWidth < 8 {
		cardContentWidth = 8
	}

	// Colors vary based on selection (dark-on-light when selected)
	var statusLine string
	idStyle := lipgloss.NewStyle().Foreground(theme.TextSecondary).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)

	if selected {
		// Selected: dark text on light background — all inner styles must set bg too
		selBg := lipgloss.Color("#EEEDFE")
		selDark := lipgloss.Color("#3C3489")
		selDim := lipgloss.Color("#534AB7")
		idStyle = lipgloss.NewStyle().Foreground(selDark).Background(selBg).Bold(true)
		dimStyle = lipgloss.NewStyle().Foreground(selDim).Background(selBg)

		// Activity indicator uses selected-friendly colors
		selIndicator := func(fg lipgloss.Color, bold bool) lipgloss.Style {
			s := lipgloss.NewStyle().Foreground(fg).Background(selBg)
			if bold {
				s = s.Bold(true)
			}
			return s
		}
		switch {
		case a.Status == "dead":
			statusLine = selIndicator(lipgloss.Color("#993C1D"), false).
				Render(m.icons.Warning + " DEAD")
		case a.Activity == "working":
			statusLine = selIndicator(lipgloss.Color("#0F6E56"), true).
				Render(m.icons.AgentWorking + " RUNNING")
		case a.Activity == "waiting":
			statusLine = selIndicator(selDim, true).
				Render(m.icons.AgentWaiting + " WAITING")
		default:
			statusLine = selIndicator(selDim, false).
				Render(m.icons.Terminal + " IDLE")
		}
	} else {
		// Unselected: light text on dark background
		switch {
		case a.Status == "dead":
			statusLine = lipgloss.NewStyle().Foreground(theme.SyncError).
				Render(m.icons.Warning + " DEAD")
		case a.Activity == "working":
			statusLine = lipgloss.NewStyle().Foreground(theme.SyncOK).Bold(true).
				Render(m.icons.AgentWorking + " RUNNING")
		case a.Activity == "waiting":
			statusLine = lipgloss.NewStyle().Foreground(theme.ColReady).Bold(true).
				Render(m.icons.AgentWaiting + " WAITING")
		default:
			statusLine = lipgloss.NewStyle().Foreground(theme.TextTertiary).
				Render(m.icons.Terminal + " IDLE")
		}
	}

	idStr := truncateID(a.TaskID, cardContentWidth)

	line1 := statusLine + idStyle.Render(" "+idStr)
	line2 := dimStyle.Render(a.Command)

	content := line1 + "\n" + line2

	// Add title line if present, truncated to fit
	if a.Title != "" {
		titleStr := truncateID(a.Title, cardContentWidth)
		content += "\n" + dimStyle.Render(titleStr)
	}

	// Card styling
	if selected {
		return lipgloss.NewStyle().
			Background(lipgloss.Color("#EEEDFE")).
			Width(cardContentWidth).
			Padding(0, 1).
			MarginBottom(1).
			MarginLeft(1).
			BorderStyle(lipgloss.NormalBorder()).
			BorderLeft(true).
			BorderRight(false).
			BorderTop(false).
			BorderBottom(false).
			BorderForeground(theme.AccentPurple).
			Render(content)
	}

	return lipgloss.NewStyle().
		Background(lipgloss.Color("#252540")).
		Foreground(theme.TextPrimary).
		Width(cardContentWidth).
		Padding(0, 1).
		MarginBottom(1).
		MarginLeft(1).
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderRight(false).
		BorderTop(false).
		BorderBottom(false).
		BorderForeground(theme.TextTertiary).
		Render(content)
}

func (m Model) renderSidebarHints(width int) string {
	hintStyle := lipgloss.NewStyle().
		Foreground(theme.TextTertiary).
		PaddingLeft(1).
		Width(width).
		BorderTop(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(theme.TextTertiary)

	keyStyle := lipgloss.NewStyle().Foreground(theme.TextSecondary)

	hints := fmt.Sprintf("%s select  %s spawn  %s kill\n%s attach  %s board",
		keyStyle.Render("j/k"),
		keyStyle.Render("s"),
		keyStyle.Render("X"),
		keyStyle.Render("↵"),
		keyStyle.Render("esc"),
	)

	return hintStyle.Render(hints)
}

func (m Model) renderTerminalPanel() string {
	termW := m.terminalWidth()

	// Terminal header
	header := m.renderTerminalHeader(termW)
	headerHeight := lipgloss.Height(header)

	// Terminal content
	termHeight := m.height - headerHeight
	if termHeight < 1 {
		termHeight = 1
	}
	terminal := m.renderTerminal(termW, termHeight)

	return lipgloss.JoinVertical(lipgloss.Left, header, terminal)
}

func (m Model) renderTerminalHeader(width int) string {
	if len(m.agents) == 0 {
		return lipgloss.NewStyle().
			Width(width).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(theme.TextTertiary).
			Padding(0, 1).
			Foreground(theme.TextTertiary).
			Render("")
	}

	a := m.agents[m.selected]

	// Status dot
	var statusDot string
	switch {
	case a.Status == "dead":
		statusDot = lipgloss.NewStyle().Foreground(theme.SyncError).Render("●")
	case a.Activity == "working":
		statusDot = lipgloss.NewStyle().Foreground(theme.SyncOK).Bold(true).Render("●")
	case a.Activity == "waiting":
		statusDot = lipgloss.NewStyle().Foreground(theme.ColReady).Render("●")
	default:
		if a.Status == "running" {
			statusDot = lipgloss.NewStyle().Foreground(theme.SyncOK).Render("●")
		} else {
			statusDot = lipgloss.NewStyle().Foreground(theme.SyncError).Render("●")
		}
	}

	elapsed := formatDuration(time.Since(a.StartedAt).Truncate(time.Second))

	ticketStyle := lipgloss.NewStyle().Foreground(theme.AccentPurple).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)

	// Build left side: status · ID [· title] · command · elapsed
	var left string
	if a.Title != "" {
		// Reserve space for fixed parts, truncate title to fit
		fixedWidth := lipgloss.Width(statusDot) + lipgloss.Width(ticketStyle.Render(a.TaskID)) +
			lipgloss.Width(a.Command) + lipgloss.Width(elapsed) + 12 // separators + spaces
		maxTitleWidth := width - fixedWidth - 20 // leave room for right label
		if maxTitleWidth < 5 {
			maxTitleWidth = 5
		}
		titleStr := truncateID(a.Title, maxTitleWidth)
		left = fmt.Sprintf("%s %s %s %s %s %s %s %s",
			statusDot,
			ticketStyle.Render(a.TaskID),
			dimStyle.Render("·"),
			dimStyle.Render(titleStr),
			dimStyle.Render("·"),
			a.Command,
			dimStyle.Render("·"),
			elapsed,
		)
	} else {
		left = fmt.Sprintf("%s %s %s %s %s %s",
			statusDot,
			ticketStyle.Render(a.TaskID),
			dimStyle.Render("·"),
			a.Command,
			dimStyle.Render("·"),
			elapsed,
		)
	}

	// Right: status indicator — matches board card indicators
	var liveLabel string
	switch {
	case a.Status == "dead":
		liveLabel = lipgloss.NewStyle().Foreground(theme.SyncError).Render(m.icons.Warning + " DEAD")
	case a.Activity == "working":
		liveLabel = lipgloss.NewStyle().Foreground(theme.SyncOK).Bold(true).Render(m.icons.AgentWorking + " RUNNING")
	case a.Activity == "waiting":
		liveLabel = lipgloss.NewStyle().Foreground(theme.ColReady).Bold(true).Render(m.icons.AgentWaiting + " WAITING")
	default:
		liveLabel = lipgloss.NewStyle().Foreground(theme.TextTertiary).Render(m.icons.Terminal + " IDLE")
	}

	gap := width - lipgloss.Width(left) - lipgloss.Width(liveLabel) - 2
	if gap < 1 {
		gap = 1
	}
	headerLine := left + strings.Repeat(" ", gap) + liveLabel

	return lipgloss.NewStyle().
		Width(width).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(theme.TextTertiary).
		Padding(0, 1).
		Render(headerLine)
}

func (m Model) renderTerminal(width, height int) string {
	if height < 1 {
		return ""
	}

	a := m.SelectedAgent()
	if a == nil {
		return lipgloss.NewStyle().
			Width(width).
			Height(height).
			Foreground(theme.TextTertiary).
			Padding(1, 2).
			Render("No agent selected")
	}

	content := m.termContent
	lines := strings.Split(content, "\n")

	// Truncate/pad each line to terminal width
	for i, line := range lines {
		if len(line) > width {
			lines[i] = line[:width]
		}
	}

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
		Width(width).
		MaxHeight(height).
		Render(strings.Join(lines, "\n"))
}

func truncateID(id string, maxWidth int) string {
	if len(id) <= maxWidth {
		return id
	}
	if maxWidth <= 3 {
		return id[:maxWidth]
	}
	return id[:maxWidth-1] + "…"
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

func (m Model) terminalWidth() int {
	w := m.width - SidebarWidth
	if w < 1 {
		w = 1
	}
	return w
}

func (m Model) terminalDims() (int, int) {
	return m.terminalWidth(), m.height
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
