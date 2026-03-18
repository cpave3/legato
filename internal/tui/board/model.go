package board

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/service"
)

const minColumnWidth = 20

// Model is the board Bubbletea model.
type Model struct {
	svc       service.BoardService
	columns   []string
	cards     map[string][]CardData
	cursorCol int
	cursorRow int
	width     int
	height    int
}

// New creates a new board model.
func New(svc service.BoardService) Model {
	return Model{
		svc:   svc,
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
				Summary:   c.Summary,
				Priority:  c.Priority,
				IssueType: c.IssueType,
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
		return m.handleKey(msg), nil
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) Model {
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
	case "1", "2", "3", "4", "5":
		idx := int(msg.String()[0]-'0') - 1
		if idx < len(m.columns) {
			m.cursorCol = idx
			m.clampRow()
		}
	}
	return m
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

	colWidth := m.width / len(m.columns)
	if colWidth < minColumnWidth {
		colWidth = minColumnWidth
	}

	// Determine visible columns (centered around cursor)
	visibleCount := m.width / colWidth
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

	var rendered []string
	for i := startCol; i < startCol+visibleCount; i++ {
		colName := m.columns[i]
		cards := m.cards[colName]
		active := i == m.cursorCol
		selectedIdx := -1
		if active {
			selectedIdx = m.cursorRow
		}
		col := RenderColumn(colName, cards, colWidth, active, selectedIdx)
		rendered = append(rendered, col)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}
