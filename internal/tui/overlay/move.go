package overlay

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// MoveSelectedMsg is sent when the user selects a target column.
type MoveSelectedMsg struct {
	TicketID     string
	TargetColumn string
}

// MoveCancelledMsg is sent when the user cancels the move.
type MoveCancelledMsg struct{}

// MoveOverlay lets the user pick a target column.
type MoveOverlay struct {
	ticketID      string
	columns       []string
	currentColumn string
	cursor        int
	width         int
	height        int
}

// NewMove creates a move overlay.
func NewMove(ticketID string, columns []string, currentColumn string) MoveOverlay {
	return MoveOverlay{
		ticketID:      ticketID,
		columns:       columns,
		currentColumn: currentColumn,
	}
}

// Init returns no command.
func (m MoveOverlay) Init() tea.Cmd {
	return nil
}

// Update handles messages.
func (m MoveOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q":
			return m, func() tea.Msg { return MoveCancelledMsg{} }
		case "j", "down":
			if m.cursor < len(m.columns)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "enter":
			if m.cursor < len(m.columns) {
				target := m.columns[m.cursor]
				id := m.ticketID
				return m, func() tea.Msg {
					return MoveSelectedMsg{TicketID: id, TargetColumn: target}
				}
			}
		}
	}
	return m, nil
}

// View renders the overlay.
func (m MoveOverlay) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.TextPrimary).
		Padding(0, 1)

	title := titleStyle.Render(fmt.Sprintf("Move %s to:", m.ticketID))

	var items []string
	for i, col := range m.columns {
		prefix := "  "
		style := lipgloss.NewStyle().Foreground(theme.TextSecondary)

		if i == m.cursor {
			prefix = "> "
			style = lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true)
		}

		label := col
		if col == m.currentColumn {
			label += " (current)"
			if i != m.cursor {
				style = style.Foreground(theme.TextTertiary)
			}
		}

		items = append(items, style.Render(prefix+label))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, append([]string{title, ""}, items...)...)

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.ColReview).
		Padding(1, 2)

	box := boxStyle.Render(content)

	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}
	return box
}

// TicketID returns the ticket being moved.
func (m MoveOverlay) TicketID() string {
	return m.ticketID
}
