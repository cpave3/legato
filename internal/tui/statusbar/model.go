package statusbar

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// SyncState represents the current sync display state.
type SyncState int

const (
	StateOffline SyncState = iota
	StateSyncing
	StateSynced
	StateError
)

// Mode identifies which top-level view is active so the status bar can show
// only the hints relevant to that view.
type Mode int

const (
	ModeBoard Mode = iota
	ModeDetail
	ModeAgents
	ModeReport
)

// Messages
type SyncStartedMsg struct{}
type SyncCompletedMsg struct{ At time.Time }
type SyncFailedMsg struct{}
type WarningMsg struct{ Text string }
type ErrorMsg struct{ Text string }
type InfoMsg struct{ Text string }
type WorkspaceMsg struct {
	Name  string
	Color string
}
type ModeMsg struct{ Mode Mode }

// NtfyMsg tells the status bar whether ntfy notifications are configured,
// so the 'n' (notify) hint shows conditionally in agent mode.
type NtfyMsg struct{ Configured bool }

// VoiceMsg tells the status bar whether voice dictation is enabled, so the
// 'v' (voice) hint shows conditionally in agent mode.
type VoiceMsg struct{ Enabled bool }

// Model is the status bar Bubbletea model.
type Model struct {
	state          SyncState
	mode           Mode
	lastSync       time.Time
	warning        string
	errorText      string
	infoText       string
	workspaceName  string
	workspaceColor string
	webServerPort  string
	ntfyConfigured bool
	voiceEnabled   bool
	width          int
	now            func() time.Time // for testing
}

// New creates a new status bar model.
func New() Model {
	return Model{
		state: StateOffline,
		mode:  ModeBoard,
		now:   time.Now,
	}
}

// Init returns no initial command.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case SyncStartedMsg:
		m.state = StateSyncing
	case SyncCompletedMsg:
		m.state = StateSynced
		m.lastSync = msg.At
		m.errorText = "" // clear errors on successful sync
		m.infoText = ""  // clear transient info
	case SyncFailedMsg:
		m.state = StateError
	case WarningMsg:
		m.warning = msg.Text
	case InfoMsg:
		m.infoText = msg.Text
	case ErrorMsg:
		m.errorText = msg.Text
	case WorkspaceMsg:
		m.workspaceName = msg.Name
		m.workspaceColor = msg.Color
	case ModeMsg:
		m.mode = msg.Mode
	case NtfyMsg:
		m.ntfyConfigured = msg.Configured
	case VoiceMsg:
		m.voiceEnabled = msg.Enabled
	case tea.WindowSizeMsg:
		m.width = msg.Width
	}
	return m, nil
}

// SetWebServer updates the status bar to show the web server port.
func (m Model) SetWebServer(port string) Model {
	m.webServerPort = port
	return m
}

// ClearWebServer removes the web server indicator from the status bar.
func (m Model) ClearWebServer() Model {
	m.webServerPort = ""
	return m
}

// View renders the status bar.
func (m Model) View() string {
	if m.width == 0 {
		return ""
	}

	// Sync indicator
	syncDisplay := m.renderSyncState()

	// Workspace indicator
	wsDisplay := ""
	if m.workspaceName != "" {
		wsColor := lipgloss.Color(theme.TextTertiary)
		if m.workspaceColor != "" {
			wsColor = lipgloss.Color(m.workspaceColor)
		}
		wsStyle := lipgloss.NewStyle().Foreground(wsColor)
		wsDisplay = "  " + wsStyle.Render(m.workspaceName)
	}

	// Web server indicator
	webDisplay := ""
	if m.webServerPort != "" {
		webStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#60a5fa"))
		webDisplay = "  " + webStyle.Render("Web :"+m.webServerPort)
	}

	// Error > warning > info (priority order)
	var warningDisplay string
	if m.errorText != "" {
		errorStyle := lipgloss.NewStyle().Foreground(theme.SyncError)
		warningDisplay = "  " + errorStyle.Render(m.errorText)
	} else if m.warning != "" {
		warningStyle := lipgloss.NewStyle().Foreground(theme.SyncActive)
		warningDisplay = "  " + warningStyle.Render(m.warning)
	} else if m.infoText != "" {
		infoStyle := lipgloss.NewStyle().Foreground(theme.TextSecondary)
		warningDisplay = "  " + infoStyle.Render(m.infoText)
	}

	// Key hints — context-aware per active view, trimmed to essentials.
	hints := m.hintsForMode()

	// Render each hint as a single unit so key+label can't be broken apart.
	var hintParts []string
	for _, h := range hints {
		part := lipgloss.NewStyle().
			Render(theme.KeyHintKey.Render(h.key) + " " + theme.KeyHintLabel.Render(h.label))
		hintParts = append(hintParts, part)
	}

	// The status bar style adds 1 char of left padding, so the inner
	// content width is m.width - 1. Budget the gap + hints from that.
	const padding = 1
	contentMax := m.width - padding
	leftPart := syncDisplay + wsDisplay + webDisplay + warningDisplay
	leftW := lipgloss.Width(leftPart)
	maxHintsW := contentMax - leftW - 1 // at least 1-space gap
	if maxHintsW < 0 {
		maxHintsW = 0
	}

	// Truncate hints to fit
	hintsStr := truncateHints(hintParts, maxHintsW)

	// Recalculate gap with the final hint width so the line is exactly full.
	gap := contentMax - leftW - lipgloss.Width(hintsStr)
	if gap < 1 {
		gap = 1
	}

	content := leftPart + strings.Repeat(" ", gap) + hintsStr
	return theme.StatusBar.Width(m.width).Render(content)
}

func (m Model) renderSyncState() string {
	var dot, text string
	var color lipgloss.Color

	switch m.state {
	case StateSynced:
		dot, text, color = "●", "synced", theme.SyncOK
		if !m.lastSync.IsZero() {
			elapsed := m.now().Sub(m.lastSync)
			text += " " + formatRelativeTime(elapsed)
		}
	case StateSyncing:
		dot, text, color = "●", "syncing...", theme.SyncActive
	case StateError:
		dot, text, color = "●", "sync error", theme.SyncError
	default:
		dot, text, color = "●", "offline", theme.SyncOffline
	}

	dotStyle := lipgloss.NewStyle().Foreground(color)
	return dotStyle.Render(dot) + " " + text
}

func formatRelativeTime(d time.Duration) string {
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d >= time.Minute:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
}

func truncateHints(hints []string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	var result string
	sep := "  "
	for i, h := range hints {
		candidate := result
		if i > 0 {
			candidate += sep
		}
		candidate += h
		if lipgloss.Width(candidate) > maxWidth {
			break
		}
		result = candidate
	}
	return result
}

// hintsForMode returns the compact key-hint set for the active view. The full
// reference is always available via the ? overlay.
func (m Model) hintsForMode() []struct{ key, label string } {
	switch m.mode {
	case ModeDetail:
		return []struct{ key, label string }{
			{"esc", "back"},
			{"e", "edit"},
			{"m", "move"},
			{"d", "delete"},
			{"y", "copy"},
			{"?", "keys"},
		}
	case ModeAgents:
		hints := []struct{ key, label string }{
			{"j/k", "select"},
			{"s", "spawn"},
			{"X", "kill"},
			{"m", "macro"},
			{"↵", "attach"},
			{"esc", "board"},
		}
		// Conditional agent keys — only shown when the feature is configured.
		if m.ntfyConfigured {
			hints = append(hints, struct{ key, label string }{"n", "notify"})
		}
		if m.voiceEnabled {
			hints = append(hints, struct{ key, label string }{"v", "voice"})
		}
		hints = append(hints, struct{ key, label string }{"?", "keys"})
		return hints
	case ModeReport:
		return []struct{ key, label string }{
			{"esc", "back"},
			{"y", "copy"},
			{"?", "keys"},
		}
	default: // ModeBoard
		return []struct{ key, label string }{
			{"↵", "detail"},
			{"m", "move"},
			{"n", "new"},
			{"/", "search"},
			{"r", "sync"},
			{"?", "keys"},
		}
	}
}
