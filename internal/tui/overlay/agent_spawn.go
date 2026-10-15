package overlay

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/cpave3/legato/internal/tui/theme"
)

// AgentSpawnSubmitMsg is sent when the user confirms the spawn.
type AgentSpawnSubmitMsg struct {
	TaskID    string // empty for ephemeral agents
	Title     string // used when TaskID is empty
	AgentKind string
	// WorkingDir is absolute when a listed directory or a suggestion was
	// picked, and the path as it was typed otherwise.
	WorkingDir string
	// Resources names the resources the agent is given.
	Resources []string
}

// AgentSpawnCancelledMsg is sent when the user cancels.
type AgentSpawnCancelledMsg struct{}

// AgentSpawnPathChangedMsg is sent when the typed path changes. The app
// answers with an AgentSpawnSuggestionsMsg for the same query.
type AgentSpawnPathChangedMsg struct{ Query string }

// AgentSpawnSuggestionsMsg carries the directories that complete Query.
type AgentSpawnSuggestionsMsg struct {
	Query string
	// Base is the directory that was searched, as it is displayed, and
	// Prefix the start of a name that was typed after it.
	Base   string
	Prefix string
	Items  []SpawnSuggestion
	// Total is how many directories matched; Items may hold fewer.
	Total int
}

// AgentSpawnSavePresetMsg asks the app to keep a directory as a preset. The
// app answers with an AgentSpawnPresetsMsg.
type AgentSpawnSavePresetMsg struct{ Path string }

// AgentSpawnPresetsMsg replaces the presets after one was saved, or says in
// Err why it was not.
type AgentSpawnPresetsMsg struct {
	Presets []SpawnDir
	Saved   string // the path that was saved
	Err     string
}

// SpawnDir is a directory the overlay lists by name. Display is Path as the
// person reads it, with the home directory written as a tilde.
type SpawnDir struct {
	Name    string
	Path    string
	Display string
}

// SpawnSuggestion is a directory that completes the typed path. Display ends
// in a separator, so that it can replace what was typed.
type SpawnSuggestion struct {
	Name    string
	Path    string
	Display string
	Git     bool
}

// SpawnResource is a resource the agent can be given: a named set of files
// and folders. Write is true when it gives any path to write.
type SpawnResource struct {
	Name  string
	Write bool
}

// spawnField tracks which input is active in the overlay.
type spawnField int

const (
	spawnFocusTitle spawnField = iota
	spawnFocusAgent
	spawnFocusWhere
	spawnFocusResources
)

// spawnFieldOrder is the order tab moves through the fields in.
var spawnFieldOrder = []spawnField{spawnFocusTitle, spawnFocusAgent, spawnFocusResources, spawnFocusWhere}

type spawnAgent struct{ label, value string }

// AgentSpawnOverlay lets the user configure and spawn an agent: a title, the
// agent tool, and where it starts. "Where" is a list of directories (the
// default one and the presets), or a path typed with suggestions.
type AgentSpawnOverlay struct {
	taskID     string // empty for ephemeral agents
	title      string
	agents     []spawnAgent
	agentIndex int
	focus      spawnField

	defaultDir SpawnDir
	rows       []SpawnDir
	// defaultRow is true when the first row is the default directory and not
	// a preset.
	defaultRow bool
	// row is the selected row; len(rows) is the "custom path" row.
	row int

	pathMode  bool
	path      string
	suggested AgentSpawnSuggestionsMsg
	// pick is the selected suggestion, or -1 for the path as it is typed.
	pick int

	resources []SpawnResource
	picked    map[string]bool
	// resource is the resource under the cursor.
	resource int

	canSave bool
	notice  string
	failed  bool

	width  int
	height int
}

// NewAgentSpawn creates an agent spawn overlay.
// adapters is the list of registered adapter names (excluding the default).
// defaultAdapter is the adapter used when none is chosen; it is offered first.
// defaultCWD is where the agent starts unless another directory is picked.
// taskID is empty for ephemeral agents; non-empty spawns an agent for an existing task.
// title pre-fills the title field; for ephemeral agents this is editable, for task-bound it is shown as context.
func NewAgentSpawn(adapters []string, defaultAdapter, defaultCWD, taskID, title string) AgentSpawnOverlay {
	first := spawnAgent{label: "default"}
	if defaultAdapter != "" {
		first.label = spawnAgentLabel(defaultAdapter)
	}
	options := []spawnAgent{first}
	for _, adapter := range adapters {
		if adapter != defaultAdapter {
			options = append(options, spawnAgent{label: spawnAgentLabel(adapter), value: adapter})
		}
	}
	options = append(options, spawnAgent{label: "shell", value: "shell"})

	focus := spawnFocusTitle
	if taskID != "" {
		// Task-bound: focus the agent selector since title is just context
		focus = spawnFocusAgent
	}

	m := AgentSpawnOverlay{taskID: taskID, title: title, agents: options, focus: focus, pick: -1}
	return m.WithLocations(SpawnDir{Path: defaultCWD}, nil)
}

// WithLocations sets where the agent starts by default and the presets
// listed after it. A default directory that is one of the presets is
// selected there and not listed twice.
func (m AgentSpawnOverlay) WithLocations(defaultDir SpawnDir, presets []SpawnDir) AgentSpawnOverlay {
	m.defaultDir = spawnDir(defaultDir)
	m.setPresets(presets, m.defaultDir.Path)
	m.pathMode = len(m.rows) == 0
	return m
}

// WithResources offers resources to give the agent. Without any, the
// overlay has no resources field.
func (m AgentSpawnOverlay) WithResources(resources []SpawnResource) AgentSpawnOverlay {
	m.resources = resources
	m.picked = map[string]bool{}
	m.resource = 0
	return m
}

// WithSaving lets a typed path be saved as a preset with ctrl+s.
func (m AgentSpawnOverlay) WithSaving() AgentSpawnOverlay {
	m.canSave = true
	return m
}

// setPresets rebuilds the rows and selects the one at selected.
func (m *AgentSpawnOverlay) setPresets(presets []SpawnDir, selected string) {
	m.rows, m.defaultRow = nil, false
	listed := false
	for _, preset := range presets {
		preset = spawnDir(preset)
		if preset.Path == "" {
			continue
		}
		listed = listed || preset.Path == m.defaultDir.Path
		m.rows = append(m.rows, preset)
	}
	if m.defaultDir.Path != "" && !listed {
		m.rows = append([]SpawnDir{m.defaultDir}, m.rows...)
		m.defaultRow = true
	}
	m.row = 0
	for i, row := range m.rows {
		if row.Path == selected {
			m.row = i
		}
	}
}

func spawnDir(dir SpawnDir) SpawnDir {
	if dir.Path == "" {
		return SpawnDir{}
	}
	if dir.Display == "" {
		dir.Display = dir.Path
	}
	if dir.Name == "" {
		dir.Name = filepath.Base(dir.Path)
	}
	return dir
}

// spawnAgentLabel is the short name an adapter is offered under.
func spawnAgentLabel(adapter string) string {
	if adapter == "claude-code" {
		return "claude"
	}
	return adapter
}

func (m AgentSpawnOverlay) Init() tea.Cmd { return nil }

func (m AgentSpawnOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case AgentSpawnSuggestionsMsg:
		if msg.Query != m.path {
			return m, nil
		}
		m.suggested = msg
		m.pick = -1
		if len(msg.Items) > 0 && !strings.HasSuffix(m.path, "/") {
			m.pick = 0
		}
		return m, nil

	case AgentSpawnPresetsMsg:
		if msg.Err != "" {
			m.notice, m.failed = msg.Err, true
			return m, nil
		}
		selected := msg.Saved
		if selected == "" && m.row < len(m.rows) {
			selected = m.rows[m.row].Path
		}
		m.setPresets(msg.Presets, selected)
		m.notice, m.failed = "", false
		if number := m.presetNumber(msg.Saved); number > 0 {
			m.notice = fmt.Sprintf("saved as preset %d", number)
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m AgentSpawnOverlay) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	typing := m.focus == spawnFocusWhere && m.pathMode
	switch msg.String() {
	case "esc":
		if m.pathMode && len(m.rows) > 0 {
			m.pathMode = false
			m.notice, m.failed = "", false
			return m, nil
		}
		return m, func() tea.Msg { return AgentSpawnCancelledMsg{} }
	case "enter":
		return m.submit()
	case "tab":
		if typing {
			return m.completePath()
		}
		m.focus = m.nextField(1)
		return m, nil
	case "shift+tab":
		m.focus = m.nextField(-1)
		return m, nil
	case "ctrl+s":
		if typing && m.offersSaving() {
			path := m.target().path
			return m, func() tea.Msg { return AgentSpawnSavePresetMsg{Path: path} }
		}
		return m, nil
	}

	switch m.focus {
	case spawnFocusTitle:
		if m.taskID == "" {
			m.title = editSpawnText(m.title, msg)
		}
	case spawnFocusAgent:
		m.moveAgent(msg.String())
	case spawnFocusResources:
		m.handleResourceKey(msg)
	case spawnFocusWhere:
		if m.pathMode {
			return m.handlePathKey(msg)
		}
		return m.handleListKey(msg)
	}
	return m, nil
}

// nextField moves the focus by step fields, around, past the title of a
// task-bound agent, which cannot be edited, and past resources when there
// are none to pick.
func (m AgentSpawnOverlay) nextField(step int) spawnField {
	at := 0
	for i, field := range spawnFieldOrder {
		if field == m.focus {
			at = i
		}
	}
	count := len(spawnFieldOrder)
	for {
		at = ((at+step)%count + count) % count
		focus := spawnFieldOrder[at]
		if focus == spawnFocusTitle && m.taskID != "" {
			continue
		}
		if focus == spawnFocusResources && len(m.resources) == 0 {
			continue
		}
		return focus
	}
}

// handleResourceKey moves between the resources and picks them.
func (m *AgentSpawnOverlay) handleResourceKey(msg tea.KeyMsg) {
	switch msg.String() {
	case "left", "h":
		if m.resource > 0 {
			m.resource--
		}
	case "right", "l":
		if m.resource < len(m.resources)-1 {
			m.resource++
		}
	case " ", "x":
		if m.resource < len(m.resources) {
			name := m.resources[m.resource].Name
			m.picked[name] = !m.picked[name]
		}
	}
}

// pickedResources names the picked resources, in the order they are offered.
func (m AgentSpawnOverlay) pickedResources() []string {
	var names []string
	for _, resource := range m.resources {
		if m.picked[resource.Name] {
			names = append(names, resource.Name)
		}
	}
	return names
}

func (m *AgentSpawnOverlay) moveAgent(key string) {
	switch key {
	case "left", "h":
		if m.agentIndex > 0 {
			m.agentIndex--
		}
	case "right", "l":
		if m.agentIndex < len(m.agents)-1 {
			m.agentIndex++
		}
	}
}

func (m AgentSpawnOverlay) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key := msg.String(); key {
	case "up", "k":
		if m.row > 0 {
			m.row--
		}
	case "down", "j":
		if m.row < len(m.rows) {
			m.row++
		}
	case "/", "~":
		return m.askForPath()
	case "left", "right", "h", "l":
		m.moveAgent(key)
	default:
		if number, err := strconv.Atoi(key); err == nil && number >= 1 && number <= len(m.rows) {
			m.row = number - 1
		}
	}
	return m, nil
}

func (m AgentSpawnOverlay) handlePathKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.items()
	switch msg.String() {
	case "up":
		lowest := 0
		if strings.HasSuffix(m.path, "/") {
			lowest = -1
		}
		if m.pick > lowest {
			m.pick--
		}
		return m, nil
	case "down":
		if m.pick < len(items)-1 {
			m.pick++
		}
		return m, nil
	case "ctrl+w":
		trimmed := strings.TrimSuffix(m.path, "/")
		cut := strings.LastIndex(trimmed, "/")
		if cut < 0 {
			return m, nil
		}
		return m.setPath(trimmed[:cut+1])
	}
	if edited := editSpawnText(m.path, msg); edited != m.path {
		return m.setPath(edited)
	}
	return m, nil
}

// editSpawnText applies a typing key to a line of text.
func editSpawnText(text string, msg tea.KeyMsg) string {
	switch msg.Type {
	case tea.KeyBackspace:
		if runes := []rune(text); len(runes) > 0 {
			return string(runes[:len(runes)-1])
		}
	case tea.KeySpace:
		return text + " "
	case tea.KeyRunes:
		return text + string(msg.Runes)
	}
	return text
}

// askForPath turns "where" into a typed path that starts inside the selected
// directory.
func (m AgentSpawnOverlay) askForPath() (tea.Model, tea.Cmd) {
	start := m.defaultDir.Display
	if m.row < len(m.rows) {
		start = m.rows[m.row].Display
	}
	if start == "" {
		start = "~"
	}
	if !strings.HasSuffix(start, "/") {
		start += "/"
	}
	m.focus = spawnFocusWhere
	m.pathMode = true
	return m.setPath(start)
}

func (m AgentSpawnOverlay) setPath(path string) (tea.Model, tea.Cmd) {
	m.path = path
	m.pick = -1
	m.notice, m.failed = "", false
	return m, func() tea.Msg { return AgentSpawnPathChangedMsg{Query: path} }
}

// completePath replaces the typed path with the picked suggestion, or the
// first one when none is picked.
func (m AgentSpawnOverlay) completePath() (tea.Model, tea.Cmd) {
	items := m.items()
	if len(items) == 0 {
		return m, nil
	}
	pick := m.pick
	if pick < 0 || pick >= len(items) {
		pick = 0
	}
	return m.setPath(items[pick].Display)
}

func (m AgentSpawnOverlay) submit() (tea.Model, tea.Cmd) {
	if !m.pathMode && m.row >= len(m.rows) {
		return m.askForPath()
	}
	title := m.title
	if m.taskID == "" && title == "" {
		title = "Ephemeral session"
	}
	submit := AgentSpawnSubmitMsg{
		TaskID:     m.taskID,
		Title:      title,
		AgentKind:  m.agents[m.agentIndex].value,
		WorkingDir: m.target().path,
		Resources:  m.pickedResources(),
	}
	return m, func() tea.Msg { return submit }
}

// items are the suggestions for the path as it is now.
func (m AgentSpawnOverlay) items() []SpawnSuggestion {
	if m.suggested.Query != m.path {
		return nil
	}
	return m.suggested.Items
}

type spawnTarget struct{ display, path string }

// target is the directory the agent would start in: the selected row, the
// picked suggestion, or the path as typed.
func (m AgentSpawnOverlay) target() spawnTarget {
	if !m.pathMode {
		if m.row < len(m.rows) {
			return spawnTarget{display: m.rows[m.row].Display, path: m.rows[m.row].Path}
		}
		return spawnTarget{}
	}
	if items := m.items(); m.pick >= 0 && m.pick < len(items) {
		return spawnTarget{display: items[m.pick].Display, path: items[m.pick].Path}
	}
	return spawnTarget{display: m.path, path: m.path}
}

// presetNumber is the number a directory is listed under, or 0 when it is
// not a preset.
func (m AgentSpawnOverlay) presetNumber(path string) int {
	if path == "" {
		return 0
	}
	typed := strings.TrimSuffix(path, "/")
	for i, row := range m.rows {
		if m.defaultRow && i == 0 {
			continue
		}
		if row.Path == path || row.Display == typed {
			return i + 1
		}
	}
	return 0
}

func (m AgentSpawnOverlay) offersSaving() bool {
	target := m.target()
	return m.canSave && strings.TrimSpace(target.path) != "" && m.presetNumber(target.path) == 0
}

const (
	spawnPanelWidth = 92
	spawnLabelWidth = 10
	// spawnFixedLines are the lines of the panel that are always drawn: the
	// header and its rule, title, agent, the rule above the summary, the
	// summary and the key hints.
	spawnFixedLines = 7
	spawnGapLines   = 4
)

func (m AgentSpawnOverlay) View() string {
	sized := m.width > 0 && m.height > 0
	boxWidth, whereLines, spaced := 80, 8, true
	fixed, gaps := spawnFixedLines, spawnGapLines
	if len(m.resources) > 0 {
		fixed, gaps = fixed+1, gaps+1
	}
	if sized {
		boxWidth = min(spawnPanelWidth, max(m.width-4, 24))
		room := m.height - 2 - fixed
		spaced = room-gaps >= 3
		if spaced {
			room -= gaps
		}
		whereLines = min(max(room, 1), 10)
	}
	inner := boxWidth - 2
	contentWidth := inner - 4
	valueWidth := contentWidth - spawnLabelWidth

	primary := lipgloss.NewStyle().Foreground(theme.TextPrimary)
	secondary := lipgloss.NewStyle().Foreground(theme.TextSecondary)
	rule := lipgloss.NewStyle().Foreground(theme.Border)
	line := func(text string) string { return theme.Fill("  "+text, inner, theme.Background) }
	field := func(name string, focus spawnField, value []string) []string {
		label := lipgloss.NewStyle().Width(spawnLabelWidth).Foreground(theme.TextSecondary).Render("  " + name)
		if m.focus == focus {
			label = lipgloss.NewStyle().Width(spawnLabelWidth).Foreground(theme.Accent).Render("› " + name)
		}
		lines := make([]string, len(value))
		for i, text := range value {
			lines[i] = line(label + text)
			label = strings.Repeat(" ", spawnLabelWidth)
		}
		return lines
	}

	context := "ephemeral"
	if m.taskID != "" {
		context = m.taskID
	}
	heading := lipgloss.NewStyle().Bold(true).Foreground(theme.TextPrimary).Render("Spawn agent")

	var lines []string
	gap := func() {
		if spaced {
			lines = append(lines, line(""))
		}
	}
	lines = append(lines,
		line(theme.Spread(heading, secondary.Render(context), contentWidth)),
		theme.Fill(rule.Render(strings.Repeat("─", inner)), inner, theme.Background),
	)
	gap()
	lines = append(lines, field("title", spawnFocusTitle, []string{m.titleValue(valueWidth)})...)
	gap()
	lines = append(lines, field("agent", spawnFocusAgent, []string{m.agentValue()})...)
	gap()
	if len(m.resources) > 0 {
		lines = append(lines, field("resources", spawnFocusResources, []string{m.resourcesValue(valueWidth)})...)
		gap()
	}
	where := m.listLines(valueWidth, whereLines)
	if m.pathMode {
		where = m.pathLines(valueWidth, whereLines)
	}
	lines = append(lines, field("where", spawnFocusWhere, where)...)
	gap()
	lines = append(lines,
		line(rule.Render(strings.Repeat("─", max(contentWidth, 0)))),
		line(m.summary(primary, secondary)),
		theme.Fill("  "+m.hints(contentWidth), inner, theme.Bar),
	)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Accent).
		BorderBackground(theme.Background).
		Render(strings.Join(lines, "\n"))
	if !sized {
		return box
	}
	// The panel sits on a darker ground than the page it covers.
	placed := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	return theme.Canvas(placed, m.width, m.height, theme.Inset)
}

func spawnCursor(under string) string {
	if under == "" {
		under = " "
	}
	return lipgloss.NewStyle().Background(theme.Accent).Foreground(theme.Background).Render(under)
}

func (m AgentSpawnOverlay) titleValue(width int) string {
	title := ansi.Truncate(m.title, max(width-1, 1), "…")
	switch {
	case m.taskID != "":
		return lipgloss.NewStyle().Foreground(theme.TextSecondary).Render(title)
	case m.focus == spawnFocusTitle:
		return lipgloss.NewStyle().Foreground(theme.TextPrimary).Render(title) + spawnCursor("")
	case title == "":
		return lipgloss.NewStyle().Foreground(theme.TextFaint).Render("Ephemeral session")
	}
	return lipgloss.NewStyle().Foreground(theme.TextPrimary).Render(title)
}

func (m AgentSpawnOverlay) agentValue() string {
	chips := make([]string, len(m.agents))
	for i, agent := range m.agents {
		style := lipgloss.NewStyle().Padding(0, 1).Foreground(theme.TextSecondary)
		if i == m.agentIndex {
			style = style.Background(theme.CardActive).Foreground(theme.TextPrimary)
			if m.focus == spawnFocusAgent {
				style = style.Background(theme.Accent).Foreground(theme.Background).Bold(true)
			}
		}
		chips[i] = style.Render(agent.label)
	}
	return strings.Join(chips, " ")
}

// resourcesValue draws the resources as boxes to tick, kept around the one
// under the cursor when they do not all fit.
func (m AgentSpawnOverlay) resourcesValue(width int) string {
	chips := make([]string, len(m.resources))
	for i, resource := range m.resources {
		box := "[ ]"
		if m.picked[resource.Name] {
			box = "[x]"
		}
		text := box + " " + resource.Name
		if resource.Write {
			text += " ✎"
		}
		style := lipgloss.NewStyle().Foreground(theme.TextSecondary)
		if m.picked[resource.Name] {
			style = style.Foreground(theme.TextPrimary)
		}
		if i == m.resource && m.focus == spawnFocusResources {
			style = style.Foreground(theme.Accent).Bold(true)
		}
		chips[i] = style.Render(text)
	}
	start := 0
	for start < m.resource && lipgloss.Width(strings.Join(chips[start:m.resource+1], "  ")) > width {
		start++
	}
	return ansi.Truncate(strings.Join(chips[start:], "  "), max(width, 1), "…")
}

// spawnWindow is the range of count rows that is shown when only room of
// them fit, kept around the selected one.
func spawnWindow(count, selected, room int) (int, int) {
	if count <= room {
		return 0, count
	}
	start := min(max(selected-room/2, 0), count-room)
	return start, start + room
}

// listLines draws the default directory and the presets, and the row that
// asks for a path.
func (m AgentSpawnOverlay) listLines(width, room int) []string {
	const customPath = "custom path…"
	nameWidth := lipgloss.Width(customPath)
	for _, row := range m.rows {
		nameWidth = max(nameWidth, lipgloss.Width(row.Name))
	}
	nameWidth = min(nameWidth, 24, max(width/3, 6))
	pathWidth := max(width-nameWidth-6, 0)

	faint := lipgloss.NewStyle().Foreground(theme.TextFaint)
	start, end := spawnWindow(len(m.rows)+1, m.row, room)
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		key, name, path := "/", customPath, ""
		nameStyle := lipgloss.NewStyle().Foreground(theme.TextSecondary)
		if i < len(m.rows) {
			key, name, path = " ", m.rows[i].Name, m.rows[i].Display
			if i < 9 {
				key = strconv.Itoa(i + 1)
			}
			nameStyle = nameStyle.Foreground(theme.TextPrimary)
		}
		marker, pathStyle, ground := "  ", lipgloss.NewStyle().Foreground(theme.TextSecondary), theme.Background
		if i == m.row {
			marker, ground = "▸ ", theme.CardActive
			nameStyle = nameStyle.Foreground(theme.Accent).Bold(true)
			pathStyle = pathStyle.Foreground(theme.TextPrimary)
		}
		name = marker + ansi.Truncate(name, nameWidth, "…")
		text := faint.Render(key) + " " + nameStyle.Width(nameWidth+3).Render(name) +
			pathStyle.Render(ansi.Truncate(path, pathWidth, "…"))
		lines = append(lines, theme.Fill(text, width, ground))
	}
	return lines
}

// pathLines draws the typed path, the directories that complete it, and how
// many matched.
func (m AgentSpawnOverlay) pathLines(width, room int) []string {
	primary := lipgloss.NewStyle().Foreground(theme.TextPrimary)
	faint := lipgloss.NewStyle().Foreground(theme.TextFaint)
	items := m.items()
	typing := m.focus == spawnFocusWhere

	typed := m.path
	if overflow := lipgloss.Width(typed) - max(width-4, 1); overflow > 0 {
		typed = "…" + string([]rune(typed)[overflow+1:])
	}
	input := " " + primary.Render(typed)
	if typing {
		ghost := []rune(m.ghost())
		if len(ghost) == 0 {
			input += spawnCursor("")
		} else {
			input += spawnCursor(string(ghost[:1])) + faint.Render(string(ghost[1:]))
		}
		if len(items) > 0 {
			input = theme.Spread(input, faint.Render("tab ⇥ completes "), width)
		}
	}
	lines := []string{theme.Fill(input, width, theme.CardActive)}

	shown := min(len(items), max(room-2, 0))
	start, end := spawnWindow(len(items), max(m.pick, 0), shown)
	for i := start; i < end; i++ {
		ground := theme.Inset
		if i == m.pick {
			ground = theme.CardActive
		}
		text := " " + theme.Spread(m.suggestionName(items[i]), m.suggestionMark(items[i])+" ", width-1)
		lines = append(lines, theme.Fill(text, width, ground))
	}

	if room >= 2 {
		count := ""
		switch {
		case m.suggested.Query != m.path:
		case len(items) == 0:
			count = "no directories match"
		case shown < m.suggested.Total:
			count = fmt.Sprintf("%d of %d matches · dirs only · git repos first", shown, m.suggested.Total)
		case shown == 1:
			count = "1 match · dirs only"
		default:
			count = fmt.Sprintf("%d matches · dirs only · git repos first", shown)
		}
		lines = append(lines, theme.Fill(" "+faint.Render(count), width, theme.Inset))
	}
	return lines
}

// ghost is what the picked suggestion adds to the typed path.
func (m AgentSpawnOverlay) ghost() string {
	items := m.items()
	if m.pick < 0 || m.pick >= len(items) {
		return ""
	}
	typed, full := []rune(m.path), []rune(items[m.pick].Display)
	if len(full) <= len(typed) || !strings.EqualFold(string(full[:len(typed)]), m.path) {
		return ""
	}
	return string(full[len(typed):])
}

// suggestionName writes a suggestion with the directory it is in set back
// and the part that was typed set forward.
func (m AgentSpawnOverlay) suggestionName(item SpawnSuggestion) string {
	primary := lipgloss.NewStyle().Foreground(theme.TextPrimary)
	base, name := m.suggested.Base, item.Display
	rest, inBase := strings.CutPrefix(name, base)
	if !inBase {
		return primary.Render(name)
	}
	typed := 0
	if prefix := []rune(m.suggested.Prefix); len(prefix) <= len([]rune(rest)) && strings.EqualFold(string([]rune(rest)[:len(prefix)]), m.suggested.Prefix) {
		typed = len(prefix)
	}
	runes := []rune(rest)
	return lipgloss.NewStyle().Foreground(theme.TextSecondary).Render(base) +
		lipgloss.NewStyle().Foreground(theme.Accent).Bold(true).Render(string(runes[:typed])) +
		primary.Render(string(runes[typed:]))
}

func (m AgentSpawnOverlay) suggestionMark(item SpawnSuggestion) string {
	var marks []string
	if item.Git {
		marks = append(marks, "git")
	}
	if number := m.presetNumber(item.Path); number > 0 {
		marks = append(marks, fmt.Sprintf("preset %d", number))
	}
	if len(marks) == 0 {
		return lipgloss.NewStyle().Foreground(theme.TextFaint).Render("dir")
	}
	return lipgloss.NewStyle().Foreground(theme.Green).Render(strings.Join(marks, " · "))
}

// summary says in one line what enter will do.
func (m AgentSpawnOverlay) summary(primary, secondary lipgloss.Style) string {
	arrow := secondary.Render("→ ")
	target := m.target()
	if strings.TrimSpace(target.display) == "" {
		if m.pathMode {
			return arrow + secondary.Render("type a path")
		}
		return arrow + secondary.Render("↵ to type a path")
	}
	text := arrow + secondary.Render(m.agents[m.agentIndex].label+" in ") + primary.Render(target.display)
	separator := lipgloss.NewStyle().Foreground(theme.TextFaint).Render(" · ")
	switch {
	case m.notice != "" && m.failed:
		text += separator + lipgloss.NewStyle().Foreground(theme.Red).Render(m.notice)
	case m.notice != "":
		text += separator + lipgloss.NewStyle().Foreground(theme.Green).Render(m.notice)
	case m.pathMode && m.offersSaving():
		text += separator + lipgloss.NewStyle().Foreground(theme.Yellow).Render("ctrl+s") + secondary.Render(" save as preset")
	case m.pathMode:
		if number := m.presetNumber(target.path); number > 0 {
			text += separator + secondary.Render(fmt.Sprintf("preset %d", number))
		}
	case m.taskID != "":
		text += separator + secondary.Render("for ") + primary.Render(m.taskID)
	default:
		title := m.title
		if title == "" {
			title = "Ephemeral session"
		}
		text += separator + secondary.Render("titled ") + primary.Render(title)
	}
	return text
}

// hints are the keys of the focused field, and at the far end the keys that
// end the dialog.
func (m AgentSpawnOverlay) hints(width int) string {
	key := lipgloss.NewStyle().Foreground(theme.TextPrimary)
	word := lipgloss.NewStyle().Foreground(theme.TextSecondary)
	join := func(pairs ...string) string {
		parts := make([]string, 0, len(pairs)/2)
		for i := 0; i+1 < len(pairs); i += 2 {
			parts = append(parts, key.Render(pairs[i])+word.Render(" "+pairs[i+1]))
		}
		return strings.Join(parts, word.Render(" · "))
	}
	spawn := lipgloss.NewStyle().Foreground(theme.Accent).Bold(true).Render("↵ spawn")
	end := spawn + word.Render(" · ") + key.Render("esc")

	var left string
	switch {
	case m.focus == spawnFocusWhere && m.pathMode:
		back := "cancel"
		if len(m.rows) > 0 {
			back = "back to presets"
		}
		left, end = join("tab", "complete", "↑↓", "pick", "ctrl+w", "up a dir", "esc", back), spawn
	case m.focus == spawnFocusWhere:
		presets := "↑↓"
		if count := min(len(m.rows), 9); count > 1 {
			presets = fmt.Sprintf("↑↓ 1-%d", count)
		}
		left = join("tab", "field", presets, "preset", "h/l", "agent", "/", "path")
	case m.focus == spawnFocusAgent:
		left = join("tab", "field", "h/l", "agent")
	case m.focus == spawnFocusResources:
		left = join("tab", "field", "h/l", "move", "space", "pick")
	default:
		left = join("tab", "field")
	}
	return theme.Spread(left, end, width)
}
