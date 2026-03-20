package overlay

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// CreateTaskMsg is sent when the user submits the create form.
type CreateTaskMsg struct {
	Title    string
	Column   string
	Priority string
}

// CreateCancelledMsg is sent when the user cancels task creation.
type CreateCancelledMsg struct{}

var priorities = []string{"", "Low", "Medium", "High"}

// CreateOverlay lets the user create a new task inline.
type CreateOverlay struct {
	title         string
	columns       []string
	columnIndex   int
	priorityIndex int
	width         int
	height        int
}

// NewCreate creates a new create-task overlay.
// currentColumn is pre-selected in the column selector.
func NewCreate(columns []string, currentColumn string) CreateOverlay {
	colIdx := 0
	for i, c := range columns {
		if c == currentColumn {
			colIdx = i
			break
		}
	}
	return CreateOverlay{
		columns:     columns,
		columnIndex: colIdx,
	}
}

// Init returns no command.
func (m CreateOverlay) Init() tea.Cmd {
	return nil
}

// Update handles messages.
func (m CreateOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return CreateCancelledMsg{} }
		case "enter":
			if m.title != "" {
				result := CreateTaskMsg{
					Title:    m.title,
					Column:   m.columns[m.columnIndex],
					Priority: priorities[m.priorityIndex],
				}
				return m, func() tea.Msg { return result }
			}
			return m, nil
		case "tab":
			m.columnIndex = (m.columnIndex + 1) % len(m.columns)
			return m, nil
		case "ctrl+p":
			m.priorityIndex = (m.priorityIndex + 1) % len(priorities)
			return m, nil
		case "backspace":
			if len(m.title) > 0 {
				m.title = m.title[:len(m.title)-1]
			}
			return m, nil
		default:
			if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
				m.title += string(msg.Runes)
			}
		}
	}
	return m, nil
}

// View renders the create overlay.
func (m CreateOverlay) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.TextPrimary)

	labelStyle := lipgloss.NewStyle().
		Foreground(theme.TextTertiary)

	inputStyle := lipgloss.NewStyle().
		Foreground(theme.AccentPurple).
		Bold(true)

	valueStyle := lipgloss.NewStyle().
		Foreground(theme.TextSecondary)

	heading := titleStyle.Render("New Task")

	// Title input
	titleLine := labelStyle.Render("Title: ") + inputStyle.Render(m.title+"█")

	// Column selector
	var colParts []string
	for i, col := range m.columns {
		if i == m.columnIndex {
			colParts = append(colParts, inputStyle.Render("["+col+"]"))
		} else {
			colParts = append(colParts, valueStyle.Render(col))
		}
	}
	columnLine := labelStyle.Render("Column: ") + strings.Join(colParts, valueStyle.Render(" · "))

	// Priority selector
	priLabel := priorities[m.priorityIndex]
	if priLabel == "" {
		priLabel = "none"
	}
	priorityLine := labelStyle.Render("Priority: ") + valueStyle.Render(priLabel)

	// Hints
	hintStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
	hints := hintStyle.Render(fmt.Sprintf("tab column · ctrl+p priority · enter submit · esc cancel"))

	content := lipgloss.JoinVertical(lipgloss.Left,
		heading, "", titleLine, "", columnLine, priorityLine, "", hints)
	return RenderPanel(content, m.width, m.height)
}

// Column returns the currently selected column.
func (m CreateOverlay) Column() string {
	return m.columns[m.columnIndex]
}

// Priority returns the currently selected priority.
func (m CreateOverlay) Priority() string {
	return priorities[m.priorityIndex]
}
