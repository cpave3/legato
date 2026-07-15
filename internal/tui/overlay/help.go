package overlay

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// HelpClosedMsg is sent when the help overlay is dismissed.
type HelpClosedMsg struct{}

type keybinding struct {
	key  string
	desc string
}

var (
	navigationBindings = []keybinding{
		{"h/l", "Move between columns"},
		{"j/k", "Move up/down within column"},
		{"g/G", "Jump to first/last card"},
		{"1-5", "Jump to column by number"},
	}
	actionBindings = []keybinding{
		{"enter", "Open task detail"},
		{"m", "Move task to column"},
		{"n", "Create new task"},
		{"d/D", "Delete task"},
		{"i", "Import remote ticket"},
		{"y", "Copy description"},
		{"Y", "Copy full context"},
		{"r", "Force sync"},
		{"p", "Link PR to task"},
		{"X", "Archive done cards"},
		{"s", "Decompose into swarm"},
		{"/", "Search/filter tasks"},
		{"esc", "Back / close overlay"},
	}
	swarmBindings = []keybinding{
		{"S", "Start swarm (board): pick working dir, spawn conductor"},
		{"y", "Approve plan (in plan-approval overlay)"},
		{"e", "Edit plan in $EDITOR (in plan-approval overlay)"},
		{"n", "Reject plan with notes (in plan-approval overlay)"},
	}
	viewBindings = []keybinding{
		{"A", "Agent view"},
		{"R", "Reports view"},
		{"V", "Review tours"},
		{"w", "Workspace filter"},
	}
	agentBindings = []keybinding{
		{"j/k", "Select agent"},
		{"s", "Spawn agent"},
		{"X", "Kill agent"},
		{"m", "Send macro"},
		{"g", "Set task group"},
		{"M", "Agent actions (swarm)"},
		{"↵", "Attach to session"},
		{"l", "Toggle worker details"},
		{"n", "Toggle notifications"},
		{"v", "Voice dictation (when enabled)"},
		{"esc", "Back to board"},
	}
	generalBindings = []keybinding{
		{"?", "Toggle this help screen"},
		{"q", "Quit"},
	}

	// Per-mode shortlists shown at the top of the help overlay so the user can
	// quickly see the most relevant keys for the view they're in.
	boardShortlist = []keybinding{
		{"↵", "detail"},
		{"m", "move"},
		{"n", "new"},
		{"/", "search"},
		{"r", "sync"},
	}
	detailShortlist = []keybinding{
		{"esc", "back"},
		{"e", "edit"},
		{"m", "move"},
		{"d", "delete"},
		{"y", "copy"},
	}
	agentsShortlist = []keybinding{
		{"j/k", "select"},
		{"s", "spawn"},
		{"X", "kill"},
		{"m", "macro"},
		{"↵", "attach"},
	}
	reportShortlist = []keybinding{
		{"esc", "back"},
		{"y", "copy"},
	}
	reviewShortlist = []keybinding{
		{"j/k", "select step"},
		{"enter", "open tour"},
		{"space", "toggle reviewed"},
		{"a", "ask question"},
		{"c", "complete"},
		{"esc", "back"},
	}
)

// HelpMode identifies which view the user was in when they opened help.
type HelpMode int

const (
	HelpModeBoard HelpMode = iota
	HelpModeDetail
	HelpModeAgents
	HelpModeReport
	HelpModeReview
)

// HelpOverlay displays a keyboard reference screen.
type HelpOverlay struct {
	width     int
	height    int
	mode      HelpMode
	worktrees bool
}

// NewHelp creates a new help overlay.
func NewHelp(width, height int) HelpOverlay {
	return HelpOverlay{width: width, height: height}
}

// NewHelpWithMode creates a help overlay that highlights the shortlist for the
// active view at the top of the screen.
func NewHelpWithMode(width, height int, mode HelpMode) HelpOverlay {
	return HelpOverlay{width: width, height: height, mode: mode}
}

func (m HelpOverlay) WithWorktrees(enabled bool) HelpOverlay { m.worktrees = enabled; return m }

// Init returns no command.
func (m HelpOverlay) Init() tea.Cmd {
	return nil
}

// Update handles messages — only esc and ? dismiss, all else ignored.
func (m HelpOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "?":
			return m, func() tea.Msg { return HelpClosedMsg{} }
		}
	}
	return m, nil
}

// View renders the help overlay.
func (m HelpOverlay) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.TextPrimary)

	sectionStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.AccentPurple).
		MarginTop(1)

	keyStyle := lipgloss.NewStyle().
		Foreground(theme.AccentPurple).
		Bold(true).
		Width(10)

	descStyle := lipgloss.NewStyle().
		Foreground(theme.TextSecondary)

	shortlistKeyStyle := lipgloss.NewStyle().
		Foreground(theme.AccentPurpleAlt).
		Bold(true)

	shortlistDescStyle := lipgloss.NewStyle().
		Foreground(theme.TextSecondary)

	var lines []string
	lines = append(lines, titleStyle.Render("Legato -- Keyboard Reference"))

	// Per-mode shortlist at the top
	shortlist := m.shortlist()
	if len(shortlist) > 0 {
		shortlistStyle := lipgloss.NewStyle().
			Foreground(theme.AccentPurpleAlt).
			Bold(true).
			MarginTop(1)
		lines = append(lines, shortlistStyle.Render(m.shortlistTitle()))
		var parts []string
		for _, b := range shortlist {
			parts = append(parts,
				shortlistKeyStyle.Render(b.key)+" "+shortlistDescStyle.Render(b.desc))
		}
		lines = append(lines, strings.Join(parts, "  "))
	}

	renderSection := func(name string, bindings []keybinding) {
		lines = append(lines, sectionStyle.Render(name))
		for _, b := range bindings {
			lines = append(lines, fmt.Sprintf("  %s%s", keyStyle.Render(b.key), descStyle.Render(b.desc)))
		}
	}

	renderSection("Navigation", navigationBindings)
	actions := actionBindings
	if m.worktrees {
		actions = append(append([]keybinding{}, actions...), keybinding{"b", "Create task worktree"})
	}
	renderSection("Actions", actions)
	renderSection("Swarm", swarmBindings)
	renderSection("Views", viewBindings)
	renderSection("Agents", agentBindings)
	renderSection("General", generalBindings)

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return RenderPanel(content, m.width, m.height)
}

func (m HelpOverlay) shortlist() []keybinding {
	switch m.mode {
	case HelpModeDetail:
		return detailShortlist
	case HelpModeAgents:
		return agentsShortlist
	case HelpModeReport:
		return reportShortlist
	case HelpModeReview:
		return reviewShortlist
	default:
		return boardShortlist
	}
}

func (m HelpOverlay) shortlistTitle() string {
	switch m.mode {
	case HelpModeDetail:
		return "Detail — Quick Reference"
	case HelpModeAgents:
		return "Agents — Quick Reference"
	case HelpModeReport:
		return "Report — Quick Reference"
	case HelpModeReview:
		return "Review — Quick Reference"
	default:
		return "Board — Quick Reference"
	}
}
