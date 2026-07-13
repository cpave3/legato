package overlay

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// WorktreeSubmitMsg requests creation of a worktree for a task.
type WorktreeSubmitMsg struct {
	TaskID, PrimaryDir, Branch, BaseBranch string
}

type WorktreeCancelledMsg struct{}

type WorktreeOverlay struct {
	taskID        string
	values        [3]string
	focus         int
	width, height int
}

func NewWorktree(taskID, primaryDir, branch string) WorktreeOverlay {
	return WorktreeOverlay{taskID: taskID, values: [3]string{primaryDir, branch, "main"}}
}
func (m WorktreeOverlay) Init() tea.Cmd { return nil }
func (m WorktreeOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return WorktreeCancelledMsg{} }
		case "tab", "down":
			m.focus = (m.focus + 1) % 3
		case "shift+tab", "up":
			m.focus = (m.focus + 2) % 3
		case "backspace":
			if len(m.values[m.focus]) > 0 {
				m.values[m.focus] = m.values[m.focus][:len(m.values[m.focus])-1]
			}
		case "enter":
			if strings.TrimSpace(m.values[0]) == "" || strings.TrimSpace(m.values[1]) == "" || strings.TrimSpace(m.values[2]) == "" {
				return m, nil
			}
			return m, func() tea.Msg { return WorktreeSubmitMsg{m.taskID, m.values[0], m.values[1], m.values[2]} }
		default:
			if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
				m.values[m.focus] += string(msg.Runes)
				if msg.Type == tea.KeySpace {
					m.values[m.focus] += " "
				}
			}
		}
	}
	return m, nil
}
func (m WorktreeOverlay) View() string {
	labels := []string{"Primary directory", "Branch", "Base branch"}
	lines := []string{lipgloss.NewStyle().Bold(true).Foreground(theme.TextPrimary).Render("Create task worktree")}
	for i, label := range labels {
		marker := "  "
		if i == m.focus {
			marker = "> "
		}
		lines = append(lines, marker+label+": "+m.values[i])
	}
	lines = append(lines, "", lipgloss.NewStyle().Foreground(theme.TextTertiary).Render("tab: next  enter: create  esc: cancel"))
	return lipgloss.NewStyle().Padding(1, 2).Border(lipgloss.RoundedBorder()).Render(strings.Join(lines, "\n"))
}
