package overlay

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/hotkey"
	"github.com/cpave3/legato/internal/tui/theme"
)

// MoveSelectedMsg is sent when the user selects a target column.
type MoveSelectedMsg struct {
	TaskID       string
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
	taskID        string
	title         string
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
		taskID:        taskID,
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

// buildShortcuts assigns single-letter shortcuts to columns, keyed by column
// name: columns with the same name share one shortcut and choose that name.
func buildShortcuts(columns []string) map[rune]string {
	byName := func(i int) string { return columns[i] }
	return assignShortcuts(columns, byName, reservedIn(hotkey.Context{Overlay: hotkey.OverlayMove}))
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
		binding, fixed := hotkey.DefaultRegistry().Resolve(msg, hotkey.Context{Overlay: hotkey.OverlayMove})
		if fixed {
			switch binding.ID {
			case hotkey.ActionOverlayCancel:
				return m, func() tea.Msg { return MoveCancelledMsg{} }
			case hotkey.ActionOverlayDown:
				if m.cursor < len(m.columns)-1 {
					m.cursor++
				}
			case hotkey.ActionOverlayUp:
				if m.cursor > 0 {
					m.cursor--
				}
			case hotkey.ActionMoveWorkspace:
				id := m.taskID
				return m, func() tea.Msg { return OpenMoveWorkspaceMsg{TaskID: id} }
			case hotkey.ActionOverlaySelect:
				if m.cursor < len(m.columns) {
					target := m.columns[m.cursor]
					if target == m.currentColumn {
						return m, func() tea.Msg { return MoveCancelledMsg{} }
					}
					id := m.taskID
					return m, func() tea.Msg { return MoveSelectedMsg{TaskID: id, TargetColumn: target} }
				}
			}
		} else if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
			if col, ok := m.shortcuts[msg.Runes[0]]; ok {
				if col == m.currentColumn {
					return m, func() tea.Msg { return MoveCancelledMsg{} }
				}
				id := m.taskID
				return m, func() tea.Msg { return MoveSelectedMsg{TaskID: id, TargetColumn: col} }
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
		shortcut := shortcutLabel(m.shortcuts, col)
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
