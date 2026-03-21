package overlay

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// TitleEditSubmitMsg is sent when the user confirms the title edit.
type TitleEditSubmitMsg struct {
	TaskID string
	Title  string
}

// TitleEditCancelledMsg is sent when the user cancels.
type TitleEditCancelledMsg struct{}

// TitleEditOverlay lets the user edit a task title inline.
type TitleEditOverlay struct {
	taskID string
	title  string
	width  int
	height int
}

// NewTitleEdit creates a title edit overlay pre-filled with the current title.
func NewTitleEdit(taskID, currentTitle string) TitleEditOverlay {
	return TitleEditOverlay{
		taskID: taskID,
		title:  currentTitle,
	}
}

// Init returns no command.
func (m TitleEditOverlay) Init() tea.Cmd {
	return nil
}

// Update handles messages.
func (m TitleEditOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return TitleEditCancelledMsg{} }
		case "enter":
			if m.title != "" {
				result := TitleEditSubmitMsg{
					TaskID: m.taskID,
					Title:  m.title,
				}
				return m, func() tea.Msg { return result }
			}
			return m, nil
		case "backspace":
			if len(m.title) > 0 {
				m.title = m.title[:len(m.title)-1]
			}
			return m, nil
		default:
			if msg.Type == tea.KeySpace {
				m.title += " "
			} else if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
				m.title += string(msg.Runes)
			}
		}
	}
	return m, nil
}

// View renders the title edit overlay.
func (m TitleEditOverlay) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.TextPrimary)

	labelStyle := lipgloss.NewStyle().
		Foreground(theme.TextTertiary)

	inputStyle := lipgloss.NewStyle().
		Foreground(theme.AccentPurple).
		Bold(true)

	hintStyle := lipgloss.NewStyle().
		Foreground(theme.TextTertiary)

	heading := titleStyle.Render("Edit Title")
	titleLine := labelStyle.Render("Title: ") + inputStyle.Render(m.title+"█")
	hints := hintStyle.Render("enter save · esc cancel")

	content := lipgloss.JoinVertical(lipgloss.Left, heading, "", titleLine, "", hints)
	return RenderPanel(content, m.width, m.height)
}
