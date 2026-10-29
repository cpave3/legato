package agents

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/hotkey"
	"github.com/cpave3/legato/internal/tui/theme"
)

const pollInterval = 200 * time.Millisecond

// SidebarWidthFor is the width of the agent sidebar in a terminal of the
// given width, its edge included.
func SidebarWidthFor(width int) int {
	switch {
	case width >= 140:
		return 40
	case width >= 100:
		return 34
	default:
		return 28
	}
}

var groupColorPalette = []lipgloss.Color{
	"#0072B2", // blue
	"#009E73", // green
	"#D55E00", // vermilion
	"#CC79A7", // reddish purple
	"#E69F00", // orange
}

// groupColor maps a group name to the fixed color-blind-accessible palette.
func groupColor(name string) lipgloss.Color {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return groupColorPalette[int(h.Sum32())%len(groupColorPalette)]
}

// DurationData holds cumulative working/waiting durations for a task.
type DurationData struct {
	Working time.Duration
	Waiting time.Duration
}

// Model is the agent view Bubbletea model.
type Model struct {
	agents            []service.AgentSession
	durations         map[string]DurationData
	timelines         map[string][]string
	notifyStates      map[string]bool  // taskID -> notify enabled
	shown             map[string]Shown // taskID -> cards and artifacts shown in the web UI
	selected          int
	termContent       string
	width             int
	height            int
	polling           bool
	showWorkerDetails bool
	icons             theme.Icons
	ntfyConfigured    bool
	voiceEnabled      bool
	// syncLabel and webLabel close the sidebar as given.
	syncLabel string
	webLabel  string
	// usage is how many tokens were used; nil when that is not known.
	usage *Usage
	// spinner is the frame the mark of a running agent is drawn at.
	spinner int
	// layout is how the agents are shown: a list beside one terminal, or
	// every agent at once.
	layout Layout
	// previews holds the end of each agent's terminal, for the ensemble, and
	// conversations the end of what a native agent said and did. An agent is
	// in one of the two: the one that was read last.
	previews      map[string][]string
	conversations map[string]service.ConversationTail
	previewsAt    time.Time
	// feed is what changed among the agents since the view first saw them,
	// oldest first, and seen the state each agent was last seen in.
	feed         []feedEvent
	seen         map[string]string
	now          func() time.Time
	recording    bool
	transcribing bool
	audioLevels  []float64
}

// New creates a new agent view model.
func New(icons theme.Icons) Model {
	return Model{icons: icons, showWorkerDetails: true, notifyStates: make(map[string]bool), now: time.Now}
}

// SetNtfyConfigured records whether a notification channel is available.
func (m *Model) SetNtfyConfigured(v bool) {
	m.ntfyConfigured = v
}

// SetVoiceEnabled controls whether the 'v' voice dictation keybinding is active.
func (m *Model) SetVoiceEnabled(v bool) {
	m.voiceEnabled = v
}

// SetSpinner sets the frame the mark of a running agent is drawn at.
func (m *Model) SetSpinner(frame int) {
	m.spinner = frame
}

// HasWorkingAgent reports whether any agent is running, which is what there
// is to animate.
func (m Model) HasWorkingAgent() bool {
	for _, a := range m.agents {
		if stateOf(a) == "working" {
			return true
		}
	}
	return false
}

// SetStatus sets what the sidebar closes with: the sync state and the web
// server, each already worded.
func (m *Model) SetStatus(sync, web string) {
	m.syncLabel = sync
	m.webLabel = web
}

// SetAgents updates the agent list. Agents are listed by what they are doing,
// the ones that need the person first. Within one state, swarm participants
// for the same parent_task_id are contiguous (conductor first, then workers
// alphabetically by sub-task ID), and solo (non-swarm) sessions appear after
// all swarm groups.
func (m *Model) SetAgents(agents []service.AgentSession) {
	// Preserve selection by ID across re-orderings.
	prevSelTaskID := ""
	if m.selected < len(m.agents) {
		prevSelTaskID = m.agents[m.selected].TaskID
	}

	m.observe(agents)
	m.agents = orderAgents(agents)

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

// orderAgents returns agents in the order the view lists them: by section,
// and inside a section as sortAgentsForGrouping clusters them.
func orderAgents(agents []service.AgentSession) []service.AgentSession {
	ordered := sortAgentsForGrouping(agents)
	sort.SliceStable(ordered, func(i, j int) bool {
		return sectionOf(ordered[i]) < sectionOf(ordered[j])
	})
	return ordered
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
	groupOrder := []string{}
	grouped := map[string][]service.AgentSession{}
	var ungrouped []service.AgentSession
	for _, a := range solo {
		if a.Group == "" {
			ungrouped = append(ungrouped, a)
			continue
		}
		if _, ok := grouped[a.Group]; !ok {
			groupOrder = append(groupOrder, a.Group)
		}
		grouped[a.Group] = append(grouped[a.Group], a)
	}
	for _, name := range groupOrder {
		out = append(out, grouped[name]...)
	}
	out = append(out, ungrouped...)
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
		m.observe(msg.Agents)
		m.agents = orderAgents(msg.Agents)
		for _, a := range m.agents {
			m.notifyStates[a.TaskID] = a.NotifyEnabled
		}
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

	case PreviewMsg:
		if m.previews == nil {
			m.previews = make(map[string][]string)
		}
		m.previews[msg.TaskID] = previewLines(msg.Output)
		delete(m.conversations, msg.TaskID)
		return m, nil

	case ConversationMsg:
		if m.conversations == nil {
			m.conversations = make(map[string]service.ConversationTail)
		}
		m.conversations[msg.TaskID] = msg.Tail
		delete(m.previews, msg.TaskID)
		return m, nil

	case ShownRefreshedMsg:
		m.shown = msg.Shown
		if m.shown == nil {
			// Loaded and empty, which is not the same as never loaded: the
			// open key answers once there is somewhere to ask.
			m.shown = map[string]Shown{}
		}
		return m, nil

	case StateTimelinesRefreshedMsg:
		if m.timelines == nil {
			m.timelines = make(map[string][]string)
		}
		for k, v := range msg.Timelines {
			m.timelines[k] = v
		}
		return m, nil

	case NotifyToggledMsg:
		m.notifyStates[msg.TaskID] = msg.Enabled
		return m, nil

	case VoiceRecordingMsg:
		m.recording = msg.Recording
		if msg.Recording {
			m.audioLevels = nil
		}
		return m, nil

	case VoiceLevelMsg:
		m.audioLevels = msg.Levels
		return m, nil

	case VoiceTranscriptionMsg:
		m.transcribing = false
		m.audioLevels = nil
		return m, nil

	case VoiceTranscribingMsg:
		m.transcribing = true
		m.recording = false
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
	ctx := hotkey.Context{Page: hotkey.PageAgents, Mode: m.HotkeyMode(), Capabilities: hotkey.NewCapabilities()}
	if m.ntfyConfigured {
		ctx.Capabilities[hotkey.CapabilityNotifications] = true
	}
	if m.voiceEnabled {
		ctx.Capabilities[hotkey.CapabilityVoice] = true
	}
	if m.shown != nil {
		ctx.Capabilities[hotkey.CapabilityAgentUI] = true
	}
	// The page still checks whether the selected agent belongs to a swarm.
	ctx.Capabilities[hotkey.CapabilitySwarm] = true
	binding, ok := hotkey.DefaultRegistry().Resolve(msg, ctx)
	if !ok || binding.Target != hotkey.TargetPage {
		return m, nil
	}
	return m.HandleAction(binding.ID)
}

// HandleAction executes a registry action owned by the agents model.
func (m Model) HandleAction(action hotkey.ActionID) (Model, tea.Cmd) {
	switch action {
	case hotkey.ActionAgentsEnsemble:
		m.layout = LayoutGrid
		return m, nil
	case hotkey.ActionAgentsList:
		m.layout = LayoutList
		m.termContent = ""
		return m, nil
	case hotkey.ActionAgentsDown, hotkey.ActionAgentsUp, hotkey.ActionAgentsLeft, hotkey.ActionAgentsRight:
		if next := m.neighbour(action); next != m.selected {
			m.selected = next
			m.termContent = "" // clear stale content while new capture loads
		}
		return m, nil
	case hotkey.ActionAgentsNextWaiting:
		// The next agent that needs the person, from the one after the
		// selection and around.
		for step := 1; step <= len(m.agents); step++ {
			next := (m.selected + step) % len(m.agents)
			if sectionOf(m.agents[next]) == sectionNeedsYou {
				if next != m.selected {
					m.selected = next
					m.termContent = ""
				}
				break
			}
		}
		return m, nil
	case hotkey.ActionAgentsSpawn:
		return m, func() tea.Msg { return OpenAgentSpawnMsg{} }
	case hotkey.ActionAgentsKill:
		if a := m.SelectedAgent(); a != nil {
			return m, func() tea.Msg { return KillAgentMsg{TaskID: a.TaskID} }
		}
		return m, nil
	case hotkey.ActionAgentsReload:
		if a := m.SelectedAgent(); a != nil {
			return m, func() tea.Msg { return ReloadAgentMsg{TaskID: a.TaskID} }
		}
		return m, nil
	case hotkey.ActionAgentsAttach:
		if a := m.SelectedAgent(); a != nil {
			session := a.TmuxSession
			return m, func() tea.Msg { return AttachSessionMsg{TmuxSession: session} }
		}
		return m, nil
	case hotkey.ActionBack:
		return m, func() tea.Msg { return ReturnToBoardMsg{} }
	case hotkey.ActionAgentsMacro:
		return m, func() tea.Msg { return OpenMacroPickerMsg{} }
	case hotkey.ActionAgentsOpenShown:
		if a := m.SelectedAgent(); a != nil {
			shown := m.shown[a.TaskID]
			msg := OpenShownMsg{TaskID: a.TaskID, URL: shown.URL, Count: shown.Count()}
			return m, func() tea.Msg { return msg }
		}
		return m, nil
	case hotkey.ActionAgentsGroup:
		if a := m.SelectedAgent(); a != nil {
			return m, func() tea.Msg { return OpenGroupMsg{TaskID: a.TaskID, Group: a.Group} }
		}
		return m, nil
	case hotkey.ActionAgentsActions:
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
	case hotkey.ActionAgentsDetails:
		// Toggle worker details pane (swarm workers only).
		if a := m.SelectedAgent(); a != nil && a.ParentTaskID != "" && a.Role != "conductor" {
			m.showWorkerDetails = !m.showWorkerDetails
		}
		return m, nil
	case hotkey.ActionAgentsNotify:
		if m.ntfyConfigured {
			if a := m.SelectedAgent(); a != nil {
				return m, func() tea.Msg { return ToggleNotifyMsg{TaskID: a.TaskID} }
			}
		}
		return m, nil
	case hotkey.ActionAgentsVoice:
		if m.voiceEnabled {
			if a := m.SelectedAgent(); a != nil && a.Status == "running" {
				return m, func() tea.Msg {
					return VoiceToggleMsg{
						TaskID:      a.TaskID,
						TmuxSession: a.TmuxSession,
						AgentKind:   a.AgentKind,
					}
				}
			}
		}
		return m, nil
	}
	return m, nil
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
			Foreground(theme.TextSecondary).
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

// renderVoiceBar renders a compact recording/transcribing bar at the bottom
// of the terminal panel. Shows a REC indicator + waveform on one line and
// the hint on the second line. During transcription, shows a spinner.
func (m Model) renderVoiceBar(width, height int) string {
	if height < 1 || width < 10 {
		return ""
	}

	var line1, line2 string
	if m.transcribing {
		line1 = lipgloss.NewStyle().
			Foreground(theme.AccentPurple).
			Bold(true).
			Render("⏳ Transcribing...")
	} else {
		recLabel := lipgloss.NewStyle().
			Foreground(theme.Red).
			Bold(true).
			Render("● REC")

		waveform := m.renderWaveform(width - 10) // leave room for "● REC " prefix
		line1 = recLabel + " " + waveform
		line2 = lipgloss.NewStyle().
			Foreground(theme.TextTertiary).
			Render("press v to stop  ·  esc to cancel")
	}

	content := lipgloss.JoinVertical(lipgloss.Left, line1, line2)

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		BorderTop(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(theme.Border).
		Padding(0, 1).
		Foreground(theme.TextPrimary).
		Render(content)
}

// renderWaveform renders amplitude levels as bar characters. Levels are
// normalized to the peak in the current window so the waveform shows relative
// dynamics (up and down) rather than a flat line.
func (m Model) renderWaveform(width int) string {
	bars := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

	// Find the peak in the current window for normalization.
	peak := 0.0
	for _, l := range m.audioLevels {
		if l > peak {
			peak = l
		}
	}
	// Floor: if everything is near-silent, show flatline at minimum bar.
	if peak < 0.0001 {
		peak = 0.0001
	}

	var sb strings.Builder
	for _, level := range m.audioLevels {
		normalized := level / peak
		if normalized > 1.0 {
			normalized = 1.0
		}

		idx := int(normalized * float64(len(bars)))
		if idx >= len(bars) {
			idx = len(bars) - 1
		}
		if idx < 0 {
			idx = 0
		}

		color := theme.SyncOK
		if normalized < 0.15 {
			color = theme.TextTertiary
		}
		sb.WriteString(lipgloss.NewStyle().Foreground(color).Render(string(bars[idx])))
	}
	// Pad with spaces to fill width
	result := sb.String()
	if w := lipgloss.Width(result); w < width {
		result += strings.Repeat(" ", width-w)
	}
	return result
}

// truncateID cuts a string to maxWidth columns, ending it with an ellipsis
// when something was cut.
func truncateID(id string, maxWidth int) string {
	if maxWidth < 1 {
		return ""
	}
	return ansi.Truncate(id, maxWidth, "…")
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

func (m Model) terminalWidth() int {
	w := m.width - SidebarWidthFor(m.width)
	if w < 1 {
		w = 1
	}
	return w
}
