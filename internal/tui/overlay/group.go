package overlay

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// GroupSelectedMsg is sent when the user selects or enters a group.
type GroupSelectedMsg struct {
	TaskID string
	Group  *string
}

// GroupCancelledMsg is sent when the user cancels group selection.
type GroupCancelledMsg struct{}

// GroupOverlay lets the user select an existing group or enter a new one.
type GroupOverlay struct {
	taskID  string
	options []string
	cursor  int
	custom  string
	editing bool
	width   int
	height  int
}

// NewGroup creates a group overlay. Options are expected to already include
// merged defaults and active groups.
func NewGroup(taskID, currentGroup string, options []string) GroupOverlay {
	merged := make([]string, 0, len(options)+1)
	merged = append(merged, "Ungrouped")
	cursor := 0
	for _, option := range options {
		if option == "" || option == "Ungrouped" {
			continue
		}
		merged = append(merged, option)
		if option == currentGroup {
			cursor = len(merged) - 1
		}
	}
	return GroupOverlay{taskID: taskID, options: merged, cursor: cursor}
}

func (m GroupOverlay) Init() tea.Cmd { return nil }

func (m GroupOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return GroupCancelledMsg{} }
		case "enter":
			if m.editing {
				group := strings.TrimSpace(m.custom)
				if group == "" {
					return m, nil
				}
				return m, selectedGroup(m.taskID, &group)
			}
			if m.cursor == 0 {
				return m, selectedGroup(m.taskID, nil)
			}
			group := m.options[m.cursor]
			return m, selectedGroup(m.taskID, &group)
		case "tab":
			m.editing = !m.editing
		case "up":
			if !m.editing && m.cursor > 0 {
				m.cursor--
			}
		case "down":
			if !m.editing && m.cursor < len(m.options)-1 {
				m.cursor++
			}
		case "backspace":
			if m.editing && len(m.custom) > 0 {
				m.custom = m.custom[:len(m.custom)-1]
			}
		default:
			if msg.Type == tea.KeySpace || msg.Type == tea.KeyRunes {
				m.editing = true
				if msg.Type == tea.KeySpace {
					m.custom += " "
				} else {
					m.custom += string(msg.Runes)
				}
			}
		}
	}
	return m, nil
}

func selectedGroup(taskID string, group *string) tea.Cmd {
	return func() tea.Msg { return GroupSelectedMsg{TaskID: taskID, Group: group} }
}

func (m GroupOverlay) View() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(theme.TextPrimary).Render("Set Group")
	items := make([]string, 0, len(m.options))
	for i, option := range m.options {
		prefix := "  "
		if !m.editing && i == m.cursor {
			prefix = "> "
		}
		items = append(items, prefix+option)
	}
	inputPrefix := "  "
	if m.editing {
		inputPrefix = "> "
	}
	input := inputPrefix + "Custom: " + m.custom
	if m.editing {
		input += "█"
	}
	hints := lipgloss.NewStyle().Foreground(theme.TextTertiary).Render("↑/↓ select · tab list/custom · type custom · enter apply · esc cancel")
	parts := append([]string{title, ""}, items...)
	parts = append(parts, "", input, "", hints)
	return RenderPanel(lipgloss.JoinVertical(lipgloss.Left, parts...), m.width, m.height)
}
