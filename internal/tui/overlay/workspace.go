package overlay

import (
	"fmt"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

// WorkspaceSelectedMsg is sent when the user selects a workspace view.
type WorkspaceSelectedMsg struct {
	View store.WorkspaceView
}

// WorkspaceCancelledMsg is sent when the user cancels the workspace switcher.
type WorkspaceCancelledMsg struct{}

type wsEntry struct {
	label string
	view  store.WorkspaceView
	color string // hex color or ""
}

// WorkspaceOverlay lets the user pick a workspace view.
type WorkspaceOverlay struct {
	entries   []wsEntry
	shortcuts map[rune]int // shortcut key → entry index
	current   store.WorkspaceView
	cursor    int
	width     int
	height    int
}

// NewWorkspace creates a workspace switcher overlay.
func NewWorkspace(workspaces []service.Workspace, current store.WorkspaceView) WorkspaceOverlay {
	entries := []wsEntry{
		{label: "All", view: store.WorkspaceView{Kind: store.ViewAll}},
		{label: "Unassigned", view: store.WorkspaceView{Kind: store.ViewUnassigned}},
	}
	for _, ws := range workspaces {
		entries = append(entries, wsEntry{
			label: ws.Name,
			view:  store.WorkspaceView{Kind: store.ViewWorkspace, WorkspaceID: ws.ID},
			color: ws.Color,
		})
	}

	shortcuts := buildWSShortcuts(entries)

	// Set cursor to current selection
	cursor := 0
	for i, e := range entries {
		if e.view.Kind == current.Kind && e.view.WorkspaceID == current.WorkspaceID {
			cursor = i
			break
		}
	}

	return WorkspaceOverlay{
		entries:   entries,
		shortcuts: shortcuts,
		current:   current,
		cursor:    cursor,
	}
}

func buildWSShortcuts(entries []wsEntry) map[rune]int {
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

	// Handle conflicts — assign number keys for unassigned entries
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

func (m WorkspaceOverlay) shortcutFor(idx int) string {
	for k, v := range m.shortcuts {
		if v == idx {
			return string(k)
		}
	}
	return ""
}

func (m WorkspaceOverlay) Init() tea.Cmd { return nil }

func (m WorkspaceOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q":
			return m, func() tea.Msg { return WorkspaceCancelledMsg{} }
		case "j", "down":
			if m.cursor < len(m.entries)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "enter":
			if m.cursor < len(m.entries) {
				view := m.entries[m.cursor].view
				return m, func() tea.Msg { return WorkspaceSelectedMsg{View: view} }
			}
		default:
			if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
				r := msg.Runes[0]
				if idx, ok := m.shortcuts[r]; ok {
					view := m.entries[idx].view
					return m, func() tea.Msg { return WorkspaceSelectedMsg{View: view} }
				}
			}
		}
	}
	return m, nil
}

func (m WorkspaceOverlay) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.TextPrimary).
		Padding(0, 1)

	title := titleStyle.Render("Workspace")
	content := []string{title, ""}

	for i, e := range m.entries {
		shortcut := m.shortcutFor(i)
		keyStyle := lipgloss.NewStyle().Foreground(theme.AccentPurple).Bold(true)
		labelStyle := lipgloss.NewStyle().Foreground(theme.TextSecondary)

		if i == m.cursor {
			labelStyle = lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true)
		}

		isCurrent := e.view.Kind == m.current.Kind && e.view.WorkspaceID == m.current.WorkspaceID

		// Color dot for workspaces
		colorDot := ""
		if e.color != "" {
			dotStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(e.color))
			colorDot = dotStyle.Render("●") + " "
		}

		if isCurrent {
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

	return RenderPanel(lipgloss.JoinVertical(lipgloss.Left, content...), m.width, m.height)
}
