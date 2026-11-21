package overlay

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/hotkey"
	"github.com/cpave3/legato/internal/tui/theme"
)

// SessionHistorySearchMsg asks for the history matching a query ("" for
// all of it, newest first).
type SessionHistorySearchMsg struct{ Query string }

// SessionRestoreMsg asks to restore a conversation into Target.
type SessionRestoreMsg struct {
	ID     int64
	TaskID string
	Target string
}

// SessionHistoryRefusedMsg says why the selected conversation cannot be
// restored that way.
type SessionHistoryRefusedMsg struct{ Reason string }

// SessionHistoryCancelledMsg closes the history.
type SessionHistoryCancelledMsg struct{}

// SessionHistoryOverlay lists the session history, newest first, with the
// selected entry's first prompt and last message, a search field and keys
// to restore.
type SessionHistoryOverlay struct {
	entries   []service.SessionEntry
	err       error
	loaded    bool
	cursor    int
	query     string
	searching bool
	width     int
	height    int
}

// NewSessionHistory makes the overlay; its entries come with SetEntries.
func NewSessionHistory() SessionHistoryOverlay { return SessionHistoryOverlay{} }

// SetEntries shows a page of the history, or why it could not be read.
func (m SessionHistoryOverlay) SetEntries(entries []service.SessionEntry, err error) SessionHistoryOverlay {
	m.entries, m.err, m.loaded = entries, err, true
	if m.cursor >= len(entries) {
		m.cursor = max(0, len(entries)-1)
	}
	return m
}

// Selected is the selected entry, nil when there is none.
func (m SessionHistoryOverlay) Selected() *service.SessionEntry {
	if m.cursor < len(m.entries) {
		return &m.entries[m.cursor]
	}
	return nil
}

func (m SessionHistoryOverlay) Init() tea.Cmd { return nil }

// ConsumesPrintableInput is true while the search field has the focus, so
// a typed ? is part of the search, not a request for help.
func (m SessionHistoryOverlay) ConsumesPrintableInput() bool { return m.searching }

func (m SessionHistoryOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		if m.searching {
			return m.typeSearch(msg)
		}
		binding, ok := hotkey.DefaultRegistry().Resolve(msg, hotkey.Context{Overlay: hotkey.OverlaySessionHistory})
		if !ok {
			return m, nil
		}
		switch binding.ID {
		case hotkey.ActionOverlayDown:
			if m.cursor < len(m.entries)-1 {
				m.cursor++
			}
		case hotkey.ActionOverlayUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case hotkey.ActionHistorySearch:
			m.searching = true
		case hotkey.ActionHistoryRestore:
			return m, m.restore(service.RestoreIntoTask)
		case hotkey.ActionHistoryNewTask:
			return m, m.restore(service.RestoreIntoNewTask)
		case hotkey.ActionOverlayCancel:
			return m, func() tea.Msg { return SessionHistoryCancelledMsg{} }
		}
	}
	return m, nil
}

// typeSearch edits the search field; enter and esc end the search.
func (m SessionHistoryOverlay) typeSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter, tea.KeyEsc:
		m.searching = false
		return m, nil
	case tea.KeyBackspace:
		if runes := []rune(m.query); len(runes) > 0 {
			m.query = string(runes[:len(runes)-1])
		}
	case tea.KeyRunes, tea.KeySpace:
		m.query += string(msg.Runes)
		if msg.Type == tea.KeySpace {
			m.query += " "
		}
	default:
		return m, nil
	}
	m.cursor = 0
	query := m.query
	return m, func() tea.Msg { return SessionHistorySearchMsg{Query: query} }
}

// restore asks to restore the selected entry, or says why it cannot be.
func (m SessionHistoryOverlay) restore(target string) tea.Cmd {
	e := m.Selected()
	if e == nil {
		return nil
	}
	reason := ""
	switch {
	case !e.Restorable:
		reason = e.Reason
	case target == service.RestoreIntoTask && e.NewTaskOnly:
		reason = e.Reason
	case target == service.RestoreIntoNewTask && e.Reeve:
		reason = "a Reeve conversation is restored as the Reeve's, not into a new task"
	}
	if reason != "" {
		return func() tea.Msg { return SessionHistoryRefusedMsg{Reason: reason} }
	}
	restore := SessionRestoreMsg{ID: e.ID, TaskID: e.TaskID, Target: target}
	return func() tea.Msg { return restore }
}

func historyState(e service.SessionEntry) string {
	switch e.State {
	case "running":
		return "running"
	case "interrupted":
		return "interrupted"
	case "away":
		return "away on a worker"
	}
	if e.EndReason != "" {
		return "ended · " + e.EndReason
	}
	return "ended"
}

func (m SessionHistoryOverlay) View() string {
	faint := lipgloss.NewStyle().Foreground(theme.TextTertiary)
	plain := lipgloss.NewStyle().Foreground(theme.TextSecondary)
	bold := lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true)
	lines := []string{bold.Padding(0, 1).Render("Session history"), ""}
	search := "/ search"
	if m.searching || m.query != "" {
		search = "Search: " + m.query
		if m.searching {
			search += "▏"
		}
	}
	lines = append(lines, plain.Render(search), "")
	rows := max(3, m.height/3)
	switch {
	case m.err != nil:
		lines = append(lines, faint.Render("Could not read the history: "+m.err.Error()))
	case !m.loaded:
		lines = append(lines, faint.Render("Loading…"))
	case len(m.entries) == 0:
		lines = append(lines, faint.Render("No sessions."))
	}
	start := max(0, min(m.cursor-rows/2, len(m.entries)-rows))
	for i := start; i < len(m.entries) && i < start+rows; i++ {
		e := m.entries[i]
		prefix, style := "  ", plain
		if i == m.cursor {
			prefix, style = "> ", bold
		}
		where := e.CWD
		if e.Repo != "" {
			where = e.Repo
			if e.Branch != "" {
				where += " · " + e.Branch
			}
		}
		title := e.TaskTitle
		if title == "" {
			title = e.TaskID
		}
		lines = append(lines, style.Render(prefix+title)+faint.Render(fmt.Sprintf("  %s · %s · %s", e.AgentKind, historyState(e), e.LastSeenAt.Local().Format("2 Jan 15:04"))))
		lines = append(lines, faint.Render("    "+where))
	}
	if e := m.Selected(); e != nil {
		lines = append(lines, "")
		if e.FirstMessage != "" {
			lines = append(lines, plain.Render("First prompt: ")+oneLine(e.FirstMessage))
		}
		if e.LastMessage != "" {
			lines = append(lines, plain.Render("Last message: ")+oneLine(e.LastMessage))
		}
		if e.Source == "recovered" {
			lines = append(lines, faint.Render("Found by folder and time"))
		}
		if e.Reason != "" {
			lines = append(lines, faint.Render(e.Reason))
		}
	}
	lines = append(lines, "", faint.Padding(0, 1).Render("j/k select · / search · ↵ restore · n new task · esc close"))
	return RenderPanel(lipgloss.JoinVertical(lipgloss.Left, lines...), m.width, m.height)
}

// oneLine is text on one line, cut to fit a panel.
func oneLine(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if runes := []rune(text); len(runes) > 160 {
		text = string(runes[:159]) + "…"
	}
	return text
}
