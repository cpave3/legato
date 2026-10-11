package agents

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/hotkey"
	"github.com/cpave3/legato/internal/tui/theme"
)

// Layout is how the agents view shows its agents.
type Layout int

const (
	// LayoutList is the list of agents beside the selected one's terminal.
	LayoutList Layout = iota
	// LayoutGrid is the ensemble: every agent at once, each as a tile with
	// the end of what its terminal shows.
	LayoutGrid
)

const (
	// previewInterval is how often the ensemble asks what the agents are at.
	// A native agent answers with its whole conversation, which for a long
	// session is megabytes to read.
	previewInterval = 2 * time.Second
	// previewKept is how many lines of a terminal are kept for its tile.
	previewKept = 40
	// feedKept is how many events the activity feed remembers.
	feedKept = 100

	tileMinWidth  = 40
	tileMinHeight = 9
	tileMaxHeight = 20
	tileGap       = 2
	maxTileCols   = 4

	// roomForGap is how many lines a tile must have for its conversation
	// before it spends one on a gap above it.
	roomForGap = 9

	// activityMinWidth is the narrowest view that has the activity panel.
	activityMinWidth = 120
	activityWidth    = 38
)

// feedEvent is one change among the agents.
type feedEvent struct {
	at     time.Time
	taskID string
	state  string
	text   string
}

// Layout returns how the agents are shown.
func (m Model) Layout() Layout { return m.layout }

// HotkeyMode names the layout to the hotkey registry.
func (m Model) HotkeyMode() hotkey.Mode {
	if m.layout == LayoutGrid {
		return hotkey.ModeGrid
	}
	return hotkey.ModeList
}

// PreviewsDue reports whether it is time to ask what the terminals show.
// Only the ensemble needs it; the list shows one terminal, live.
func (m Model) PreviewsDue(now time.Time) bool {
	return m.layout == LayoutGrid && now.Sub(m.previewsAt) >= previewInterval
}

// PreviewsRequested records that the terminals were asked for at now.
func (m *Model) PreviewsRequested(now time.Time) {
	m.previewsAt = now
}

// previewLines keeps the end of what a terminal shows, as plain lines.
func previewLines(output string) []string {
	lines := strings.Split(ansi.Strip(output), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t\r")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > previewKept {
		lines = lines[len(lines)-previewKept:]
	}
	return lines
}

// observe adds to the feed what changed between the agents as they were last
// seen and as they are now. The first look records nothing: what was already
// there is not news.
func (m *Model) observe(agents []service.AgentSession) {
	states := make(map[string]string, len(agents))
	for _, a := range agents {
		states[a.TaskID] = stateOf(a)
	}
	if m.seen != nil {
		for _, a := range agents {
			state := states[a.TaskID]
			switch before, known := m.seen[a.TaskID]; {
			case !known:
				m.record(a.TaskID, state, "started")
			case before != state:
				m.record(a.TaskID, state, changeText(state))
			}
		}
		var closed []string
		for id := range m.seen {
			if _, still := states[id]; !still {
				closed = append(closed, id)
			}
		}
		sort.Strings(closed)
		for _, id := range closed {
			m.record(id, "dead", "closed")
		}
	}
	m.seen = states
}

func (m *Model) record(taskID, state, text string) {
	now := time.Now
	if m.now != nil {
		now = m.now
	}
	m.feed = append(m.feed, feedEvent{at: now(), taskID: taskID, state: state, text: text})
	if len(m.feed) > feedKept {
		m.feed = m.feed[len(m.feed)-feedKept:]
	}
}

// changeText says what an agent did when it came to a state.
func changeText(state string) string {
	switch state {
	case "working":
		return "started working"
	case "waiting":
		return "needs you"
	case "background":
		return "waits on background work"
	case "error":
		return "stopped on an error"
	case "dead":
		return "ended"
	default:
		return "went idle"
	}
}

// neighbour returns the index a movement key selects. In the list the agents
// are one below the other; in the ensemble they also stand side by side.
func (m Model) neighbour(action hotkey.ActionID) int {
	if len(m.agents) == 0 {
		return m.selected
	}
	next := m.selected
	if m.layout == LayoutList {
		switch action {
		case hotkey.ActionAgentsDown:
			next++
		case hotkey.ActionAgentsUp:
			next--
		}
	} else {
		cols := m.gridLayout().cols
		switch action {
		case hotkey.ActionAgentsDown:
			next += cols
		case hotkey.ActionAgentsUp:
			next -= cols
		case hotkey.ActionAgentsRight:
			if (m.selected+1)%cols != 0 {
				next++
			}
		case hotkey.ActionAgentsLeft:
			if m.selected%cols != 0 {
				next--
			}
		}
	}
	if next < 0 || next >= len(m.agents) {
		return m.selected
	}
	return next
}

// grid is the measure of the ensemble: how wide its two parts are and how
// its tiles are laid out.
type grid struct {
	width      int // of the tiles' area
	activity   int // of the activity panel; 0 without one
	cols       int
	rows       int // rows of tiles shown at once
	firstRow   int
	tileWidth  int
	tileHeight int
}

func (m Model) gridLayout() grid {
	g := grid{width: m.width}
	if m.width >= activityMinWidth {
		g.activity = activityWidth
		g.width -= g.activity
	}
	inner := g.width - 4 // two columns of margin at each side
	g.cols = min(max((inner+tileGap)/(tileMinWidth+tileGap), 1), maxTileCols)
	g.tileWidth = (inner - (g.cols-1)*tileGap) / g.cols

	// One line is clear above the tiles, and one between two rows of them.
	avail := m.height - theme.TitleBarHeight - 1
	tiles := len(m.agents) + 1 // and the tile that offers to spawn
	needed := (tiles + g.cols - 1) / g.cols
	g.rows = min(max((avail+1)/(tileMinHeight+1), 1), needed)
	g.tileHeight = min((avail-(g.rows-1))/g.rows, tileMaxHeight)
	// The tiles are shown a page of rows at a time, the page of the selected.
	g.firstRow = (m.selected / g.cols) / g.rows * g.rows
	return g
}

// renderEnsemble renders every agent at once: a title bar that counts them
// by what they are doing, a grid of tiles, and beside it what changed.
func (m Model) renderEnsemble() string {
	header := m.renderEnsembleHeader()
	bodyHeight := m.height - theme.TitleBarHeight
	if bodyHeight < 1 {
		return theme.Canvas(header, m.width, m.height, theme.Background)
	}
	g := m.gridLayout()
	body := m.renderTiles(g, bodyHeight)
	if g.activity > 0 {
		body = lipgloss.JoinHorizontal(lipgloss.Top, body, m.renderActivity(g.activity, bodyHeight))
	}
	return header + "\n" + body
}

func (m Model) renderEnsembleHeader() string {
	counts := map[section]int{}
	for _, a := range m.agents {
		counts[sectionOf(a)]++
	}
	var facts []string
	add := func(s section, mark, one, many string) {
		if count := counts[s]; count > 0 {
			facts = append(facts, colored(sectionColors[s], mark+" "+countOf(count, one, many)))
		}
	}
	add(sectionNeedsYou, m.icons.AgentWaiting, "needs you", "need you")
	add(sectionRunning, theme.Spinner(m.spinner), "running", "running")
	add(sectionBackground, m.icons.AgentBackground, "in background", "in background")
	add(sectionIdle, m.icons.AgentIdle, "idle", "idle")
	add(sectionEnded, m.icons.CIFail, "ended", "ended")
	if len(facts) == 0 {
		facts = []string{colored(theme.TextSecondary, "no agents")}
	}

	var trailing []string
	if m.usage != nil {
		tokens := colored(theme.TextSecondary, formatTokens(m.usage.TokensToday)+" tokens today")
		if m.usage.hasParts() {
			tokens += " " + colored(theme.TextFaint, "↑ "+formatTokens(m.usage.InputToday)+" ↓ "+formatTokens(m.usage.OutputToday))
		}
		trailing = append(trailing, tokens)
	}
	if m.syncLabel != "" {
		trailing = append(trailing, m.syncLabel)
	}
	if m.webLabel != "" {
		trailing = append(trailing, colored(theme.TextSecondary, m.webLabel))
	}
	return theme.TitleBar(m.width, "ensemble", facts, trailing)
}

// renderTiles renders the grid of tiles in an area height lines tall.
func (m Model) renderTiles(g grid, height int) string {
	if g.tileHeight < 3 || g.tileWidth < 12 {
		return theme.Canvas("", g.width, height, theme.Background)
	}
	gap := strings.Repeat(" ", tileGap)
	lines := []string{""}
	for row := g.firstRow; row < g.firstRow+g.rows; row++ {
		var tiles []string
		for col := 0; col < g.cols; col++ {
			index := row*g.cols + col
			switch {
			case index < len(m.agents):
				tiles = append(tiles, m.renderTile(m.agents[index], index == m.selected, g.tileWidth, g.tileHeight))
			case index == len(m.agents):
				tiles = append(tiles, renderSpawnTile(g.tileWidth, g.tileHeight))
			default:
				continue
			}
			if col < g.cols-1 {
				tiles = append(tiles, gap)
			}
		}
		if len(tiles) == 0 {
			break
		}
		if row > g.firstRow {
			lines = append(lines, "")
		}
		for _, line := range strings.Split(lipgloss.JoinHorizontal(lipgloss.Top, tiles...), "\n") {
			lines = append(lines, "  "+line)
		}
	}
	return theme.Canvas(strings.Join(lines, "\n"), g.width, height, theme.Background)
}

// renderTile renders one agent of the ensemble: who it is and what it is
// doing, its task, the end of what its terminal shows, and its recent
// activity with the time it has spent.
func (m Model) renderTile(a service.AgentSession, selected bool, width, height int) string {
	inner := width - 2
	state := stateOf(a)
	urgent := sectionOf(a) == sectionNeedsYou

	frameColor, headBg := theme.Border, theme.Card
	switch {
	case urgent:
		frameColor, headBg = theme.StateColor(state), theme.StateTint(state)
	case selected:
		headBg = theme.CardActive
	}
	if selected {
		frameColor = theme.Accent
	}

	label := strings.ToUpper(stateWord(state))
	if state == "working" {
		if duration := m.stateDuration(a); duration != "" {
			label = duration
		}
	}
	labelColor := theme.StateColor(state)
	if state == "" {
		labelColor = theme.TextSecondary
	}
	who := colored(theme.StateColor(state), m.stateMark(a)) + " " +
		lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true).Render(truncateID(a.TaskID, inner-lipgloss.Width(label)-len(kindLabel(a))-7)) +
		" " + colored(theme.TextSecondary, kindLabel(a))
	head := theme.Fill(" "+theme.Spread(who, colored(labelColor, label), inner-2)+" ", inner, headBg)

	parts := []string{}
	if a.ParentTaskID != "" && a.Role != "" {
		parts = append(parts, a.Role)
	}
	if a.Title != "" {
		parts = append(parts, a.Title)
	}
	task := strings.Join(parts, " · ")
	if task == "" {
		task = "no task"
	}

	// Inside the frame, the head, the task, the rule and the foot take five
	// lines. The rest shows what the agent is at: the end of its conversation
	// when its endpoint can be asked, and of its terminal otherwise.
	room := max(height-2-1-1-2, 0)
	body := []string{" " + colored(theme.TextSecondary, truncateID(task, inner-2))}
	tail, native := m.conversations[a.TaskID]
	var shown []string
	if native {
		// A tall tile sets the conversation off from the task with a line.
		lines := room
		if room >= roomForGap {
			body = append(body, "")
			lines--
		}
		shown = m.conversationLines(tail, state, inner-2, lines)
	} else {
		shown = m.paneLines(a.TaskID, state, inner-2, room)
	}
	for _, line := range shown {
		body = append(body, " "+line)
	}
	for len(body) < room+1 {
		body = append(body, "")
	}

	// The time an agent has spent is in its head; the foot has what the head
	// has no room for.
	activity, fill := m.timelineBar(a.TaskID), contextBar(tail)
	if lipgloss.Width(activity)+1+lipgloss.Width(fill) > inner-2 {
		activity = ""
	}
	rule := colored(theme.Border, strings.Repeat("─", inner))
	foot := " " + theme.Spread(activity, fill, inner-2)

	rows := append(append([]string{head}, body[:room+1]...), rule, foot)
	return frame(rows, inner, height-2, frameColor, "╭", "╮", "╰", "╯", "─", "│")
}

// paneLines renders the end of what an agent's terminal shows in at most room
// lines, the last one brighter than those before it.
func (m Model) paneLines(taskID, state string, width, room int) []string {
	preview := m.previews[taskID]
	if len(preview) > room {
		preview = preview[len(preview)-room:]
	}
	if len(preview) == 0 {
		if state == "dead" {
			return []string{colored(theme.TextFaint, "The session has ended.")}
		}
		return []string{colored(theme.TextFaint, "Nothing on its terminal yet.")}
	}
	lines := make([]string, len(preview))
	for i, line := range preview {
		color := theme.TextSecondary
		if i == len(preview)-1 {
			color = theme.TextPrimary
		}
		lines[i] = colored(color, truncateID(line, width))
	}
	return lines
}

// renderSpawnTile renders the tile after the last agent, which says how to
// add one.
func renderSpawnTile(width, height int) string {
	inner := width - 2
	lines := []string{
		colored(theme.TextSecondary, "+"),
		colored(theme.TextPrimary, "s") + colored(theme.TextFaint, " spawn agent"),
	}
	block := lipgloss.Place(inner, height-2, lipgloss.Center, lipgloss.Center, strings.Join(lines, "\n"))
	return frame(strings.Split(block, "\n"), inner, height-2, theme.Border, "╭", "╮", "╰", "╯", "┄", "┆")
}

// frame draws a border of one color around rows, which it pads or cuts to
// height lines of inner columns.
func frame(rows []string, inner, height int, color lipgloss.Color, topLeft, topRight, bottomLeft, bottomRight, horizontal, vertical string) string {
	edge := lipgloss.NewStyle().Foreground(color)
	side := edge.Render(vertical)
	out := []string{edge.Render(topLeft + strings.Repeat(horizontal, inner) + topRight)}
	for _, row := range strings.Split(theme.Canvas(strings.Join(rows, "\n"), inner, max(height, 1), theme.Background), "\n") {
		out = append(out, side+row+side)
	}
	return strings.Join(append(out, edge.Render(bottomLeft+strings.Repeat(horizontal, inner)+bottomRight)), "\n")
}

// renderActivity renders the panel beside the tiles: what changed among the
// agents, the newest first, and under it how many were at work lately.
func (m Model) renderActivity(width, height int) string {
	inner := width - 5 // the edge, then two columns of margin at each side

	lines := []string{"", theme.SectionLabel.Render("ACTIVITY"), ""}
	if len(m.feed) == 0 {
		lines = append(lines, colored(theme.TextFaint, "Nothing has changed yet."))
	}
	for i := len(m.feed) - 1; i >= 0; i-- {
		event := m.feed[i]
		idColor := theme.StateColor(event.state)
		if event.state == "" {
			idColor = theme.TextSecondary
		}
		at := colored(theme.TextFaint, event.at.Format("15:04")) + "  "
		room := inner - lipgloss.Width(at)
		id := truncateID(event.taskID, room)
		text := truncateID(event.text, room-lipgloss.Width(id)-1)
		lines = append(lines, at+colored(idColor, id)+" "+colored(theme.TextPrimary, text))
	}

	// Tokens are the better measure of work where they are known; without
	// them, how many agents were at work stands in.
	var foot []string
	if m.usage != nil && len(m.usage.Hourly) > 0 {
		hours := m.usage.Hourly
		if len(hours) > inner {
			hours = hours[len(hours)-inner:]
		}
		foot = []string{
			theme.SectionLabel.Render(fmt.Sprintf("TOKENS · LAST %dH", len(hours))),
			colored(theme.Accent, levelBar(hours)),
			colored(theme.TextFaint, "each hour, across agents"),
			"",
		}
	} else if work := m.workBar(inner); work != "" {
		foot = []string{
			theme.SectionLabel.Render("AGENTS AT WORK"),
			colored(theme.Accent, work),
			colored(theme.TextFaint, "recent activity, oldest first"),
			"",
		}
	}
	room := height - len(foot)
	if len(foot) > 0 {
		room--
	}
	if len(lines) > room {
		lines = lines[:max(room, 0)]
	}
	for len(lines) < height-len(foot) {
		lines = append(lines, "")
	}
	lines = append(lines, foot...)

	edge := colored(theme.Border, "│")
	for i, line := range lines {
		lines[i] = edge + "  " + line
	}
	return theme.Canvas(strings.Join(lines, "\n"), width, height, theme.Surface)
}

// workBar draws how many agents were at work in each recent stretch of time,
// or "" when no activity is known.
func (m Model) workBar(width int) string {
	var counts []int64
	for _, timeline := range m.timelines {
		for i, state := range timeline {
			for len(counts) <= i {
				counts = append(counts, 0)
			}
			if state == "working" {
				counts[i]++
			}
		}
	}
	if len(counts) == 0 {
		return ""
	}
	if len(counts) > width {
		counts = counts[len(counts)-width:]
	}
	return levelBar(counts)
}
