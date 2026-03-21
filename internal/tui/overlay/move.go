package overlay

import (
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// MoveSelectedMsg is sent when the user selects a target column.
type MoveSelectedMsg struct {
	TaskID     string
	TargetColumn string
}

// MoveCancelledMsg is sent when the user cancels the move.
type MoveCancelledMsg struct{}

// OpenMoveWorkspaceMsg is sent when the user presses w in the move overlay to switch workspace.
type OpenMoveWorkspaceMsg struct {
	TaskID string
}

// MoveOverlay lets the user pick a target column.
type MoveOverlay struct {
	taskID      string
	title       string
	columns       []string
	shortcuts     map[rune]string // shortcut key → column name
	currentColumn string
	cursor        int
	width         int
	height        int
}

// NewMove creates a move overlay.
func NewMove(taskID string, columns []string, currentColumn string) MoveOverlay {
	m := MoveOverlay{
		taskID:      taskID,
		columns:       columns,
		currentColumn: currentColumn,
		shortcuts:     buildShortcuts(columns),
	}
	return m
}

// WithTitle sets the ticket title for display.
func (m MoveOverlay) WithTitle(title string) MoveOverlay {
	m.title = title
	return m
}

// buildShortcuts assigns single-letter shortcuts to columns.
// Uses first letter of column name (lowercased). On conflict, falls back to number keys.
func buildShortcuts(columns []string) map[rune]string {
	shortcuts := make(map[rune]string)
	used := make(map[rune]bool)

	for _, col := range columns {
		if len(col) == 0 {
			continue
		}
		key := unicode.ToLower(rune(col[0]))
		if !used[key] {
			shortcuts[key] = col
			used[key] = true
		}
	}

	// Handle conflicts — assign number keys for unassigned columns
	num := '1'
	for _, col := range columns {
		assigned := false
		for _, v := range shortcuts {
			if v == col {
				assigned = true
				break
			}
		}
		if !assigned && num <= '9' {
			shortcuts[num] = col
			num++
		}
	}

	return shortcuts
}

// shortcutFor returns the shortcut key for a column name.
func (m MoveOverlay) shortcutFor(col string) string {
	for k, v := range m.shortcuts {
		if v == col {
			return string(k)
		}
	}
	return ""
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
		case "w":
			id := m.taskID
			return m, func() tea.Msg { return OpenMoveWorkspaceMsg{TaskID: id} }
		case "enter":
			if m.cursor < len(m.columns) {
				target := m.columns[m.cursor]
				if target == m.currentColumn {
					return m, func() tea.Msg { return MoveCancelledMsg{} }
				}
				id := m.taskID
				return m, func() tea.Msg {
					return MoveSelectedMsg{TaskID: id, TargetColumn: target}
				}
			}
		default:
			// Check shortcut keys
			if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
				r := msg.Runes[0]
				if col, ok := m.shortcuts[r]; ok {
					if col == m.currentColumn {
						return m, func() tea.Msg { return MoveCancelledMsg{} }
					}
					id := m.taskID
					return m, func() tea.Msg {
						return MoveSelectedMsg{TaskID: id, TargetColumn: col}
					}
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

	title := titleStyle.Render(fmt.Sprintf("Move %s", m.taskID))

	var header []string
	header = append(header, title)
	if m.title != "" {
		subtitleStyle := lipgloss.NewStyle().
			Foreground(theme.TextSecondary).
			Padding(0, 1)
		header = append(header, subtitleStyle.Render(m.title))
	}
	header = append(header, "")

	var items []string
	for i, col := range m.columns {
		shortcut := m.shortcutFor(col)
		style := lipgloss.NewStyle().Foreground(theme.TextSecondary)
		keyStyle := lipgloss.NewStyle().Foreground(theme.AccentPurple).Bold(true)

		if i == m.cursor {
			style = lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true)
		}

		if col == m.currentColumn {
			label := fmt.Sprintf("  %s  (current)", col)
			if i == m.cursor {
				label = "> " + strings.TrimLeft(label, " ")
			}
			items = append(items, lipgloss.NewStyle().Foreground(theme.TextTertiary).Render(label))
		} else {
			prefix := "  "
			if i == m.cursor {
				prefix = "> "
			}
			items = append(items, style.Render(prefix)+keyStyle.Render(shortcut)+"  "+style.Render(col))
		}
	}

	hintStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary).Padding(0, 1)
	hints := hintStyle.Render("w workspace · esc cancel")

	parts := append(header, items...)
	parts = append(parts, "", hints)
	content := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return RenderPanel(content, m.width, m.height)
}

// TaskID returns the ticket being moved.
func (m MoveOverlay) TaskID() string {
	return m.taskID
}
