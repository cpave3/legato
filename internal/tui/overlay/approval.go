package overlay

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// ApprovalItem is one pending approval as the overlay shows it.
type ApprovalItem struct {
	ID         string
	Kind       string // "Permission", "Question", ...
	Title      string
	Asker      string
	Detail     string
	AllowLabel string // the label of the option y takes
	// Allows are the ways to allow, in order: 1 takes the first, 2 the
	// second; y takes the first too.
	Allows []ApprovalChoice
	// Question: answered in the web UI; here it can only be dismissed.
	Question bool
}

// ApprovalChoice is one way to allow an approval.
type ApprovalChoice struct {
	ID    string
	Label string
}

// ApprovalDecideMsg asks the app to allow (y, or a number) or deny (n) an
// approval. Option is the way to allow, "" for the first. For a question,
// Allow is never set and a deny dismisses it.
type ApprovalDecideMsg struct {
	ID     string
	Allow  bool
	Option string
}

// ApprovalOverlayClosedMsg says the person closed the overlay.
type ApprovalOverlayClosedMsg struct{}

// ApprovalOverlay answers pending approvals one at a time: y allows with the
// first way to allow, a number with that way, n denies, j/k choose, esc
// closes.
type ApprovalOverlay struct {
	items         []ApprovalItem
	cursor        int
	note          string
	width, height int
}

func NewApprovalOverlay(items []ApprovalItem) ApprovalOverlay {
	return ApprovalOverlay{items: items}
}

// SetItems replaces the list, keeping the selected approval when it is
// still there.
func (m ApprovalOverlay) SetItems(items []ApprovalItem) ApprovalOverlay {
	selected := ""
	if m.cursor < len(m.items) {
		selected = m.items[m.cursor].ID
	}
	m.items, m.cursor, m.note = items, 0, ""
	for i, item := range items {
		if item.ID == selected {
			m.cursor = i
		}
	}
	return m
}

// SetNote shows a line under the list, such as why a decision failed.
func (m ApprovalOverlay) SetNote(note string) ApprovalOverlay {
	m.note = note
	return m
}

func (m ApprovalOverlay) Init() tea.Cmd { return nil }

func (m ApprovalOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q":
			return m, func() tea.Msg { return ApprovalOverlayClosedMsg{} }
		case "j", "down":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
			m.note = ""
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
			m.note = ""
		case "y", "1", "2", "3", "4", "5", "6", "7", "8", "9":
			if m.cursor >= len(m.items) {
				return m, nil
			}
			item := m.items[m.cursor]
			if item.Question {
				m.note = "Answer a question in the web UI, or press n to dismiss it."
				return m, nil
			}
			option := ""
			if k := msg.String(); k != "y" {
				n := int(k[0] - '1')
				if n >= len(item.Allows) {
					return m, nil
				}
				option = item.Allows[n].ID
			} else if len(item.Allows) > 0 {
				option = item.Allows[0].ID
			}
			return m, func() tea.Msg { return ApprovalDecideMsg{ID: item.ID, Allow: true, Option: option} }
		case "n":
			if m.cursor >= len(m.items) {
				return m, nil
			}
			id := m.items[m.cursor].ID
			return m, func() tea.Msg { return ApprovalDecideMsg{ID: id} }
		}
	}
	return m, nil
}

func (m ApprovalOverlay) View() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(theme.TextPrimary)
	faint := lipgloss.NewStyle().Foreground(theme.TextSecondary)
	needs := lipgloss.NewStyle().Foreground(theme.Amber)
	mono := lipgloss.NewStyle().Foreground(theme.TextPrimary)

	var b strings.Builder
	b.WriteString(needs.Render("◆ ") + title.Render(fmt.Sprintf("%d %s you", len(m.items), plural(len(m.items), "approval needs", "approvals need"))) + "\n\n")
	for i, item := range m.items {
		marker := "  "
		if i == m.cursor {
			marker = needs.Render("▸ ")
		}
		b.WriteString(marker + faint.Render(strings.ToUpper(item.Kind)+" · ") + title.Render(item.Title) + "\n")
		if item.Asker != "" {
			b.WriteString("    " + faint.Render(item.Asker) + "\n")
		}
		if i == m.cursor && item.Detail != "" {
			for _, line := range strings.Split(clipLines(item.Detail, 4), "\n") {
				b.WriteString("    " + mono.Render(line) + "\n")
			}
		}
	}
	b.WriteString("\n")
	if m.note != "" {
		b.WriteString(needs.Render(m.note) + "\n")
	}
	keys := "y allow · n deny · j/k choose · esc close"
	if m.cursor < len(m.items) {
		item := m.items[m.cursor]
		switch {
		case item.Question:
			keys = "n dismiss · answer in the web UI · j/k choose · esc close"
		case len(item.Allows) > 1:
			var ways []string
			for i, a := range item.Allows {
				if i < 9 {
					ways = append(ways, fmt.Sprintf("%d %s", i+1, strings.ToLower(a.Label)))
				}
			}
			keys = strings.Join(ways, " · ") + " · n deny · j/k choose · esc close"
		case len(item.Allows) == 1:
			keys = "y " + strings.ToLower(item.Allows[0].Label) + " · n deny · j/k choose · esc close"
		case item.AllowLabel != "":
			keys = "y " + strings.ToLower(item.AllowLabel) + " · n deny · j/k choose · esc close"
		}
	}
	b.WriteString(faint.Render(keys))
	return RenderPanel(b.String(), m.width, m.height)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// clipLines keeps at most n lines of s, each at most 72 characters.
func clipLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = append(lines[:n], "…")
	}
	for i, line := range lines {
		if r := []rune(line); len(r) > 72 {
			lines[i] = string(r[:71]) + "…"
		}
	}
	return strings.Join(lines, "\n")
}
