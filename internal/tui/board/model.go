package board

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	svc       service.BoardService
	icons     theme.Icons
	columns   []string
	cards     map[string][]CardData
	cursorCol int
	cursorRow int
	width     int
	height    int
}

// New creates a new board model.
func New(svc service.BoardService, icons theme.Icons) Model {
	return Model{
		svc:   svc,
		icons: icons,
		cards: make(map[string][]CardData),
	}
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
		cards, err := m.svc.ListCards(ctx, col.Name)
		if err != nil {
			continue
		}
		cardData := make([]CardData, len(cards))
		for j, c := range cards {
			cardData[j] = CardData{
				Key:       c.ID,
				Title:     c.Title,
				Priority:  c.Priority,
				IssueType: c.IssueType,
				Provider:  c.Provider,
				Warning:   c.HasWarning,
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

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case DataLoadedMsg:
		m.columns = msg.columns
		m.cards = msg.cards
		m.clampRow()
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
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
	case "k":
		if m.cursorRow > 0 {
			m.cursorRow--
		}
	case "g":
		m.cursorRow = 0
	case "G":
		max := m.currentColumnCardCount() - 1
		if max >= 0 {
			m.cursorRow = max
		}
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
}

func (m Model) currentColumnCardCount() int {
	if m.cursorCol >= len(m.columns) {
		return 0
	}
	return len(m.cards[m.columns[m.cursorCol]])
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
		cards := m.cards[colName]
		active := i == m.cursorCol
		selectedIdx := -1
		if active {
			selectedIdx = m.cursorRow
		}
		col := RenderColumn(colName, cards, colWidth, active, selectedIdx, m.icons)
		rendered = append(rendered, col)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}
