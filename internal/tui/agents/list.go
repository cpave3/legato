package agents

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

// section is one group of the agent list: what its agents are doing, in the
// order the list shows them.
type section int

const (
	sectionNeedsYou section = iota
	sectionRunning
	sectionBackground
	sectionIdle
	sectionEnded
)

var sectionLabels = map[section]string{
	sectionNeedsYou:   "NEEDS YOU",
	sectionRunning:    "RUNNING",
	sectionBackground: "BACKGROUND",
	sectionIdle:       "IDLE",
	sectionEnded:      "ENDED",
}

var sectionColors = map[section]lipgloss.Color{
	sectionNeedsYou:   theme.Amber,
	sectionRunning:    theme.Green,
	sectionBackground: theme.Cyan,
	sectionIdle:       theme.TextSecondary,
	sectionEnded:      theme.TextSecondary,
}

// sectionOf places an agent in the list. An agent that failed needs the
// person as much as one that asks.
func sectionOf(a service.AgentSession) section {
	switch {
	case a.Status == "dead":
		return sectionEnded
	case a.Activity == "waiting", a.Activity == "error":
		return sectionNeedsYou
	case a.Activity == "working":
		return sectionRunning
	case a.Activity == "background":
		return sectionBackground
	default:
		return sectionIdle
	}
}

// stateOf names what an agent is doing, in the theme's words.
func stateOf(a service.AgentSession) string {
	if a.Status == "dead" {
		return "dead"
	}
	return a.Activity
}

// stateWord is how a state is written out.
func stateWord(state string) string {
	switch state {
	case "working":
		return "running"
	case "waiting":
		return "needs you"
	case "background":
		return "background"
	case "error":
		return "error"
	case "dead":
		return "ended"
	default:
		return "idle"
	}
}

func colored(color lipgloss.Color, text string) string {
	return lipgloss.NewStyle().Foreground(color).Render(text)
}

// stateMark is the sign an agent is listed with.
func (m Model) stateMark(a service.AgentSession) string {
	switch stateOf(a) {
	case "working":
		return theme.Spinner(m.spinner)
	case "waiting":
		return m.icons.AgentWaiting
	case "background":
		return m.icons.AgentBackground
	case "error":
		return m.icons.Warning
	case "dead":
		return m.icons.CIFail
	default:
		return m.icons.AgentIdle
	}
}

// kindLabel names the tool an agent runs.
func kindLabel(a service.AgentSession) string {
	switch {
	case a.AgentKind == "claude-code":
		return "claude"
	case a.AgentKind != "":
		return a.AgentKind
	case a.Command != "":
		return a.Command
	default:
		return "shell"
	}
}

// stateDuration is the time that goes with an agent's state: how long it
// has waited when it waits, how long it has worked otherwise.
func (m Model) stateDuration(a service.AgentSession) string {
	if a.Activity == "waiting" {
		return formatDuration(m.durations[a.TaskID].Waiting)
	}
	return formatDuration(m.durations[a.TaskID].Working)
}

func countOf(count int, one, many string) string {
	if count == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", count, many)
}

// View renders the agent view: the list of agents on the left and the
// selected agent's terminal beside it. When the selected agent is a swarm
// worker, a third panel says what it was asked to do.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	if m.layout == LayoutGrid {
		return m.renderEnsemble()
	}

	sidebar := m.renderSidebar()
	rest := m.terminalWidth()

	a := m.SelectedAgent()
	if a != nil && a.ParentTaskID != "" && a.Role != "conductor" && m.showWorkerDetails && rest >= 60 {
		// The terminal has six tenths of what the list leaves.
		termWidth := rest * 6 / 10
		return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, m.renderMain(termWidth), m.renderWorkerDetailsPanel(rest-termWidth))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, m.renderMain(rest))
}

// renderSidebar renders the agent list: the brand and the count, the agents
// under what they are doing, and at the foot the status it was given.
func (m Model) renderSidebar() string {
	width := SidebarWidthFor(m.width)
	inner := width - 1 // the edge
	text := inner - 4  // two columns of margin at each side

	head := []string{"", "  " + theme.Spread(theme.Brand(), colored(theme.TextSecondary, countOf(len(m.agents), "agent", "agents")), text), ""}
	var foot []string
	if m.usage != nil {
		// The total, and under it the two parts it is made of.
		row := func(label string, tokens int64, labelColor, valueColor lipgloss.Color) {
			foot = append(foot, "  "+theme.Spread(colored(labelColor, label), colored(valueColor, formatTokens(tokens)), text))
		}
		row("tokens today", m.usage.TokensToday, theme.TextSecondary, theme.TextPrimary)
		if m.usage.hasParts() {
			row("  ↑ in", m.usage.InputToday, theme.TextFaint, theme.TextSecondary)
			row("  ↓ out", m.usage.OutputToday, theme.TextFaint, theme.TextSecondary)
		}
	}
	if m.syncLabel != "" || m.webLabel != "" {
		foot = append(foot, "  "+theme.Spread(m.syncLabel, colored(theme.TextSecondary, m.webLabel), text))
	}
	if len(foot) > 0 {
		foot = append(append([]string{""}, foot...), "")
	}
	listHeight := m.height - len(head) - len(foot)
	if listHeight < 1 {
		listHeight = 1
	}

	var list []string
	if len(m.agents) == 0 {
		list = []string{
			"  " + colored(theme.TextSecondary, "No agents"),
			"",
			"  " + colored(theme.TextFaint, "Press s to spawn"),
			"  " + colored(theme.TextFaint, "an agent session"),
		}
	} else {
		entries, selectedStart, selectedHeight := m.sidebarEntries(inner)
		list = windowAround(entries, selectedStart, selectedHeight, listHeight)
	}
	for len(list) < listHeight {
		list = append(list, "")
	}

	lines := append(append(head, list[:listHeight]...), foot...)
	edge := colored(theme.Border, "│")
	for i, line := range lines {
		lines[i] = theme.Fill(line, inner, theme.Surface) + edge
	}
	return theme.Canvas(strings.Join(lines, "\n"), width, m.height, theme.Surface)
}

// windowAround returns height lines of lines that hold the selected entry,
// which starts at start and is size lines tall, near their middle.
func windowAround(lines []string, start, size, height int) []string {
	if len(lines) <= height {
		return lines
	}
	offset := start - (height-size)/2
	if offset > len(lines)-height {
		offset = len(lines) - height
	}
	if offset < 0 {
		offset = 0
	}
	return lines[offset : offset+height]
}

// sidebarEntries renders the agents under their sections as lines width
// columns wide. It also says where the selected agent's lines are.
func (m Model) sidebarEntries(width int) (lines []string, selectedStart, selectedHeight int) {
	counts := map[section]int{}
	for _, a := range m.agents {
		counts[sectionOf(a)]++
	}

	current := section(-1)
	for i, a := range m.agents {
		if s := sectionOf(a); s != current {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, "  "+colored(sectionColors[s], fmt.Sprintf("%s · %d", sectionLabels[s], counts[s])))
			current = s
		}
		entry := m.renderSidebarEntry(a, i == m.selected, width)
		if i == m.selected {
			selectedStart, selectedHeight = len(lines), len(entry)
		}
		lines = append(lines, entry...)
	}
	return lines, selectedStart, selectedHeight
}

// renderSidebarEntry renders one agent of the list: its state and its ID with
// the time that goes with the state, then the tool it runs and its task with
// its recent activity. A swarm worker and a grouped agent add where they
// belong.
func (m Model) renderSidebarEntry(a service.AgentSession, selected bool, width int) []string {
	text := width - 4 // two columns of margin at each side
	quiet := sectionOf(a) == sectionIdle || sectionOf(a) == sectionEnded

	var note string
	switch state := stateOf(a); {
	case state == "dead" || state == "error":
		note = colored(theme.Red, stateWord(state))
	case state == "waiting":
		note = colored(theme.Amber, m.stateDuration(a))
	case quiet:
		note = colored(theme.TextFaint, m.stateDuration(a))
	default:
		note = colored(theme.TextSecondary, m.stateDuration(a))
	}

	bell := ""
	if m.ntfyConfigured && m.notifyStates[a.TaskID] {
		bell = " " + colored(theme.Accent, "🔔")
	}
	idWidth := text - 2 - lipgloss.Width(bell) - lipgloss.Width(note) - 1
	id := lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(selected).Render(truncateID(a.TaskID, idWidth))
	first := theme.Spread(colored(theme.StateColor(stateOf(a)), m.stateMark(a))+" "+id+bell, note, text)

	parts := []string{kindLabel(a)}
	if a.ParentTaskID != "" && a.Role != "" {
		parts = append(parts, a.Role)
	}
	if a.Title != "" {
		parts = append(parts, a.Title)
	}
	detailColor := theme.TextSecondary
	if quiet {
		detailColor = theme.TextFaint
	}
	activity := m.timelineBar(a.TaskID)
	detailWidth := text - 2
	if activity != "" {
		detailWidth -= lipgloss.Width(activity) + 1
	}
	detail := colored(detailColor, truncateID(strings.Join(parts, " · "), detailWidth))
	lines := []string{first, "  " + theme.Spread(detail, activity, text-2)}

	switch {
	case a.ParentTaskID != "" && a.Role != "conductor":
		lines = append(lines, "  "+colored(theme.Accent, "↳")+" "+colored(theme.TextFaint, truncateID("swarm "+a.ParentTaskID, text-4)))
	case a.ParentTaskID == "" && a.Group != "":
		lines = append(lines, "  "+colored(groupColor(a.Group), truncateID("● "+a.Group, text-2)))
	}

	// The selected agent is marked down its left side and lifted off the
	// panel.
	lead, bg := "  ", theme.Surface
	if selected {
		lead, bg = colored(theme.Accent, "▌")+" ", theme.CardActive
	}
	for i, line := range lines {
		lines[i] = theme.Fill(lead+line, width, bg)
	}
	return lines
}

// timelineBar draws an agent's recent activity, one cell for each stretch of
// time: filled in the color of what it did, hollow where it did nothing.
func (m Model) timelineBar(taskID string) string {
	var bar strings.Builder
	for _, state := range m.timelines[taskID] {
		switch state {
		case "working", "waiting", "error", "background":
			bar.WriteString(colored(theme.StateColor(state), "▰"))
		default:
			bar.WriteString(colored(theme.TextFaint, "▱"))
		}
	}
	return bar.String()
}

// renderMain renders the selected agent: who it is, what its terminal shows,
// which other agent needs the person, and how to talk to it.
func (m Model) renderMain(width int) string {
	header := m.renderHeader(width)
	if bar := m.renderShownBar(width); bar != "" {
		header += "\n" + bar
	}

	var footer []string
	if m.recording || m.transcribing {
		footer = append(footer, m.renderVoiceBar(width, 2))
	}
	if banner := m.renderBanner(width); banner != "" {
		footer = append(footer, banner)
	}
	footer = append(footer, m.renderPrompt(width))
	foot := strings.Join(footer, "\n")

	termHeight := m.height - lipgloss.Height(header) - lipgloss.Height(foot)
	if termHeight < 1 {
		return theme.Canvas(header+"\n"+foot, width, m.height, theme.Background)
	}
	terminal := theme.Canvas(m.renderTerminal(width, termHeight), width, termHeight, theme.Background)
	return theme.Canvas(header+"\n"+terminal+"\n"+foot, width, m.height, theme.Background)
}

// renderHeader renders the line over the terminal and its rule: the agent,
// the tool it runs and its task, and at the far end what it is doing.
func (m Model) renderHeader(width int) string {
	rule := colored(theme.Border, strings.Repeat("─", width))
	a := m.SelectedAgent()
	inner := width - 4
	if a == nil || inner < 1 {
		return "\n" + rule
	}

	state := stateOf(*a)
	status := colored(theme.StateColor(state), stateWord(state))
	if state == "" {
		status = colored(theme.TextSecondary, stateWord(state))
	}
	if duration := m.stateDuration(*a); duration != "" && state != "dead" && state != "error" {
		status += " " + colored(theme.TextSecondary, duration)
	}

	step := " " + colored(theme.TextFaint, "›") + " "
	trail := lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true).Render(a.TaskID) +
		step + colored(theme.TextSecondary, kindLabel(*a))
	if a.Title != "" {
		room := inner - lipgloss.Width(trail) - lipgloss.Width(step) - lipgloss.Width(status) - 2
		if room >= 4 {
			trail += step + colored(theme.TextSecondary, truncateID(a.Title, room))
		}
	}
	return "  " + theme.Spread(trail, status, inner) + "\n" + rule
}

// renderBanner renders the notice that an agent other than the selected one
// needs the person, or "" when none does.
func (m Model) renderBanner(width int) string {
	var waiting []service.AgentSession
	for i, a := range m.agents {
		if i != m.selected && sectionOf(a) == sectionNeedsYou {
			waiting = append(waiting, a)
		}
	}
	inner := width - 4
	if len(waiting) == 0 || inner < 20 {
		return ""
	}

	first := waiting[0]
	state := stateOf(first)
	what := stateWord(state)
	if state == "error" {
		what = "stopped on an error"
	}
	hint := colored(theme.Amber, "w") + colored(theme.TextSecondary, " jump")
	if more := len(waiting) - 1; more > 0 {
		hint = colored(theme.TextSecondary, fmt.Sprintf("%d more · ", more)) + hint
	}

	notice := colored(theme.StateColor(state), m.stateMark(first)+" "+first.TaskID) + " " + colored(theme.TextPrimary, what)
	if first.Title != "" {
		room := inner - 4 - lipgloss.Width(notice) - lipgloss.Width(hint) - 3
		if room >= 4 {
			notice += colored(theme.TextSecondary, " · "+truncateID(first.Title, room))
		}
	}
	return "  " + theme.Fill(" "+theme.Spread(notice, hint, inner-2)+" ", inner, theme.StateTint(state))
}

// renderPrompt renders the foot of the main panel: a rule, and how to talk
// to the selected agent.
func (m Model) renderPrompt(width int) string {
	inner := width - 4
	if inner < 1 {
		return ""
	}
	hint := "s spawns an agent"
	if a := m.SelectedAgent(); a != nil {
		switch {
		case a.Status == "dead":
			hint = "this session has ended · X removes it"
		default:
			hint = "↵ attaches to " + a.TaskID + " · m sends a macro"
			if m.voiceEnabled {
				hint += " · v dictates"
			}
		}
	}
	rule := colored(theme.Border, strings.Repeat("─", inner))
	return "  " + rule + "\n  " + colored(theme.Accent, "›") + " " + colored(theme.TextFaint, truncateID(hint, inner-2))
}

// renderWorkerDetailsPanel renders the selected swarm worker's role, scope,
// and instructions, so its purpose can be read beside its terminal.
func (m Model) renderWorkerDetailsPanel(width int) string {
	a := m.SelectedAgent()
	if a == nil || a.ParentTaskID == "" || width < 8 {
		return ""
	}
	inner := width - 5 // the edge, then two columns of margin at each side

	wrap := func(color lipgloss.Color, text string) []string {
		return strings.Split(lipgloss.NewStyle().Foreground(color).Width(inner).Render(text), "\n")
	}
	lines := []string{"", theme.SectionLabel.Render("WORKER"), ""}
	if a.Title != "" {
		lines = append(lines, wrap(theme.TextPrimary, a.Title)...)
		lines = append(lines, "")
	}
	if a.Role != "" && a.Role != "conductor" {
		lines = append(lines, theme.Spread(colored(theme.TextSecondary, "role"), colored(theme.TextPrimary, a.Role), inner))
	}
	if a.SubtaskID != "" {
		lines = append(lines, theme.Spread(colored(theme.TextSecondary, "subtask"), colored(theme.TextPrimary, a.SubtaskID), inner))
	}
	if len(a.Scope) > 0 {
		lines = append(lines, "", theme.SectionLabel.Render("SCOPE"))
		for _, glob := range a.Scope {
			lines = append(lines, wrap(theme.TextSecondary, glob)...)
		}
	}
	if instructions := firstNonEmpty(a.Prompt, a.Description); instructions != "" {
		lines = append(lines, "", theme.SectionLabel.Render("INSTRUCTIONS"))
		lines = append(lines, wrap(theme.TextSecondary, instructions)...)
	}

	edge := colored(theme.Border, "│")
	for i, line := range lines {
		lines[i] = edge + "  " + line
	}
	for len(lines) < m.height {
		lines = append(lines, edge)
	}
	return theme.Canvas(strings.Join(lines, "\n"), width, m.height, theme.Surface)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
