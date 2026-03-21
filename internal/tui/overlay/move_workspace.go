package overlay

import (
	"fmt"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

// WorkspaceAssignedMsg is sent when the user assigns a task to a workspace.
type WorkspaceAssignedMsg struct {
	TaskID      string
	WorkspaceID *int
}

// WorkspaceAssignCancelledMsg is sent when the user cancels workspace assignment.
type WorkspaceAssignCancelledMsg struct{}

type mwEntry struct {
	id    *int
	label string
	color string
}

// MoveWorkspaceOverlay lets the user assign a task to a workspace.
type MoveWorkspaceOverlay struct {
	taskID    string
	entries   []mwEntry
	shortcuts map[rune]int
	currentID *int // current workspace_id of the task (nil = unassigned)
	cursor    int
	width     int
	height    int
}

// NewMoveWorkspace creates a workspace assignment overlay for a task.
func NewMoveWorkspace(taskID string, workspaces []service.Workspace, currentWorkspaceID *int) MoveWorkspaceOverlay {
	entries := []mwEntry{{id: nil, label: "None"}}
	for _, ws := range workspaces {
		id := ws.ID
		entries = append(entries, mwEntry{id: &id, label: ws.Name, color: ws.Color})
	}

	shortcuts := buildMWShortcuts(entries)

	// Set cursor to current workspace
	cursor := 0
	for i, e := range entries {
		if currentWorkspaceID == nil && e.id == nil {
			cursor = i
			break
		}
		if currentWorkspaceID != nil && e.id != nil && *currentWorkspaceID == *e.id {
			cursor = i
			break
		}
	}

	return MoveWorkspaceOverlay{
		taskID:    taskID,
		entries:   entries,
		shortcuts: shortcuts,
		currentID: currentWorkspaceID,
		cursor:    cursor,
	}
}

func buildMWShortcuts(entries []mwEntry) map[rune]int {
	shortcuts := make(map[rune]int)
	used := make(map[rune]bool)

	for i, e := range entries {
		if len(e.label) == 0 {
			continue
		}
		key := unicode.ToLower(rune(e.label[0]))
		if !used[key] {
			shortcuts[key] = i
			used[key] = true
		}
	}

	num := '1'
	for i := range entries {
		assigned := false
		for _, idx := range shortcuts {
			if idx == i {
				assigned = true
				break
			}
		}
		if !assigned && num <= '9' {
			shortcuts[num] = i
			num++
		}
	}

	return shortcuts
}

func (m MoveWorkspaceOverlay) shortcutFor(idx int) string {
	for k, v := range m.shortcuts {
		if v == idx {
			return string(k)
		}
	}
	return ""
}

func (m MoveWorkspaceOverlay) isCurrent(e mwEntry) bool {
	if m.currentID == nil && e.id == nil {
		return true
	}
	if m.currentID != nil && e.id != nil && *m.currentID == *e.id {
		return true
	}
	return false
}

func (m MoveWorkspaceOverlay) Init() tea.Cmd { return nil }

func (m MoveWorkspaceOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q":
			return m, func() tea.Msg { return WorkspaceAssignCancelledMsg{} }
		case "j", "down":
			if m.cursor < len(m.entries)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "enter":
			return m.selectCurrent()
		default:
			if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
				r := msg.Runes[0]
				if idx, ok := m.shortcuts[r]; ok {
					m.cursor = idx
					return m.selectCurrent()
				}
			}
		}
	}
	return m, nil
}

func (m MoveWorkspaceOverlay) selectCurrent() (tea.Model, tea.Cmd) {
	if m.cursor >= len(m.entries) {
		return m, nil
	}
	e := m.entries[m.cursor]
	if m.isCurrent(e) {
		return m, func() tea.Msg { return WorkspaceAssignCancelledMsg{} }
	}
	taskID := m.taskID
	wsID := e.id
	return m, func() tea.Msg { return WorkspaceAssignedMsg{TaskID: taskID, WorkspaceID: wsID} }
}

func (m MoveWorkspaceOverlay) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.TextPrimary).
		Padding(0, 1)

	title := titleStyle.Render(fmt.Sprintf("Move %s to workspace", m.taskID))
	content := []string{title, ""}

	for i, e := range m.entries {
		shortcut := m.shortcutFor(i)
		keyStyle := lipgloss.NewStyle().Foreground(theme.AccentPurple).Bold(true)
		labelStyle := lipgloss.NewStyle().Foreground(theme.TextSecondary)

		if i == m.cursor {
			labelStyle = lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true)
		}

		colorDot := ""
		if e.color != "" {
			dotStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(e.color))
			colorDot = dotStyle.Render("●") + " "
		}

		if m.isCurrent(e) {
			label := fmt.Sprintf("  %s%s  (current)", colorDot, e.label)
			if i == m.cursor {
				label = fmt.Sprintf("> %s%s  (current)", colorDot, e.label)
			}
			content = append(content, lipgloss.NewStyle().Foreground(theme.TextTertiary).Render(label))
		} else {
			prefix := "  "
			if i == m.cursor {
				prefix = "> "
			}
			content = append(content, labelStyle.Render(prefix)+keyStyle.Render(shortcut)+"  "+colorDot+labelStyle.Render(e.label))
		}
	}

	hintStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary).Padding(0, 1)
	content = append(content, "", hintStyle.Render("esc cancel"))

	return RenderPanel(lipgloss.JoinVertical(lipgloss.Left, content...), m.width, m.height)
}
