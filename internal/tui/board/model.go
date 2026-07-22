package board

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

// DurationData holds aggregated state durations for a card.
type DurationData struct {
	Working time.Duration
	Waiting time.Duration
}

const minColumnWidth = 20

// Model is the board Bubbletea model.
type Model struct {
	svc           service.BoardService
	icons         theme.Icons
	columns       []string
	cards         map[string][]CardData
	cursorCol     int
	cursorRow     int
	rowOffset     int // first visible card index in active column
	maxVisible    int // max cards that fit vertically in terminal
	width         int
	height        int
	workspaceView store.WorkspaceView
	workspaces    []service.Workspace
}

// New creates a new board model.
func New(svc service.BoardService, icons theme.Icons) Model {
	return Model{
		svc:           svc,
		icons:         icons,
		cards:         make(map[string][]CardData),
		workspaceView: store.WorkspaceView{Kind: store.ViewAll},
	}
}

// SetWorkspaceView sets the active workspace filter.
func (m *Model) SetWorkspaceView(view store.WorkspaceView) {
	m.workspaceView = view
}

// WorkspaceView returns the current workspace filter.
func (m Model) WorkspaceView() store.WorkspaceView {
	return m.workspaceView
}

// SetWorkspaces sets the available workspaces.
func (m *Model) SetWorkspaces(ws []service.Workspace) {
	m.workspaces = ws
}

// Workspaces returns the available workspaces.
func (m Model) Workspaces() []service.Workspace {
	return m.workspaces
}

// loadData fetches columns and cards from the service.
func (m Model) loadData() Model {
	ctx := context.Background()
	cols, err := m.svc.ListColumns(ctx)
	if err != nil {
		return m
	}

	m.columns = make([]string, len(cols))
	m.cards = make(map[string][]CardData)
	for i, col := range cols {
		m.columns[i] = col.Name
		cards, err := m.svc.ListCardsByWorkspace(ctx, col.Name, m.workspaceView)
		if err != nil {
			continue
		}
		cardData := make([]CardData, len(cards))
		for j, c := range cards {
			cardData[j] = CardData{
				Key:            c.ID,
				Title:          c.Title,
				Priority:       c.Priority,
				IssueType:      c.IssueType,
				Provider:       c.Provider,
				Warning:        c.HasWarning,
				HasWorktree:    c.HasWorktree,
				WorkspaceName:  c.WorkspaceName,
				WorkspaceColor: c.WorkspaceColor,
			}
		}
		m.cards[col.Name] = cardData
	}
	return m
}

// DataLoadedMsg carries loaded board data.
type DataLoadedMsg struct {
	columns []string
	cards   map[string][]CardData
}

// Init returns the initial command to load data.
func (m Model) Init() tea.Cmd {
	return func() tea.Msg {
		loaded := m.loadData()
		return DataLoadedMsg{columns: loaded.columns, cards: loaded.cards}
	}
}

// OpenDetailMsg signals the app to open the detail view for a card.
type OpenDetailMsg struct {
	CardKey string
}

// OpenMoveMsg signals the app to open the move overlay for a card.
type OpenMoveMsg struct {
	CardKey string
}

// OpenDeleteMsg signals the app to open the delete confirmation for a card.
type OpenDeleteMsg struct {
	CardKey string
}

// OpenImportMsg signals the app to open the remote import overlay.
type OpenImportMsg struct{}

// OpenWorkspaceMsg signals the app to open the workspace switcher overlay.
type OpenWorkspaceMsg struct{}

// OpenLinkPRMsg signals the app to open the link PR overlay.
type OpenLinkPRMsg struct {
	CardKey string
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case DataLoadedMsg:
		m.columns = msg.columns
		m.cards = msg.cards
		m.computeMaxVisible()
		m.clampRow()
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.computeMaxVisible()
		m.syncScrollOffset()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "h":
		if m.cursorCol > 0 {
			m.cursorCol--
			m.clampRow()
		}
	case "l":
		if m.cursorCol < len(m.columns)-1 {
			m.cursorCol++
			m.clampRow()
		}
	case "j":
		max := m.currentColumnCardCount() - 1
		if m.cursorRow < max {
			m.cursorRow++
		}
		m.syncScrollOffset()
	case "k":
		if m.cursorRow > 0 {
			m.cursorRow--
		}
		m.syncScrollOffset()
	case "g":
		m.cursorRow = 0
		m.syncScrollOffset()
	case "G":
		max := m.currentColumnCardCount() - 1
		if max >= 0 {
			m.cursorRow = max
		}
		m.syncScrollOffset()
	case "enter":
		if card := m.SelectedCard(); card != nil {
			key := card.Key
			return m, func() tea.Msg { return OpenDetailMsg{CardKey: key} }
		}
	case "m":
		if card := m.SelectedCard(); card != nil {
			key := card.Key
			return m, func() tea.Msg { return OpenMoveMsg{CardKey: key} }
		}
	case "d":
		if card := m.SelectedCard(); card != nil {
			key := card.Key
			return m, func() tea.Msg { return OpenDeleteMsg{CardKey: key} }
		}
	case "i":
		return m, func() tea.Msg { return OpenImportMsg{} }
	case "p":
		if card := m.SelectedCard(); card != nil {
			key := card.Key
			return m, func() tea.Msg { return OpenLinkPRMsg{CardKey: key} }
		}
	case "w":
		return m, func() tea.Msg { return OpenWorkspaceMsg{} }
	case "1", "2", "3", "4", "5":
		idx := int(msg.String()[0]-'0') - 1
		if idx < len(m.columns) {
			m.cursorCol = idx
			m.clampRow()
		}
	}
	return m, nil
}

// SelectedCard returns the currently selected card data, or nil if none.
func (m Model) SelectedCard() *CardData {
	if m.cursorCol >= len(m.columns) {
		return nil
	}
	col := m.columns[m.cursorCol]
	cards := m.cards[col]
	if m.cursorRow >= len(cards) {
		return nil
	}
	c := cards[m.cursorRow]
	return &c
}

// SetActiveAgents updates which task IDs have running agent sessions.
func (m *Model) SetActiveAgents(taskIDs map[string]bool) {
	for colName, cards := range m.cards {
		for i := range cards {
			cards[i].AgentActive = taskIDs[cards[i].Key]
		}
		m.cards[colName] = cards
	}
	m.computeMaxVisible()
}

// SetAgentStates updates the agent activity state for each card.
// States: "working", "waiting", or "" (idle/no agent).
func (m *Model) SetAgentStates(states map[string]string) {
	for colName, cards := range m.cards {
		for i := range cards {
			cards[i].AgentState = states[cards[i].Key]
		}
		m.cards[colName] = cards
	}
	m.computeMaxVisible()
}

// SetDurations updates the working/waiting durations for each card.
func (m *Model) SetDurations(durations map[string]DurationData) {
	for colName, cards := range m.cards {
		for i := range cards {
			if d, ok := durations[cards[i].Key]; ok {
				cards[i].WorkingDuration = d.Working
				cards[i].WaitingDuration = d.Waiting
			}
		}
		m.cards[colName] = cards
	}
	m.computeMaxVisible()
}

// PRStateData holds PR status for populating card data.
type PRStateData struct {
	CheckStatus    string
	ReviewDecision string
	CommentCount   int
	IsDraft        bool
	PRNumber       int
}

// SetSwarmStats populates SwarmStats per card.
func (m *Model) SetSwarmStats(stats map[string]SwarmStats) {
	for colName, cards := range m.cards {
		for i := range cards {
			if s, ok := stats[cards[i].Key]; ok {
				cards[i].SwarmStats = s
			}
		}
		m.cards[colName] = cards
	}
	m.computeMaxVisible()
}

// SetReviewStates updates review-tour badges for each card.
func (m *Model) SetReviewStates(states map[string]service.ReviewBadgeState) {
	for colName, cards := range m.cards {
		for i := range cards {
			state := states[cards[i].Key]
			cards[i].ReviewUnreviewed = state.Unreviewed
			cards[i].ReviewReady = state.Ready
		}
		m.cards[colName] = cards
	}
}

// SetReviewCounts preserves the count-only update used by older callers.
func (m *Model) SetReviewCounts(counts map[string]int) {
	states := make(map[string]service.ReviewBadgeState, len(counts))
	for taskID, count := range counts {
		states[taskID] = service.ReviewBadgeState{Unreviewed: count}
	}
	m.SetReviewStates(states)
}

// SetPRStates updates the PR status fields for each card.
func (m *Model) SetPRStates(states map[string]PRStateData) {
	for colName, cards := range m.cards {
		for i := range cards {
			if pr, ok := states[cards[i].Key]; ok {
				cards[i].PRCheckStatus = pr.CheckStatus
				cards[i].PRReviewDecision = pr.ReviewDecision
				cards[i].PRCommentCount = pr.CommentCount
				cards[i].PRIsDraft = pr.IsDraft
				cards[i].PRNumber = pr.PRNumber
			}
		}
		m.cards[colName] = cards
	}
	m.computeMaxVisible()
}

// TaskIDs returns all task IDs currently on the board.
func (m Model) TaskIDs() []string {
	var ids []string
	for _, colName := range m.columns {
		for _, card := range m.cards[colName] {
			ids = append(ids, card.Key)
		}
	}
	return ids
}

// NavigateTo moves the board cursor to the card with the given ID.
func (m *Model) NavigateTo(cardID string) {
	for colIdx, colName := range m.columns {
		for rowIdx, card := range m.cards[colName] {
			if card.Key == cardID {
				m.cursorCol = colIdx
				m.cursorRow = rowIdx
				m.syncScrollOffset()
				return
			}
		}
	}
}

func (m *Model) clampRow() {
	max := m.currentColumnCardCount() - 1
	if max < 0 {
		m.cursorRow = 0
	} else if m.cursorRow > max {
		m.cursorRow = max
	}
	m.syncScrollOffset()
}

func (m Model) currentColumnCardCount() int {
	if m.cursorCol >= len(m.columns) {
		return 0
	}
	return len(m.cards[m.columns[m.cursorCol]])
}

// syncScrollOffset keeps cursorRow inside the visible window [rowOffset, rowOffset+maxVisible-1].
func (m *Model) syncScrollOffset() {
	count := m.currentColumnCardCount()
	if count == 0 || m.maxVisible <= 0 {
		m.rowOffset = 0
		m.cursorRow = 0
		return
	}
	if count <= m.maxVisible {
		m.rowOffset = 0
		return
	}
	if m.cursorRow < m.rowOffset {
		m.rowOffset = m.cursorRow
	}
	if m.cursorRow >= m.rowOffset+m.maxVisible {
		m.rowOffset = m.cursorRow - m.maxVisible + 1
	}
	// Clamp offset to valid range so the window stays anchored at bottom.
	maxOffset := count - m.maxVisible
	if m.rowOffset > maxOffset {
		m.rowOffset = maxOffset
	}
	if m.rowOffset < 0 {
		m.rowOffset = 0
	}
}

// computeMaxVisible figures out how many cards fit vertically in the terminal.
// It measures the tallest rendered card actually on the board, and accounts
// for the newline separator between cards in the column output.
func (m *Model) computeMaxVisible() {
	if m.height <= 0 {
		m.maxVisible = 0
		return
	}

	// Measure the tallest card actually loaded on the board.
	// This is more accurate than a fabricated max-height sample.
	maxH := 0
	for _, colName := range m.columns {
		for i, card := range m.cards[colName] {
			selected := i == m.cursorRow && colName == m.columns[m.cursorCol]
			h := lipgloss.Height(RenderCard(card, 30, selected, colName, m.icons))
			if h > maxH {
				maxH = h
			}
		}
	}

	// Fallback to a representative card if the board is empty or all cards
	// returned zero height.
	if maxH <= 0 {
		sample := CardData{
			Key:              "SAMPLE",
			Title:            "Sample",
			Priority:         "High",
			IssueType:        "Bug",
			Provider:         "jira",
			AgentActive:      true,
			AgentState:       "working",
			WorkingDuration:  time.Hour,
			WaitingDuration:  time.Hour,
			PRCheckStatus:    "pass",
			PRReviewDecision: "APPROVED",
			PRCommentCount:   5,
			PRNumber:         1,
		}
		maxH = lipgloss.Height(RenderCard(sample, 30, true, "Doing", m.icons))
		if maxH <= 0 {
			maxH = 5
		}
	}

	// Each real card in RenderColumn is linked by a newline, so
	// a column of N cards takes N*maxH + (N-1) lines.
	headerH := 2 // top bar + separator
	avail := m.height - headerH
	if avail <= 0 {
		m.maxVisible = 1
		return
	}

	// Solve N*maxH + (N-1) <= avail  =>  N*(maxH+1) <= avail+1
	m.maxVisible = (avail + 1) / (maxH + 1)
	if m.maxVisible < 1 {
		m.maxVisible = 1
	}
}

// View renders the board.
func (m Model) View() string {
	if len(m.columns) == 0 || m.width == 0 {
		return ""
	}

	const colGap = 1

	// Account for gaps between columns in width calculation
	totalGaps := len(m.columns) - 1
	availWidth := m.width - (totalGaps * colGap)
	colWidth := availWidth / len(m.columns)
	if colWidth < minColumnWidth {
		colWidth = minColumnWidth
	}

	// Determine visible columns (centered around cursor)
	visibleCount := (m.width + colGap) / (colWidth + colGap)
	if visibleCount > len(m.columns) {
		visibleCount = len(m.columns)
	}
	if visibleCount == 0 {
		visibleCount = 1
	}

	startCol := m.cursorCol - visibleCount/2
	if startCol < 0 {
		startCol = 0
	}
	if startCol+visibleCount > len(m.columns) {
		startCol = len(m.columns) - visibleCount
	}

	gap := lipgloss.NewStyle().Width(colGap).Render(" ")

	var rendered []string
	for i := startCol; i < startCol+visibleCount; i++ {
		if i > startCol {
			rendered = append(rendered, gap)
		}
		colName := m.columns[i]
		allCards := m.cards[colName]
		active := i == m.cursorCol
		selectedIdx := -1
		if active {
			selectedIdx = m.cursorRow
		}
		var cards []CardData
		if active && m.maxVisible > 0 && len(allCards) > m.maxVisible {
			// Slice active column to visible window and adjust selectedIdx relative to the slice
			cards = allCards[m.rowOffset : m.rowOffset+m.maxVisible]
			selectedIdx = m.cursorRow - m.rowOffset
		} else if m.maxVisible > 0 && len(allCards) > m.maxVisible {
			// Inactive columns: cap to maxVisible from top so they don't blow out the layout
			cards = allCards[:m.maxVisible]
		} else {
			cards = allCards
		}
		col := RenderColumn(colName, cards, colWidth, active, selectedIdx, len(allCards), m.icons)
		rendered = append(rendered, col)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}
