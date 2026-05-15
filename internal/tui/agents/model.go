package agents

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

const pollInterval = 200 * time.Millisecond

// SidebarWidth is the fixed width of the agent sidebar in characters.
const SidebarWidth = 30

// DurationData holds cumulative working/waiting durations for a task.
type DurationData struct {
	Working time.Duration
	Waiting time.Duration
}

// Model is the agent view Bubbletea model.
type Model struct {
	agents      []service.AgentSession
	durations   map[string]DurationData
	timelines   map[string][]string
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

// SetAgents updates the agent list. Agents are grouped so swarm participants
// for the same parent_task_id are contiguous (conductor first, then workers
// alphabetically by sub-task ID), and solo (non-swarm) sessions appear after
// all swarm groups.
func (m *Model) SetAgents(agents []service.AgentSession) {
	// Preserve selection by ID across re-orderings.
	prevSelTaskID := ""
	if m.selected < len(m.agents) {
		prevSelTaskID = m.agents[m.selected].TaskID
	}

	m.agents = sortAgentsForGrouping(agents)

	// Re-locate the selection if it still exists.
	m.selected = 0
	for i, a := range m.agents {
		if a.TaskID == prevSelTaskID {
			m.selected = i
			break
		}
	}
	if m.selected >= len(m.agents) {
		m.selected = max(0, len(m.agents)-1)
	}
}

// sortAgentsForGrouping returns agents ordered so swarm sessions cluster by
// parent_task_id (conductor first within each group), with solo sessions at
// the end.
func sortAgentsForGrouping(agents []service.AgentSession) []service.AgentSession {
	swarmsOrder := []string{}
	swarms := map[string][]service.AgentSession{}
	var solo []service.AgentSession
	for _, a := range agents {
		if a.ParentTaskID == "" {
			solo = append(solo, a)
			continue
		}
		if _, ok := swarms[a.ParentTaskID]; !ok {
			swarmsOrder = append(swarmsOrder, a.ParentTaskID)
		}
		swarms[a.ParentTaskID] = append(swarms[a.ParentTaskID], a)
	}
	out := make([]service.AgentSession, 0, len(agents))
	for _, parentID := range swarmsOrder {
		group := swarms[parentID]
		// Conductor first, then workers ordered by sub-task ID for stability.
		var conductor *service.AgentSession
		var workers []service.AgentSession
		for i := range group {
			if group[i].Role == "conductor" {
				c := group[i]
				conductor = &c
			} else {
				workers = append(workers, group[i])
			}
		}
		if conductor != nil {
			out = append(out, *conductor)
		}
		out = append(out, workers...)
	}
	out = append(out, solo...)
	return out
}

// SetDurations updates the working/waiting durations for agent tasks.
func (m *Model) SetDurations(durations map[string]DurationData) {
	m.durations = durations
}

// SetTimelines updates the sparkline timeline data per agent task ID.
func (m *Model) SetTimelines(timelines map[string][]string) {
	m.timelines = timelines
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
// Agents returns a copy of the current agent slice.
func (m Model) Agents() []service.AgentSession {
	if m.agents == nil {
		return nil
	}
	out := make([]service.AgentSession, len(m.agents))
	copy(out, m.agents)
	return out
}

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
		prevSelTaskID := ""
		if m.selected < len(m.agents) {
			prevSelTaskID = m.agents[m.selected].TaskID
		}
		m.agents = sortAgentsForGrouping(msg.Agents)
		if m.selected >= len(m.agents) {
			m.selected = max(0, len(m.agents)-1)
		}
		if prevSelTaskID != "" {
			m.SelectByTaskID(prevSelTaskID)
		}
		if msg.SelectTask != "" {
			m.SelectByTaskID(msg.SelectTask)
		}
		return m, nil

	case CaptureOutputMsg:
		m.termContent = msg.Output
		return m, nil

	case StateTimelinesRefreshedMsg:
		if m.timelines == nil {
			m.timelines = make(map[string][]string)
		}
		for k, v := range msg.Timelines {
			m.timelines[k] = v
		}
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
		return m, func() tea.Msg { return OpenAgentSpawnMsg{} }
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
	case "m":
		return m, func() tea.Msg { return OpenMacroPickerMsg{} }
	case "M":
		// Action menu is swarm-only; no-op for solo agents.
		if a := m.SelectedAgent(); a != nil && a.ParentTaskID != "" {
			return m, func() tea.Msg {
				return OpenAgentActionMsg{
					TaskID:       a.TaskID,
					ParentTaskID: a.ParentTaskID,
					Role:         a.Role,
				}
			}
		}
		return m, nil
	}
	return m, nil
}

// View renders the agent view: sidebar on the left, terminal panel on the right.
// When the selected agent is a swarm worker, a third panel with worker details
// is appended.
func (m Model) View() string {
	if m.width == 0 {
		return ""
	}

	sidebar := m.renderSidebar()

	a := m.SelectedAgent()
	if a == nil || a.ParentTaskID == "" {
		termPanel := m.renderTerminalPanel()
		return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, termPanel)
	}

	// 3-panel layout: sidebar | terminal (60% of remaining) | worker details (40%)
	remaining := m.width - SidebarWidth
	if remaining < 20 {
		// Too narrow — fall back to 2-panel layout.
		termPanel := m.renderTerminalPanel()
		return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, termPanel)
	}
	termWidth := remaining * 6 / 10
	coordWidth := remaining - termWidth
	termPanel := m.renderTerminalPanelWithWidth(termWidth)
	coordPanel := m.renderWorkerDetailsPanel(coordWidth)
	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, termPanel, coordPanel)
}

// renderTerminalPanelWithWidth is a width-overridden version of renderTerminalPanel
// for the 3-panel coordination layout.
func (m Model) renderTerminalPanelWithWidth(width int) string {
	original := m.width
	m.width = SidebarWidth + width
	out := m.renderTerminalPanel()
	m.width = original
	return out
}

// renderWorkerDetailsPanel renders the selected swarm worker's role, scope,
// and instructions. It replaces the raw JSON swarm snapshot so the user can
// see the purpose of the focused worker at a glance.
func (m Model) renderWorkerDetailsPanel(width int) string {
	a := m.SelectedAgent()
	if a == nil || a.ParentTaskID == "" {
		return ""
	}

	cardW := width - 1 // -1 for border
	if cardW < 4 {
		cardW = 4
	}

	// Header
	header := lipgloss.NewStyle().
		Foreground(theme.AccentPurple).
		Bold(true).
		PaddingLeft(1).
		PaddingBottom(1).
		Render("WORKER")

	// Content lines
	var parts []string
	labelStyle := lipgloss.NewStyle().Foreground(theme.TextSecondary).Bold(true)
	valueStyle := lipgloss.NewStyle().Foreground(theme.TextPrimary)
	wrapStyle := lipgloss.NewStyle().Foreground(theme.TextSecondary)
	dimStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary).Italic(true)

	// Title
	if a.Title != "" {
		parts = append(parts, wrapStyle.Render(a.Title))
	}

	// Role + Subtask
	if a.Role != "" && a.Role != "conductor" {
		parts = append(parts, labelStyle.Render("role")+" "+valueStyle.Render(a.Role))
	}
	if a.SubtaskID != "" {
		parts = append(parts, labelStyle.Render("subtask")+" "+dimStyle.Render(a.SubtaskID))
	}

	// Scope
	if len(a.Scope) > 0 {
		scopeText := strings.Join(a.Scope, "\n")
		parts = append(parts,
			labelStyle.Render("scope"),
			wrapStyle.Render(scopeText),
		)
	}

	// Prompt / instructions
	if a.Prompt != "" {
		parts = append(parts,
			"",
			labelStyle.Render("instructions"),
			wrapStyle.Render(a.Prompt),
		)
	} else if a.Description != "" {
		parts = append(parts,
			"",
			labelStyle.Render("instructions"),
			wrapStyle.Render(a.Description),
		)
	}

	body := lipgloss.NewStyle().
		Padding(0, 1).
		Width(cardW).
		Render(strings.Join(parts, "\n"))

	// Clip to available vertical space (lipgloss Height pads but never truncates)
	bodyHeight := m.height - lipgloss.Height(header)
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	if bodyLines := strings.Split(body, "\n"); len(bodyLines) > bodyHeight {
		body = strings.Join(bodyLines[:bodyHeight], "\n")
	}

	content := lipgloss.JoinVertical(lipgloss.Left, header, body)
	return lipgloss.NewStyle().
		Width(width - 1).
		Height(m.height).
		BorderLeft(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(theme.TextTertiary).
		Render(content)
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
		entries = emptyStyle.Render("No agents\n\nPress s to spawn\nan agent session")
	} else {
		var entryLines []string
		selectedEntryIdx := -1
		var prevParent string
		for i, a := range m.agents {
			// Emit a small group header before the first session of each swarm.
			if a.ParentTaskID != "" && a.ParentTaskID != prevParent {
				entryLines = append(entryLines, m.renderSwarmGroupHeader(a.ParentTaskID, sidebarContentWidth))
				prevParent = a.ParentTaskID
			}
			if a.ParentTaskID == "" && prevParent != "" {
				// Transitioning from swarm groups to solo sessions — emit a divider.
				entryLines = append(entryLines, m.renderSoloDivider(sidebarContentWidth))
				prevParent = ""
			}
			if i == m.selected {
				selectedEntryIdx = len(entryLines)
			}
			entryLines = append(entryLines, m.renderSidebarEntry(a, i == m.selected, sidebarContentWidth))
		}
		entries = strings.Join(entryLines, "\n")

		// Scroll if entries exceed listHeight, using real per-entry heights so
		// variable-height cards (titles, role tags, swarm headers) stay aligned.
		entryRenderedHeight := lipgloss.Height(entries)
		if entryRenderedHeight > listHeight {
			selectedStart := 0
			selectedHeight := 0
			for k, e := range entryLines {
				h := lipgloss.Height(e)
				if k == selectedEntryIdx {
					selectedHeight = h
					break
				}
				selectedStart += h
			}
			margin := (listHeight - selectedHeight) / 2
			if margin < 0 {
				margin = 0
			}
			scrollOffset := selectedStart - margin
			lines := strings.Split(entries, "\n")
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

		// Final clamp: lipgloss Height pads but never truncates, so guarantee
		// the sidebar can't grow past listHeight and break the side-by-side
		// JoinHorizontal layout with the terminal panel.
		if lipgloss.Height(entries) > listHeight {
			lines := strings.Split(entries, "\n")
			entries = strings.Join(lines[:listHeight], "\n")
		}
	}

	// Constrain list to available height
	listStyle := lipgloss.NewStyle().
		Width(sidebarContentWidth).
		Height(listHeight)
	list := listStyle.Render(entries)

	// Compose sidebar
	content := lipgloss.JoinVertical(lipgloss.Left, header, list, hints)

	// Width(SidebarWidth-1) + BorderRight = exactly SidebarWidth columns total.
	// Lipgloss adds borders OUTSIDE the configured Width, so without the -1
	// the sidebar would render 1 col over and force the terminal panel into
	// the next line — breaking the side-by-side layout.
	return lipgloss.NewStyle().
		Width(SidebarWidth - 1).
		Height(m.height).
		BorderRight(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(theme.TextTertiary).
		Render(content)
}

// renderSwarmGroupHeader returns a single-line header that visually marks the
// start of a swarm group in the sidebar. The header includes a swarm icon
// and the parent task ID so the user can tell at a glance which sessions
// belong together.
func (m Model) renderSwarmGroupHeader(parentTaskID string, width int) string {
	style := lipgloss.NewStyle().
		Foreground(theme.AccentPurple).
		Bold(true).
		PaddingLeft(1).
		PaddingTop(1).
		Width(width)
	return style.Render("◆ swarm: " + parentTaskID)
}

// renderSoloDivider visually separates the swarm groups from solo agent
// sessions in the sidebar.
func (m Model) renderSoloDivider(width int) string {
	style := lipgloss.NewStyle().
		Foreground(theme.TextTertiary).
		PaddingLeft(1).
		PaddingTop(1).
		Width(width)
	return style.Render("── solo ──")
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

	// Conductor and worker labels appear before the ID for at-a-glance role
	// recognition. Conductor gets a diamond + accent color; workers get a
	// subtle "└─" branch indicator.
	rolePrefix := ""
	if a.Role == "conductor" {
		bg := lipgloss.Color("#252540")
		if selected {
			bg = lipgloss.Color("#EEEDFE")
		}
		rolePrefix = lipgloss.NewStyle().Foreground(theme.AccentPurple).Background(bg).Bold(true).Render("◆ ")
	} else if a.ParentTaskID != "" {
		bg := lipgloss.Color("#252540")
		if selected {
			bg = lipgloss.Color("#EEEDFE")
		}
		rolePrefix = lipgloss.NewStyle().Foreground(theme.TextTertiary).Background(bg).Render("└─ ")
	}

	// Status on its own line; the role badge + task ID on the next line so
	// the swarm group structure (`◆` for conductor, `└─` for worker) sits
	// directly under the status indicator rather than competing with it.
	line1 := statusLine
	line2 := rolePrefix + idStyle.Render(idStr)
	line3 := dimStyle.Render(a.Command)

	content := line1 + "\n" + line2 + "\n" + line3

	// Add title line if present, truncated to fit
	if a.Title != "" {
		titleStr := truncateID(a.Title, cardContentWidth)
		content += "\n" + dimStyle.Render(titleStr)
	}

	// Add a small role tag below the title for swarm workers (so the user can
	// see "backend", "frontend" etc. without inspecting the brief).
	if a.Role != "" && a.Role != "conductor" && a.ParentTaskID != "" {
		roleTag := lipgloss.NewStyle().Foreground(theme.TextTertiary).Italic(true).Render("role: " + a.Role)
		content += "\n" + roleTag
	}

	// Sparkline
	if tl, ok := m.timelines[a.TaskID]; ok && len(tl) > 0 {
		var bars []string
		for _, s := range tl {
			var bar string
			switch s {
			case "working":
				bar = lipgloss.NewStyle().Foreground(theme.SyncOK).Render("█")
			case "waiting":
				bar = lipgloss.NewStyle().Foreground(theme.ColReady).Render("█")
			default:
				bar = lipgloss.NewStyle().Foreground(theme.TextTertiary).Render("░")
			}
			bars = append(bars, bar)
		}
		content += "\n" + strings.Join(bars, "")
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

	// Border accent: swarm sessions get the accent purple to visually group
	// them together; solo sessions keep the muted gray.
	borderColor := theme.TextTertiary
	if a.ParentTaskID != "" {
		borderColor = theme.AccentPurple
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
		BorderForeground(borderColor).
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
	hints := fmt.Sprintf("%s select  %s spawn  %s kill  %s macro\n%s action  %s attach  %s board",
		keyStyle.Render("j/k"),
		keyStyle.Render("s"),
		keyStyle.Render("X"),
		keyStyle.Render("m"),
		keyStyle.Render("M"),
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

	durationStr := m.renderDurationSummary(a.TaskID)

	ticketStyle := lipgloss.NewStyle().Foreground(theme.AccentPurple).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)

	// Build left side: status · ID [· title] · command [· durations]
	var left string
	if a.Title != "" {
		fixedWidth := lipgloss.Width(statusDot) + lipgloss.Width(ticketStyle.Render(a.TaskID)) +
			lipgloss.Width(a.Command) + lipgloss.Width(durationStr) + 12
		maxTitleWidth := width - fixedWidth - 20
		if maxTitleWidth < 5 {
			maxTitleWidth = 5
		}
		titleStr := truncateID(a.Title, maxTitleWidth)
		parts := []string{
			statusDot,
			ticketStyle.Render(a.TaskID),
			dimStyle.Render("·"),
			dimStyle.Render(titleStr),
			dimStyle.Render("·"),
			a.Command,
		}
		if durationStr != "" {
			parts = append(parts, dimStyle.Render("·"), durationStr)
		}
		left = strings.Join(parts, " ")
	} else {
		parts := []string{
			statusDot,
			ticketStyle.Render(a.TaskID),
			dimStyle.Render("·"),
			a.Command,
		}
		if durationStr != "" {
			parts = append(parts, dimStyle.Render("·"), durationStr)
		}
		left = strings.Join(parts, " ")
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

	// Truncate each line to terminal width (ANSI-aware)
	for i, line := range lines {
		if ansi.StringWidth(line) > width {
			lines[i] = ansi.Truncate(line, width, "")
		}
	}

	// Strip trailing empty lines (tmux pads the pane to full height)
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
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
	if d <= 0 {
		return ""
	}
	totalMinutes := int(d.Minutes())
	if totalMinutes < 1 {
		return "<1m"
	}
	hours := totalMinutes / 60
	minutes := totalMinutes % 60
	if hours == 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	if minutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, minutes)
}

// renderDurationSummary builds a compact working/waiting duration string for the terminal header.
func (m Model) renderDurationSummary(taskID string) string {
	dur, ok := m.durations[taskID]
	if !ok || (dur.Working <= 0 && dur.Waiting <= 0) {
		return ""
	}

	workingColor := theme.SyncOK
	waitingColor := theme.ColReady
	dimStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)

	var parts []string
	if d := formatDuration(dur.Working); d != "" {
		wStyle := lipgloss.NewStyle().Foreground(workingColor)
		parts = append(parts, wStyle.Render(m.icons.AgentWorking+" ")+dimStyle.Render(d))
	}
	if d := formatDuration(dur.Waiting); d != "" {
		wStyle := lipgloss.NewStyle().Foreground(waitingColor)
		parts = append(parts, wStyle.Render(m.icons.AgentWaiting+" ")+dimStyle.Render(d))
	}

	return strings.Join(parts, dimStyle.Render(" · "))
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
