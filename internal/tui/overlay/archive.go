package overlay

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// ArchiveConfirmedMsg is sent when the user confirms archiving done cards.
type ArchiveConfirmedMsg struct{}

// ArchiveCancelledMsg is sent when the user cancels the archive action.
type ArchiveCancelledMsg struct{}

// ArchiveOverlay asks for archive confirmation.
type ArchiveOverlay struct {
	count  int
	width  int
	height int
}

// NewArchive creates an archive confirmation overlay.
func NewArchive(count int) ArchiveOverlay {
	return ArchiveOverlay{count: count}
}

// Init returns no command.
func (m ArchiveOverlay) Init() tea.Cmd {
	return nil
}

// Update handles messages.
func (m ArchiveOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "y":
			return m, func() tea.Msg { return ArchiveConfirmedMsg{} }
		case "n", "esc", "q":
			return m, func() tea.Msg { return ArchiveCancelledMsg{} }
		}
	}
	return m, nil
}

// View renders the overlay.
func (m ArchiveOverlay) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.TextPrimary).
		Padding(0, 1)

	subtitleStyle := lipgloss.NewStyle().
		Foreground(theme.TextSecondary).
		Padding(0, 1)

	hintStyle := lipgloss.NewStyle().
		Foreground(theme.TextTertiary).
		Padding(0, 1)

	cardWord := "cards"
	if m.count == 1 {
		cardWord = "card"
	}

	header := titleStyle.Render(fmt.Sprintf("Archive %d done %s?", m.count, cardWord))
	subtitle := subtitleStyle.Render("Archived cards are hidden from the board but kept in the database.")

	lines := []string{header, "", subtitle, "", hintStyle.Render("y = confirm  n/esc = cancel")}

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return RenderPanel(content, m.width, m.height)
}
