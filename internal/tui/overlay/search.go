package overlay

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

// SearchSelectedMsg is sent when the user selects a search result.
type SearchSelectedMsg struct {
	CardID string
}

// SearchCancelledMsg is sent when the user dismisses the search.
type SearchCancelledMsg struct{}

// SearchQueryChangedMsg is sent when the query text changes.
type SearchQueryChangedMsg struct {
	Query string
}

// SearchOverlay provides fuzzy search/filter over tickets.
type SearchOverlay struct {
	query   string
	results []service.Card
	cursor  int
	width   int
	height  int
}

// NewSearch creates a new search overlay.
func NewSearch(width, height int) SearchOverlay {
	return SearchOverlay{
		width:  width,
		height: height,
	}
}

// SetResults updates the displayed results (called after search completes).
func (m SearchOverlay) SetResults(results []service.Card) SearchOverlay {
	m.results = results
	if m.cursor >= len(results) {
		m.cursor = 0
	}
	return m
}

// Query returns the current search query.
func (m SearchOverlay) Query() string {
	return m.query
}

// Init returns no command.
func (m SearchOverlay) Init() tea.Cmd {
	return nil
}

// Update handles messages.
func (m SearchOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return SearchCancelledMsg{} }
		case "enter":
			if len(m.results) > 0 && m.cursor < len(m.results) {
				id := m.results[m.cursor].ID
				return m, func() tea.Msg { return SearchSelectedMsg{CardID: id} }
			}
			return m, nil
		case "j", "down":
			if m.cursor < len(m.results)-1 {
				m.cursor++
			}
			return m, nil
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case "backspace":
			if len(m.query) > 0 {
				m.query = m.query[:len(m.query)-1]
				q := m.query
				return m, func() tea.Msg { return SearchQueryChangedMsg{Query: q} }
			}
			return m, nil
		default:
			// Single printable rune → append to query
			if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
				m.query += string(msg.Runes)
				q := m.query
				return m, func() tea.Msg { return SearchQueryChangedMsg{Query: q} }
			}
		}
	}
	return m, nil
}

// View renders the search overlay.
func (m SearchOverlay) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.TextPrimary)

	inputStyle := lipgloss.NewStyle().
		Foreground(theme.AccentPurple).
		Bold(true)

	promptStyle := lipgloss.NewStyle().
		Foreground(theme.TextTertiary)

	title := titleStyle.Render("Search Tickets")
	input := promptStyle.Render("/") + " " + inputStyle.Render(m.query+"█")

	var lines []string
	lines = append(lines, title, input, "")

	if len(m.results) == 0 {
		if m.query != "" {
			lines = append(lines, lipgloss.NewStyle().Foreground(theme.TextTertiary).Render("No results"))
		}
	} else {
		for i, card := range m.results {
			prefix := "  "
			style := lipgloss.NewStyle().Foreground(theme.TextSecondary)
			if i == m.cursor {
				prefix = "> "
				style = lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true)
			}
			colStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
			line := fmt.Sprintf("%s%s  %s", prefix, card.ID,
				strings.TrimSpace(card.Title))
			lines = append(lines, style.Render(line)+"  "+colStyle.Render(card.Status))
		}
	}

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return RenderPanel(content, m.width, m.height)
}
