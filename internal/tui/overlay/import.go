package overlay

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

const importVisibleRows = 10

// ImportSelectedMsg is sent when the user selects a remote ticket to import.
type ImportSelectedMsg struct {
	TicketID string
}

// ImportCancelledMsg is sent when the user dismisses the import overlay.
type ImportCancelledMsg struct{}

// ImportQueryChangedMsg is sent when the search query text changes.
type ImportQueryChangedMsg struct {
	Query string
}

// ImportOverlay provides search over remote tickets for importing.
type ImportOverlay struct {
	query    string
	results  []service.RemoteSearchResult
	errMsg   string
	cursor   int
	scrollOff int // first visible index
	width    int
	height   int
}

// NewImport creates a new import overlay.
func NewImport(width, height int) ImportOverlay {
	return ImportOverlay{
		width:  width,
		height: height,
	}
}

// SetResults updates the displayed results.
func (m ImportOverlay) SetResults(results []service.RemoteSearchResult) ImportOverlay {
	m.results = results
	m.errMsg = ""
	if m.cursor >= len(results) {
		m.cursor = 0
	}
	m.scrollOff = 0
	return m
}

// SetError displays an error message in the overlay.
func (m ImportOverlay) SetError(err string) ImportOverlay {
	m.errMsg = err
	m.results = nil
	return m
}

// Query returns the current search query.
func (m ImportOverlay) Query() string {
	return m.query
}

// Init returns no command.
func (m ImportOverlay) Init() tea.Cmd {
	return nil
}

// Update handles messages.
func (m ImportOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return ImportCancelledMsg{} }
		case "enter":
			if len(m.results) > 0 && m.cursor < len(m.results) {
				id := m.results[m.cursor].ID
				return m, func() tea.Msg { return ImportSelectedMsg{TicketID: id} }
			}
			return m, nil
		case "j", "down":
			if m.cursor < len(m.results)-1 {
				m.cursor++
				m.ensureVisible()
			}
			return m, nil
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
				m.ensureVisible()
			}
			return m, nil
		case "backspace":
			if len(m.query) > 0 {
				m.query = m.query[:len(m.query)-1]
				q := m.query
				return m, func() tea.Msg { return ImportQueryChangedMsg{Query: q} }
			}
			return m, nil
		default:
			if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
				m.query += string(msg.Runes)
				q := m.query
				return m, func() tea.Msg { return ImportQueryChangedMsg{Query: q} }
			}
		}
	}
	return m, nil
}

func (m *ImportOverlay) ensureVisible() {
	if m.cursor < m.scrollOff {
		m.scrollOff = m.cursor
	}
	if m.cursor >= m.scrollOff+importVisibleRows {
		m.scrollOff = m.cursor - importVisibleRows + 1
	}
}

// View renders the import overlay.
func (m ImportOverlay) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.TextPrimary)

	inputStyle := lipgloss.NewStyle().
		Foreground(theme.AccentPurple).
		Bold(true)

	promptStyle := lipgloss.NewStyle().
		Foreground(theme.TextTertiary)

	title := titleStyle.Render("Import Remote Ticket")
	input := promptStyle.Render(">") + " " + inputStyle.Render(m.query+"█")

	var lines []string
	lines = append(lines, title, input, "")

	// Fixed-height result area
	resultLines := make([]string, importVisibleRows)
	for i := range resultLines {
		resultLines[i] = "" // empty line placeholder
	}

	if m.errMsg != "" {
		resultLines[0] = lipgloss.NewStyle().Foreground(theme.SyncError).Render("Error: " + m.errMsg)
	} else if len(m.results) == 0 {
		if m.query != "" {
			resultLines[0] = lipgloss.NewStyle().Foreground(theme.TextTertiary).Render("No results")
		} else {
			resultLines[0] = lipgloss.NewStyle().Foreground(theme.TextTertiary).Render("Type to search remote tickets...")
		}
	} else {
		end := m.scrollOff + importVisibleRows
		if end > len(m.results) {
			end = len(m.results)
		}
		for i, idx := 0, m.scrollOff; idx < end; i, idx = i+1, idx+1 {
			r := m.results[idx]
			prefix := "  "
			style := lipgloss.NewStyle().Foreground(theme.TextSecondary)
			if idx == m.cursor {
				prefix = "> "
				style = lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true)
			}
			statusStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
			line := fmt.Sprintf("%s%s  %s", prefix, r.ID, strings.TrimSpace(r.Summary))
			resultLines[i] = style.Render(line) + "  " + statusStyle.Render(r.Status)
		}

	}

	lines = append(lines, resultLines...)

	// Scroll indicator on its own line (always present to keep height stable)
	if len(m.results) > importVisibleRows {
		end := m.scrollOff + importVisibleRows
		if end > len(m.results) {
			end = len(m.results)
		}
		lines = append(lines, promptStyle.Render(fmt.Sprintf("  %d-%d of %d", m.scrollOff+1, end, len(m.results))))
	} else {
		lines = append(lines, "") // blank line to keep height consistent
	}

	// Fixed panel width (60% of terminal, min 50)
	panelWidth := m.width * 60 / 100
	if panelWidth < 50 {
		panelWidth = 50
	}
	// Account for panel border + padding (2 border + 4 padding = 6)
	contentWidth := panelWidth - 6
	if contentWidth < 20 {
		contentWidth = 20
	}

	content := lipgloss.NewStyle().Width(contentWidth).Render(
		lipgloss.JoinVertical(lipgloss.Left, lines...),
	)
	return RenderPanel(content, m.width, m.height)
}
