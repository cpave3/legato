package board

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/hotkey"
	"github.com/cpave3/legato/internal/tui/theme"
)

// DurationData holds aggregated state durations for a card.
type DurationData struct {
	Working time.Duration
	Waiting time.Duration
}

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
	// hideEmpty collapses a column with no cards to a narrow rail.
	hideEmpty bool
	// syncLabel and webLabel are shown at the end of the header as given.
	syncLabel string
	webLabel  string
	// preview is what the panel beside the board says about the selected
	// task beyond what its card holds.
	preview preview
	// spinner is the frame the mark of a running agent is drawn at.
	spinner int
}

// preview is the description of one task, read when the task is selected.
type preview struct {
	key         string
	description string
}

// New creates a new board model.
func New(svc service.BoardService, icons theme.Icons) Model {
	return Model{
		svc:           svc,
		icons:         icons,
		cards:         make(map[string][]CardData),
		workspaceView: store.WorkspaceView{Kind: store.ViewAll},
		hideEmpty:     true,
	}
}

// SetSpinner sets the frame the mark of a running agent is drawn at.
func (m *Model) SetSpinner(frame int) {
	m.spinner = frame
}

// HasWorkingAgent reports whether any card has a running agent, which is
// what there is to animate.
func (m Model) HasWorkingAgent() bool {
	for _, cards := range m.cards {
		for _, card := range cards {
			if card.AgentState == "working" {
				return true
			}
		}
	}
	return false
}

// liveIcons are the icons with the mark of a running agent at its frame.
func (m Model) liveIcons() theme.Icons {
	icons := m.icons
	icons.AgentWorking = theme.Spinner(m.spinner)
	return icons
}

// SetStatus sets what the header shows at its end: the sync state and the
// web server, each already worded.
func (m *Model) SetStatus(sync, web string) {
	m.syncLabel = sync
	m.webLabel = web
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

// OpenContextMsg signals the app to open the context menu for a card.
type OpenContextMsg struct {
	CardKey string
}

// OpenMoveMsg signals the app to open the move overlay for a card.
type OpenMoveMsg struct {
	CardKey string
}

// OpenMoveWorkspaceMsg signals the app to open workspace assignment for a card.
type OpenMoveWorkspaceMsg struct {
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

type RefreshPRMsg struct{ CardKey string }
type RediscoverPRMsg struct{ CardKey string }

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case DataLoadedMsg:
		m.columns = msg.columns
		m.cards = msg.cards
		m.computeMaxVisible()
		m.clampRow()
		// The selected task may have changed with the data.
		m.preview = preview{}
		m.syncPreview()
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.computeMaxVisible()
		m.syncScrollOffset()
		m.syncPreview()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	binding, ok := hotkey.DefaultRegistry().Resolve(msg, hotkey.Context{Page: hotkey.PageBoard, Capabilities: hotkey.NewCapabilities(hotkey.CapabilitySync, hotkey.CapabilityPR)})
	if !ok || binding.Target != hotkey.TargetPage {
		return m, nil
	}
	return m.HandleAction(binding.ID)
}

// HandleAction executes a registry action owned by the board model.
func (m Model) HandleAction(action hotkey.ActionID) (Model, tea.Cmd) {
	m, cmd := m.handleAction(action)
	m.syncPreview()
	return m, cmd
}

func (m Model) handleAction(action hotkey.ActionID) (Model, tea.Cmd) {
	switch action {
	case hotkey.ActionBoardToggleEmpty:
		m.hideEmpty = !m.hideEmpty
		m.computeMaxVisible()
		m.syncScrollOffset()
	case hotkey.ActionBoardLeft:
		m.stepColumn(-1)
	case hotkey.ActionBoardRight:
		m.stepColumn(1)
	case hotkey.ActionBoardDown:
		max := m.currentColumnCardCount() - 1
		if m.cursorRow < max {
			m.cursorRow++
		}
		m.syncScrollOffset()
	case hotkey.ActionBoardUp:
		if m.cursorRow > 0 {
			m.cursorRow--
		}
		m.syncScrollOffset()
	case hotkey.ActionBoardFirst:
		m.cursorRow = 0
		m.syncScrollOffset()
	case hotkey.ActionBoardLast:
		max := m.currentColumnCardCount() - 1
		if max >= 0 {
			m.cursorRow = max
		}
		m.syncScrollOffset()
	case hotkey.ActionBoardOpen:
		if card := m.SelectedCard(); card != nil {
			key := card.Key
			return m, func() tea.Msg { return OpenDetailMsg{CardKey: key} }
		}
	case hotkey.ActionBoardContext:
		if card := m.SelectedCard(); card != nil {
			key := card.Key
			return m, func() tea.Msg { return OpenContextMsg{CardKey: key} }
		}
	case hotkey.ActionBoardMove:
		if card := m.SelectedCard(); card != nil {
			key := card.Key
			return m, func() tea.Msg { return OpenMoveMsg{CardKey: key} }
		}
	case hotkey.ActionBoardMoveWorkspace:
		if card := m.SelectedCard(); card != nil {
			key := card.Key
			return m, func() tea.Msg { return OpenMoveWorkspaceMsg{CardKey: key} }
		}
	case hotkey.ActionBoardDelete:
		if card := m.SelectedCard(); card != nil {
			key := card.Key
			return m, func() tea.Msg { return OpenDeleteMsg{CardKey: key} }
		}
	case hotkey.ActionBoardImport:
		return m, func() tea.Msg { return OpenImportMsg{} }
	case hotkey.ActionBoardLinkPR:
		if card := m.SelectedCard(); card != nil {
			key := card.Key
			return m, func() tea.Msg { return OpenLinkPRMsg{CardKey: key} }
		}
	case hotkey.ActionBoardRefreshPR:
		if card := m.SelectedCard(); card != nil {
			key := card.Key
			return m, func() tea.Msg { return RefreshPRMsg{CardKey: key} }
		}
	case hotkey.ActionBoardRediscoverPR:
		if card := m.SelectedCard(); card != nil {
			key := card.Key
			return m, func() tea.Msg { return RediscoverPRMsg{CardKey: key} }
		}
	case hotkey.ActionBoardWorkspace:
		return m, func() tea.Msg { return OpenWorkspaceMsg{} }
	case hotkey.ActionBoardColumn1, hotkey.ActionBoardColumn2, hotkey.ActionBoardColumn3, hotkey.ActionBoardColumn4, hotkey.ActionBoardColumn5:
		idx := map[hotkey.ActionID]int{
			hotkey.ActionBoardColumn1: 0, hotkey.ActionBoardColumn2: 1, hotkey.ActionBoardColumn3: 2,
			hotkey.ActionBoardColumn4: 3, hotkey.ActionBoardColumn5: 4,
		}[action]
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
// States: "working", "waiting", "error", or "" (idle/no agent).
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
				m.syncPreview()
				return
			}
		}
	}
}

// stepColumn moves the cursor to the next open column in a direction. A
// collapsed column has nothing to select, so the cursor steps over it; it
// stays where it is when no open column lies that way.
func (m *Model) stepColumn(direction int) {
	for next := m.cursorCol + direction; next >= 0 && next < len(m.columns); next += direction {
		if !m.collapsed(next) {
			m.cursorCol = next
			m.clampRow()
			return
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
// It measures the tallest card on the board at the width columns have now.
func (m *Model) computeMaxVisible() {
	if m.height <= 0 {
		m.maxVisible = 0
		return
	}

	width := m.openColumnWidth()
	tallest := 0
	for _, colName := range m.columns {
		for _, card := range m.cards[colName] {
			if h := lipgloss.Height(RenderCard(card, width, false, colName, m.icons)); h > tallest {
				tallest = h
			}
		}
	}
	if tallest == 0 {
		// A card with a line for its agent stands in for a board with none.
		tallest = lipgloss.Height(RenderCard(CardData{Key: "sample", AgentActive: true}, width, false, "", m.icons))
	}

	// Cards share the line between them, so N of them take N*(tallest-1)+1.
	avail := m.height - headerHeight - columnsTopPadding - columnHeaderHeight
	m.maxVisible = (avail - 1) / (tallest - 1)
	if m.maxVisible < 1 {
		m.maxVisible = 1
	}
}
