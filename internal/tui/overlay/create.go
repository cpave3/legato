package overlay

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// CreateTaskMsg is sent when the user submits the create form.
type CreateTaskMsg struct {
	Title       string
	Description string
	Column      string
	Priority    string
}

// CreateCancelledMsg is sent when the user cancels task creation.
type CreateCancelledMsg struct{}

var priorities = []string{"", "Low", "Medium", "High"}

type focusField int

const (
	focusTitle focusField = iota
	focusColumn
	focusDescription
)

// CreateOverlay lets the user create a new task inline.
type CreateOverlay struct {
	title         string
	description   string
	focus         focusField
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
		focus:       focusTitle,
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
					Title:       m.title,
					Description: m.description,
					Column:      m.columns[m.columnIndex],
					Priority:    priorities[m.priorityIndex],
				}
				return m, func() tea.Msg { return result }
			}
			return m, nil
		case "tab":
			m.focus = (m.focus + 1) % 3
			return m, nil
		case "ctrl+p":
			m.priorityIndex = (m.priorityIndex + 1) % len(priorities)
			return m, nil
		case "ctrl+j":
			if m.focus == focusDescription {
				m.description += "\n"
			}
			return m, nil
		case "backspace":
			switch m.focus {
			case focusTitle:
				if len(m.title) > 0 {
					m.title = m.title[:len(m.title)-1]
				}
			case focusDescription:
				if len(m.description) > 0 {
					m.description = m.description[:len(m.description)-1]
				}
			}
			return m, nil
		default:
			switch m.focus {
			case focusTitle:
				if msg.Type == tea.KeySpace {
					m.title += " "
				} else if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
					m.title += string(msg.Runes)
				}
			case focusColumn:
				// h/l to cycle columns
				if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
					switch msg.Runes[0] {
					case 'l':
						m.columnIndex = (m.columnIndex + 1) % len(m.columns)
					case 'h':
						m.columnIndex = (m.columnIndex - 1 + len(m.columns)) % len(m.columns)
					}
				}
			case focusDescription:
				if msg.Type == tea.KeySpace {
					m.description += " "
				} else if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
					m.description += string(msg.Runes)
				}
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

	dimInputStyle := lipgloss.NewStyle().
		Foreground(theme.TextSecondary)

	valueStyle := lipgloss.NewStyle().
		Foreground(theme.TextSecondary)

	heading := titleStyle.Render("New Task")

	// Title input
	titleCursor := ""
	titleRender := dimInputStyle
	if m.focus == focusTitle {
		titleCursor = "█"
		titleRender = inputStyle
	}
	titleLine := labelStyle.Render("Title: ") + titleRender.Render(m.title+titleCursor)

	// Column selector
	var colParts []string
	for i, col := range m.columns {
		if i == m.columnIndex {
			colParts = append(colParts, inputStyle.Render("["+col+"]"))
		} else {
			colParts = append(colParts, valueStyle.Render(col))
		}
	}
	colLabel := labelStyle.Render("Column: ")
	if m.focus == focusColumn {
		colLabel = labelStyle.Render("Column: ")
	}
	columnLine := colLabel + strings.Join(colParts, valueStyle.Render(" · "))

	// Priority selector
	priLabel := priorities[m.priorityIndex]
	if priLabel == "" {
		priLabel = "none"
	}
	priorityLine := labelStyle.Render("Priority: ") + valueStyle.Render(priLabel)

	// Description input
	descCursor := ""
	descRender := dimInputStyle
	if m.focus == focusDescription {
		descCursor = "█"
		descRender = inputStyle
	}
	descContent := m.description + descCursor
	if descContent == "" {
		descContent = descCursor
	}
	descLine := labelStyle.Render("Description: ") + descRender.Render(descContent)

	// Hints
	hintStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
	hintText := "tab field · ctrl+p priority · enter submit · esc cancel"
	if m.focus == focusDescription {
		hintText = "tab field · ctrl+j newline · ctrl+p priority · enter submit · esc cancel"
	} else if m.focus == focusColumn {
		hintText = "h/l column · tab field · ctrl+p priority · enter submit · esc cancel"
	}
	hints := hintStyle.Render(hintText)

	content := lipgloss.JoinVertical(lipgloss.Left,
		heading, "", titleLine, "", columnLine, priorityLine, "", descLine, "", hints)
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
