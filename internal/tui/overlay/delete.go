package overlay

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// DeleteConfirmedMsg is sent when the user confirms deletion.
type DeleteConfirmedMsg struct {
	TaskID string
}

// DeleteCancelledMsg is sent when the user cancels deletion.
type DeleteCancelledMsg struct{}

// DeleteOverlay asks for delete confirmation.
type DeleteOverlay struct {
	taskID   string
	title    string
	isRemote bool
	width    int
	height   int
}

// NewDelete creates a delete confirmation overlay.
func NewDelete(taskID, title string, isRemote bool) DeleteOverlay {
	return DeleteOverlay{
		taskID:   taskID,
		title:    title,
		isRemote: isRemote,
	}
}

// Init returns no command.
func (m DeleteOverlay) Init() tea.Cmd {
	return nil
}

// Update handles messages.
func (m DeleteOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "y":
			id := m.taskID
			return m, func() tea.Msg { return DeleteConfirmedMsg{TaskID: id} }
		case "n", "esc", "q":
			return m, func() tea.Msg { return DeleteCancelledMsg{} }
		}
	}
	return m, nil
}

// View renders the overlay.
func (m DeleteOverlay) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.TextPrimary).
		Padding(0, 1)

	subtitleStyle := lipgloss.NewStyle().
		Foreground(theme.TextSecondary).
		Padding(0, 1)

	warnStyle := lipgloss.NewStyle().
		Foreground(theme.SyncError).
		Padding(0, 1)

	hintStyle := lipgloss.NewStyle().
		Foreground(theme.TextTertiary).
		Padding(0, 1)

	header := titleStyle.Render(fmt.Sprintf("Delete %s", m.taskID))
	subtitle := subtitleStyle.Render(m.title)

	lines := []string{header, subtitle, ""}

	if m.isRemote {
		lines = append(lines, warnStyle.Render("This will remove the local reference only."))
		lines = append(lines, warnStyle.Render("The remote task will not be affected."))
		lines = append(lines, "")
	}

	lines = append(lines, hintStyle.Render("y = confirm  n/esc = cancel"))

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return RenderPanel(content, m.width, m.height)
}
