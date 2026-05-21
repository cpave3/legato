package overlay

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// SwarmCancelConfirmedMsg is sent when the user confirms canceling a swarm.
type SwarmCancelConfirmedMsg struct {
	ParentTaskID string
}

// SwarmCancelCancelledMsg is sent when the user cancels the cancel action.
type SwarmCancelCancelledMsg struct{}

// SwarmCancelOverlay asks for confirmation before canceling a swarm.
type SwarmCancelOverlay struct {
	parentTaskID string
	title        string
	width        int
	height       int
}

// NewSwarmCancel creates a swarm cancel confirmation overlay.
func NewSwarmCancel(parentTaskID, title string) SwarmCancelOverlay {
	return SwarmCancelOverlay{
		parentTaskID: parentTaskID,
		title:        title,
	}
}

// Init returns no command.
func (m SwarmCancelOverlay) Init() tea.Cmd {
	return nil
}

// Update handles messages.
func (m SwarmCancelOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "y":
			id := m.parentTaskID
			return m, func() tea.Msg { return SwarmCancelConfirmedMsg{ParentTaskID: id} }
		case "n", "esc", "q":
			return m, func() tea.Msg { return SwarmCancelCancelledMsg{} }
		}
	}
	return m, nil
}

// View renders the overlay.
func (m SwarmCancelOverlay) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.TextPrimary).
		Padding(0, 1)

	bodyStyle := lipgloss.NewStyle().
		Foreground(theme.TextSecondary).
		Padding(0, 1)

	warnStyle := lipgloss.NewStyle().
		Foreground(theme.SyncError).
		Padding(0, 1)

	hintStyle := lipgloss.NewStyle().
		Foreground(theme.TextTertiary).
		Padding(0, 1)

	header := titleStyle.Render(fmt.Sprintf("Cancel swarm for %s?", m.title))

	lines := []string{
		header,
		"",
		bodyStyle.Render("Kills the conductor + all live workers, deletes all"),
		bodyStyle.Render("sub-tasks, and clears the swarm state on this card."),
		bodyStyle.Render("The existing card and its history remain."),
		"",
		warnStyle.Render("This action cannot be undone."),
		"",
		hintStyle.Render("y = confirm  n/esc = cancel"),
	}

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return RenderPanel(content, m.width, m.height)
}
