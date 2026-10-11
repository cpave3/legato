package statusbar

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/hotkey"
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

// Messages
type SyncStartedMsg struct{}
type SyncCompletedMsg struct{ At time.Time }
type SyncFailedMsg struct{}
type WarningMsg struct{ Text string }
type ErrorMsg struct{ Text string }
type InfoMsg struct{ Text string }
type ProgressMsg struct{ Text string }
type ClearTransientMsg struct {
	Kind string
	Seq  uint64
}
type WorkspaceMsg struct {
	Name  string
	Color string
}
type HotkeyContextMsg struct {
	Registry *hotkey.Registry
	Context  hotkey.Context
}

// StatusShownElsewhereMsg says whether the active view shows the sync state,
// the workspace and the web server itself. The bar then leaves them out.
type StatusShownElsewhereMsg struct{ Elsewhere bool }

// NtfyMsg tells the status bar whether ntfy notifications are configured,
// so the 'n' (notify) hint shows conditionally in agent mode.
type NtfyMsg struct{ Configured bool }

// VoiceMsg tells the status bar whether voice dictation is enabled, so the
// 'v' (voice) hint shows conditionally in agent mode.
type VoiceMsg struct{ Enabled bool }

// Model is the status bar Bubbletea model.
type Model struct {
	state           SyncState
	lastSync        time.Time
	warning         string
	errorText       string
	infoText        string
	progressText    string
	transientSeq    uint64
	workspaceName   string
	workspaceColor  string
	webServerPort   string
	statusElsewhere bool
	hotkeys         *hotkey.Registry
	hotkeyContext   hotkey.Context
	width           int
	now             func() time.Time // for testing
}

// New creates a new status bar model.
func New() Model {
	return Model{
		state:         StateOffline,
		hotkeys:       hotkey.DefaultRegistry(),
		hotkeyContext: hotkey.Context{Page: hotkey.PageBoard, Predicates: hotkey.NewPredicates(hotkey.PredicateLocalTask, hotkey.PredicateEditor)},
		now:           time.Now,
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
		m.errorText = ""
		m.transientSeq++
		return m, clearTransientAfter("info", m.transientSeq)
	case ProgressMsg:
		m.progressText = msg.Text
	case ErrorMsg:
		m.errorText = msg.Text
		m.infoText = ""
		m.transientSeq++
		return m, clearTransientAfter("error", m.transientSeq)
	case ClearTransientMsg:
		if msg.Seq == m.transientSeq {
			if msg.Kind == "error" {
				m.errorText = ""
			} else {
				m.infoText = ""
			}
		}
	case WorkspaceMsg:
		m.workspaceName = msg.Name
		m.workspaceColor = msg.Color
	case HotkeyContextMsg:
		m.hotkeys = msg.Registry
		m.hotkeyContext = msg.Context
	case StatusShownElsewhereMsg:
		m.statusElsewhere = msg.Elsewhere
	case NtfyMsg:
		if m.hotkeyContext.Capabilities == nil {
			m.hotkeyContext.Capabilities = hotkey.NewCapabilities()
		}
		m.hotkeyContext.Capabilities[hotkey.CapabilityNotifications] = msg.Configured
	case VoiceMsg:
		if m.hotkeyContext.Capabilities == nil {
			m.hotkeyContext.Capabilities = hotkey.NewCapabilities()
		}
		m.hotkeyContext.Capabilities[hotkey.CapabilityVoice] = msg.Enabled
	case tea.WindowSizeMsg:
		m.width = msg.Width
	}
	return m, nil
}

func clearTransientAfter(kind string, seq uint64) tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg {
		return ClearTransientMsg{Kind: kind, Seq: seq}
	})
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

// barPadding is the space at each end of the bar.
const barPadding = 2

// hintGap is the space between two hints.
const hintGap = "   "

// View renders the bar: one line, as wide as the terminal. A message comes
// first, then the active page's keys; the keys that leave the page or change
// how it is shown sit at the far end.
func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	inner := m.width - 2*barPadding
	if inner < 1 {
		return theme.Fill("", m.width, theme.Bar)
	}

	var lead []string
	if !m.statusElsewhere {
		lead = append(lead, m.SyncLabel())
		if name, color := m.Workspace(); name != "" {
			lead = append(lead, lipgloss.NewStyle().Foreground(color).Render(name))
		}
		if web := m.WebLabel(); web != "" {
			lead = append(lead, lipgloss.NewStyle().Foreground(theme.Blue).Render(web))
		}
	}
	if message := m.renderMessage(); message != "" {
		lead = append(lead, message)
	}
	left := strings.Join(lead, "  ")

	pageHints, endHints := m.hintGroups()
	// The far group gives way from its front, so the key reference stays.
	right := fitHintsFromEnd(endHints, inner-lipgloss.Width(left)-1)
	room := inner - lipgloss.Width(left) - lipgloss.Width(right)
	if left != "" {
		room -= len(hintGap)
	}
	if right != "" {
		room -= len(hintGap)
	}
	if hints := fitHints(pageHints, room); hints != "" {
		if left != "" {
			left += hintGap
		}
		left += hints
	}

	line := strings.Repeat(" ", barPadding) + theme.Spread(left, right, inner)
	return theme.Fill(line, m.width, theme.Bar)
}

// renderMessage returns what the bar has to say, the most pressing first:
// work in progress, an error, a standing warning, a passing notice.
func (m Model) renderMessage() string {
	switch {
	case m.progressText != "":
		return lipgloss.NewStyle().Foreground(theme.SyncActive).Render(m.progressText)
	case m.errorText != "":
		return lipgloss.NewStyle().Foreground(theme.SyncError).Render(m.errorText)
	case m.warning != "":
		return lipgloss.NewStyle().Foreground(theme.Amber).Render(m.warning)
	case m.infoText != "":
		return lipgloss.NewStyle().Foreground(theme.TextPrimary).Render(m.infoText)
	}
	return ""
}

// SyncLabel is the sync state as a colored dot and a few words. A view that
// shows the state itself draws this.
func (m Model) SyncLabel() string {
	var text string
	var color lipgloss.Color

	switch m.state {
	case StateSynced:
		text, color = "synced", theme.SyncOK
		if !m.lastSync.IsZero() {
			elapsed := m.now().Sub(m.lastSync)
			text += " " + formatRelativeTime(elapsed)
		}
	case StateSyncing:
		text, color = "syncing...", theme.SyncActive
	case StateError:
		text, color = "sync error", theme.SyncError
	default:
		text, color = "offline", theme.SyncOffline
	}

	return lipgloss.NewStyle().Foreground(color).Render("●") + " " +
		lipgloss.NewStyle().Foreground(theme.TextSecondary).Render(text)
}

// WebLabel names the port of the running web server, or is "" without one.
func (m Model) WebLabel() string {
	if m.webServerPort == "" {
		return ""
	}
	return "web :" + m.webServerPort
}

// Workspace returns the name of the workspace the board is filtered to and
// the color to write it in. The name is "" when the bar was told none.
func (m Model) Workspace() (string, lipgloss.Color) {
	color := theme.TextSecondary
	if m.workspaceColor != "" {
		color = lipgloss.Color(m.workspaceColor)
	}
	return m.workspaceName, color
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

// hint is one entry of the bar: the keys for an action and its name.
type hint struct{ key, label string }

func (h hint) render() string {
	return theme.KeyHintKey.Render(h.key) + " " + theme.KeyHintLabel.Render(h.label)
}

// fitHints joins as many hints as fit in maxWidth, from the first.
func fitHints(hints []hint, maxWidth int) string {
	var result string
	for i, h := range hints {
		candidate := result
		if i > 0 {
			candidate += hintGap
		}
		candidate += h.render()
		if lipgloss.Width(candidate) > maxWidth {
			break
		}
		result = candidate
	}
	return result
}

// fitHintsFromEnd joins as many hints as fit in maxWidth, keeping the last.
func fitHintsFromEnd(hints []hint, maxWidth int) string {
	var result string
	for i := len(hints) - 1; i >= 0; i-- {
		candidate := hints[i].render()
		if result != "" {
			candidate += hintGap + result
		}
		if lipgloss.Width(candidate) > maxWidth {
			break
		}
		result = candidate
	}
	return result
}

// hintGroups returns the compact key hints of the active view: the page's
// own, and those that go at the bar's far end. Neighbours with the same name
// become one hint ("j/k select"). The full reference is always available via
// the ? overlay.
func (m Model) hintGroups() (page, end []hint) {
	if m.hotkeys == nil {
		return nil, nil
	}
	for _, binding := range m.hotkeys.Bindings(m.hotkeyContext, hotkey.ShortcutBar) {
		group := &page
		if binding.Surfaces&hotkey.BarEnd != 0 {
			group = &end
		}
		key := barKey(binding)
		if last := len(*group) - 1; last >= 0 && (*group)[last].label == binding.Label {
			(*group)[last].key += "/" + key
			continue
		}
		*group = append(*group, hint{key: key, label: binding.Label})
	}
	return page, end
}

// barKey is the first key of a binding as the bar writes it.
func barKey(binding hotkey.Binding) string {
	if len(binding.Keys) == 0 {
		return ""
	}
	if key := binding.Keys[0].Display; key != "enter" {
		return key
	}
	return "↵"
}
