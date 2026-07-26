package overlay

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
)

type ArchivedRestoreMsg struct{ CardID string }
type ArchivedCancelledMsg struct{}
type ArchivedOverlay struct {
	cards  []service.Card
	cursor int
}

func NewArchived(cards []service.Card) ArchivedOverlay { return ArchivedOverlay{cards: cards} }
func (m ArchivedOverlay) Init() tea.Cmd                { return nil }
func (m ArchivedOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			return m, func() tea.Msg { return ArchivedCancelledMsg{} }
		case "j", "down":
			if m.cursor < len(m.cards)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "enter", "r":
			if len(m.cards) > 0 {
				id := m.cards[m.cursor].ID
				return m, func() tea.Msg { return ArchivedRestoreMsg{CardID: id} }
			}
		}
	}
	return m, nil
}
func (m ArchivedOverlay) View() string {
	var out strings.Builder
	out.WriteString("Archived tasks\n\n")
	if len(m.cards) == 0 {
		out.WriteString("No archived tasks\n")
	}
	for i, card := range m.cards {
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}
		fmt.Fprintf(&out, "%s%s  %s  [%s]\n", cursor, card.ID, card.Title, card.Status)
	}
	out.WriteString("\nenter/r restore • esc close")
	return out.String()
}
