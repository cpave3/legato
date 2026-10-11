package board

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/tui/hotkey"
	"github.com/cpave3/legato/internal/tui/theme"
)

const (
	// minColumnWidth is the least width of an open column, and
	// maxColumnWidth the most it grows to on a wide terminal.
	minColumnWidth = 24
	maxColumnWidth = 72

	// headerHeight is the title bar and its rule.
	headerHeight = theme.TitleBarHeight
	// columnsTopPadding is the clear line between the header and the columns.
	columnsTopPadding = 1
	// columnHeaderHeight is a column's name and its rule.
	columnHeaderHeight = 2

	// panelMinBoardWidth is the narrowest board that still has a panel
	// beside it.
	panelMinBoardWidth = 100
	panelMinWidth      = 34
	panelMaxWidth      = 48
)

// slot is the place of one column in the row of columns.
type slot struct {
	column int
	rail   bool // collapsed to its name
	width  int
}

// anyCards reports whether any column has a card.
func (m Model) anyCards() bool {
	for _, cards := range m.cards {
		if len(cards) > 0 {
			return true
		}
	}
	return false
}

// collapsed reports whether a column is drawn as a rail: it has no cards and
// empty columns are hidden. A board with nothing on it keeps its columns
// open, since there is nothing to make room for.
func (m Model) collapsed(column int) bool {
	return m.hideEmpty && len(m.cards[m.columns[column]]) == 0 && m.anyCards()
}

// slots lays the columns out in width. When they do not all fit, the ones
// nearest the cursor are kept. The open columns share what the rails leave.
func (m Model) slots(width int) []slot {
	if len(m.columns) == 0 {
		return nil
	}
	least := func(column int) int {
		if m.collapsed(column) {
			return RailWidth
		}
		return minColumnWidth
	}
	cursor := m.cursorCol
	if cursor >= len(m.columns) {
		cursor = len(m.columns) - 1
	}

	first, last, used := cursor, cursor, least(cursor)
	for grew := true; grew; {
		grew = false
		if last+1 < len(m.columns) && used+least(last+1) <= width {
			last++
			used += least(last)
			grew = true
		}
		if first > 0 && used+least(first-1) <= width {
			first--
			used += least(first)
			grew = true
		}
	}

	open := 0
	for column := first; column <= last; column++ {
		if !m.collapsed(column) {
			open++
		}
	}
	spare := width - used
	if spare < 0 {
		spare = 0
	}
	slots := make([]slot, 0, last-first+1)
	for column := first; column <= last; column++ {
		if m.collapsed(column) {
			slots = append(slots, slot{column: column, rail: true, width: RailWidth})
			continue
		}
		share := spare / open
		spare -= share
		open--
		slots = append(slots, slot{column: column, width: min(minColumnWidth+share, maxColumnWidth)})
	}
	return slots
}

// panelWidth is the width of the panel beside the board, or 0 when the
// terminal is too narrow for one.
func (m Model) panelWidth() int {
	if m.width < panelMinBoardWidth {
		return 0
	}
	return min(max(m.width/4, panelMinWidth), panelMaxWidth)
}

// columnsWidth is the room the row of columns has, between its margins.
func (m Model) columnsWidth() int {
	return m.width - m.panelWidth() - 2
}

// openColumnWidth is the width of the narrowest open column, which is the
// one whose cards are tallest.
func (m Model) openColumnWidth() int {
	narrowest := maxColumnWidth
	for _, s := range m.slots(m.columnsWidth()) {
		if !s.rail && s.width < narrowest {
			narrowest = s.width
		}
	}
	return narrowest
}

// syncPreview reads the description of the selected task for the panel,
// once for each task that becomes selected.
func (m *Model) syncPreview() {
	if m.panelWidth() == 0 {
		return
	}
	card := m.SelectedCard()
	if card == nil {
		m.preview = preview{}
		return
	}
	if m.preview.key == card.Key {
		return
	}
	m.preview = preview{key: card.Key}
	if detail, err := m.svc.GetCard(context.Background(), card.Key); err == nil && detail != nil {
		m.preview.description = detail.DescriptionMD
	}
}

// View renders the board: its title bar, the columns, and beside them a
// panel about the selected task.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	bodyHeight := m.height - headerHeight
	if bodyHeight < 1 {
		return theme.Canvas(m.renderHeader(), m.width, m.height, theme.Background)
	}

	panelWidth := m.panelWidth()
	body := m.renderColumns(m.width-panelWidth, bodyHeight)
	if panelWidth > 0 {
		body = lipgloss.JoinHorizontal(lipgloss.Top, body, m.renderPanel(panelWidth, bodyHeight))
	}
	return m.renderHeader() + "\n" + body
}

func (m Model) renderHeader() string {
	tasks, withAgents := 0, 0
	for _, cards := range m.cards {
		tasks += len(cards)
		for _, card := range cards {
			if card.AgentActive {
				withAgents++
			}
		}
	}
	facts := []string{colored(theme.TextSecondary, countOf(tasks, "task", "tasks"))}
	switch {
	case withAgents == 1:
		facts = append(facts, colored(theme.Green, "1 with an agent"))
	case withAgents > 1:
		facts = append(facts, colored(theme.Green, fmt.Sprintf("%d with agents", withAgents)))
	}

	workspace, workspaceColor := m.workspaceLabel()
	hide := "hide empty: off"
	if m.hideEmpty {
		hide = "hide empty: on"
	}
	filter := colored(theme.TextSecondary, "filter") + " " +
		theme.Chip(workspace, workspaceColor) + " " + theme.Chip(hide, theme.TextSecondary)

	trailing := []string{filter}
	if m.syncLabel != "" {
		trailing = append(trailing, m.syncLabel)
	}
	if m.webLabel != "" {
		trailing = append(trailing, colored(theme.TextSecondary, m.webLabel))
	}
	return theme.TitleBar(m.width, "board", facts, trailing)
}

// workspaceLabel names what the board is filtered to.
func (m Model) workspaceLabel() (string, lipgloss.Color) {
	switch m.workspaceView.Kind {
	case store.ViewUnassigned:
		return "unassigned", theme.TextPrimary
	case store.ViewWorkspace:
		for _, ws := range m.workspaces {
			if ws.ID == m.workspaceView.WorkspaceID {
				if ws.Color != "" {
					return ws.Name, lipgloss.Color(ws.Color)
				}
				return ws.Name, theme.TextPrimary
			}
		}
		return "workspace", theme.TextPrimary
	default:
		return "all workspaces", theme.TextPrimary
	}
}

func countOf(count int, one, many string) string {
	if count == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", count, many)
}

// renderColumns renders the row of columns in an area of the given size.
func (m Model) renderColumns(width, height int) string {
	columnsHeight := height - columnsTopPadding
	var blocks []string
	for _, s := range m.slots(width - 2) {
		name := m.columns[s.column]
		active := s.column == m.cursorCol
		if s.rail {
			blocks = append(blocks, RenderRail(name, 0, active, columnsHeight))
			continue
		}

		all := m.cards[name]
		cards, selectedIdx := all, -1
		if active {
			selectedIdx = m.cursorRow
		}
		if m.maxVisible > 0 && len(all) > m.maxVisible {
			if active {
				// The window follows the cursor in the active column.
				cards = all[m.rowOffset : m.rowOffset+m.maxVisible]
				selectedIdx = m.cursorRow - m.rowOffset
			} else {
				cards = all[:m.maxVisible]
			}
		}
		blocks = append(blocks, RenderColumn(name, cards, s.width, active, selectedIdx, len(all), m.liveIcons()))
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top, blocks...)
	lines := strings.Split(row, "\n")
	for i, line := range lines {
		lines[i] = " " + line
	}
	top := strings.Repeat("\n", columnsTopPadding)
	return theme.Canvas(top+strings.Join(lines, "\n"), width, height, theme.Background)
}

// panelActions are the keys the panel names for the selected task.
var panelActions = []hotkey.ActionID{
	hotkey.ActionBoardOpen,
	hotkey.ActionBoardSpawnAgent,
	hotkey.ActionBoardMove,
	hotkey.ActionBoardContext,
}

// renderPanel renders the panel beside the board: what the selected task is,
// where it stands, the start of its description, and the keys that act on it.
func (m Model) renderPanel(width, height int) string {
	// The panel's edge, then two columns of margin at each side.
	inner := width - 5
	card := m.SelectedCard()

	var top []string
	if card == nil {
		top = []string{colored(theme.TextSecondary, "No task selected")}
	} else {
		top = m.panelTask(*card, inner)
	}

	var bottom []string
	if card != nil {
		bottom = append(bottom, theme.SectionLabel.Render("ACTIONS"))
		registry := hotkey.DefaultRegistry()
		for _, id := range panelActions {
			for _, binding := range registry.Bindings(hotkey.Context{Page: hotkey.PageBoard}, hotkey.FullHelp) {
				if binding.ID == id {
					key := lipgloss.NewStyle().Foreground(theme.Accent).Bold(true).Render(panelKey(binding))
					bottom = append(bottom, key+" "+colored(theme.TextPrimary, binding.Help))
				}
			}
		}
	}

	// The actions stay at the foot of the panel; what is above gives way.
	room := height - 2 - len(bottom)
	if len(bottom) > 0 {
		room--
	}
	if room < 0 {
		room = 0
	}
	if len(top) > room {
		top = top[:room]
	}
	lines := append([]string{""}, top...)
	for len(lines) < height-1-len(bottom) {
		lines = append(lines, "")
	}
	lines = append(lines, bottom...)

	edge := colored(theme.Border, "│")
	for i, line := range lines {
		lines[i] = edge + "  " + line
	}
	for len(lines) < height {
		lines = append(lines, edge)
	}
	return theme.Canvas(strings.Join(lines, "\n"), width, height, theme.Surface)
}

func panelKey(binding hotkey.Binding) string {
	if key := hotkey.DisplayKey(binding); key != "enter" {
		return key
	}
	return "↵"
}

// panelTask describes one task in lines no wider than width.
func (m Model) panelTask(card CardData, width int) []string {
	var kind []string
	if card.Priority != "" {
		kind = append(kind, colored(theme.PriorityColor(card.Priority), strings.ToLower(card.Priority)))
	}
	if card.IssueType != "" {
		kind = append(kind, colored(theme.TextSecondary, strings.ToLower(card.IssueType)))
	}
	kind = append(kind, colored(theme.TextSecondary, card.Key))
	lines := []string{priorityMark(card.Priority) + " " + strings.Join(kind, metaSeparator)}

	title := lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true)
	for _, line := range strings.Split(ansi.Wrap(card.Title, width, ""), "\n") {
		lines = append(lines, title.Render(line))
	}
	lines = append(lines, "")

	row := func(label, value string) {
		lines = append(lines, theme.Spread(colored(theme.TextSecondary, label), value, width))
	}
	if m.cursorCol < len(m.columns) {
		row("column", colored(theme.TextPrimary, strings.ToLower(m.columns[m.cursorCol])))
	}
	if card.WorkspaceName != "" {
		color := theme.TextPrimary
		if card.WorkspaceColor != "" {
			color = lipgloss.Color(card.WorkspaceColor)
		}
		row("workspace", colored(color, card.WorkspaceName))
	}
	row("source", sourceLabel(card, m.icons))
	if strip, ok := stripFor(card, m.liveIcons()); ok {
		agent := colored(theme.StateColor(strip.state), strip.mark) + " " + colored(theme.TextPrimary, strip.text)
		if strip.right != "" {
			agent += " " + colored(theme.TextSecondary, strip.right)
		}
		row("agent", agent)
	} else {
		row("agent", colored(theme.TextFaint, "none"))
	}
	if card.PRNumber > 0 {
		parts := append([]string{colored(theme.TextPrimary, fmt.Sprintf("#%d", card.PRNumber))}, pullRequestParts(card, m.icons)...)
		row("pull request", strings.Join(parts, metaSeparator))
	}
	if card.HasWorktree {
		row("worktree", colored(theme.Accent, m.icons.Worktree)+" "+colored(theme.TextPrimary, "yes"))
	}
	if card.SwarmStats.HasSwarm() {
		row("swarm", colored(theme.TextPrimary, fmt.Sprintf("%d of %d done", card.SwarmStats.Done, card.SwarmStats.Total)))
	}
	if history := historyLine(card, m.icons); history != "" {
		row("agent time", history)
	}

	lines = append(lines, colored(theme.Border, strings.Repeat("─", width)))
	description := describe(m.preview.description, width)
	if m.preview.key != card.Key || len(description) == 0 {
		return append(lines, colored(theme.TextFaint, "No description."))
	}
	for _, line := range description {
		color := theme.TextSecondary
		if line.heading {
			color = theme.TextPrimary
		}
		lines = append(lines, colored(color, line.text))
	}
	return lines
}

// descriptionLine is one line of a description as the panel shows it.
type descriptionLine struct {
	text    string
	heading bool
}

// describe turns the Markdown of a description into plain lines no wider
// than width: the text without its heading marks, code fences and blank
// lines.
func describe(markdown string, width int) []descriptionLine {
	var lines []descriptionLine
	for _, line := range strings.Split(markdown, "\n") {
		line = strings.TrimSpace(line)
		text := strings.TrimSpace(strings.TrimLeft(line, "#"))
		if text == "" || strings.HasPrefix(text, "```") {
			continue
		}
		for _, wrapped := range strings.Split(ansi.Wrap(text, width, ""), "\n") {
			lines = append(lines, descriptionLine{text: wrapped, heading: text != line})
		}
	}
	return lines
}
