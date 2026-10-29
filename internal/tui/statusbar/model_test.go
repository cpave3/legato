package statusbar

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/hotkey"
)

func TestInitialState(t *testing.T) {
	m := New()
	if m.state != StateOffline {
		t.Errorf("initial state = %v, want StateOffline", m.state)
	}
}

func TestSyncStartedTransition(t *testing.T) {
	m := New()
	m, _ = m.Update(SyncStartedMsg{})
	if m.state != StateSyncing {
		t.Errorf("state after SyncStarted = %v, want StateSyncing", m.state)
	}
}

func TestSyncCompletedTransition(t *testing.T) {
	m := New()
	m, _ = m.Update(SyncCompletedMsg{At: time.Now()})
	if m.state != StateSynced {
		t.Errorf("state after SyncCompleted = %v, want StateSynced", m.state)
	}
	if m.lastSync.IsZero() {
		t.Error("lastSync should be set after SyncCompleted")
	}
}

func TestSyncFailedTransition(t *testing.T) {
	m := New()
	m, _ = m.Update(SyncFailedMsg{})
	if m.state != StateError {
		t.Errorf("state after SyncFailed = %v, want StateError", m.state)
	}
}

func TestWindowResize(t *testing.T) {
	m := New()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if m.width != 120 {
		t.Errorf("width = %d, want 120", m.width)
	}
}

func TestRelativeTimeFormatting(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{"seconds", 45 * time.Second, "45s ago"},
		{"minutes", 2 * time.Minute, "2m ago"},
		{"hours", 3 * time.Hour, "3h ago"},
		{"zero", 0, "0s ago"},
		{"just over minute", 90 * time.Second, "1m ago"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatRelativeTime(tt.duration)
			if got != tt.want {
				t.Errorf("formatRelativeTime(%v) = %q, want %q", tt.duration, got, tt.want)
			}
		})
	}
}

func TestViewContainsSyncIndicator(t *testing.T) {
	tests := []struct {
		state SyncState
		want  string
	}{
		{StateOffline, "offline"},
		{StateSyncing, "syncing"},
		{StateSynced, "synced"},
		{StateError, "sync error"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			m := New()
			m.state = tt.state
			m.width = 120
			view := m.View()
			if !strings.Contains(view, tt.want) {
				t.Errorf("view should contain %q, got: %q", tt.want, view)
			}
		})
	}
}

func TestViewContainsKeyHints(t *testing.T) {
	m := New()
	m.width = 120
	view := m.View()
	// Board mode (default) should contain the "keys" hint for ?
	if !strings.Contains(view, "keys") {
		t.Errorf("view should contain 'keys' hint for ?, got: %q", view)
	}
}

func TestBoardModeHints(t *testing.T) {
	m := New()
	m.width = 120
	view := m.View()
	// Board mode should show these compact hints
	for _, expected := range []string{"detail", "move", "new", "search", "sync", "keys"} {
		if !strings.Contains(view, expected) {
			t.Errorf("board mode should contain %q, got: %q", expected, view)
		}
	}
}

func TestDetailModeHints(t *testing.T) {
	m := New()
	m.width = 120
	m, _ = m.Update(HotkeyContextMsg{Registry: hotkey.DefaultRegistry(), Context: hotkey.Context{Page: hotkey.PageDetail, Predicates: hotkey.NewPredicates(hotkey.PredicateLocalTask, hotkey.PredicateEditor)}})
	view := m.View()
	for _, expected := range []string{"back", "edit", "delete", "copy", "keys"} {
		if !strings.Contains(view, expected) {
			t.Errorf("detail mode should contain %q, got: %q", expected, view)
		}
	}
}

func TestAgentsModeHints(t *testing.T) {
	m := New()
	m.width = 120
	m, _ = m.Update(HotkeyContextMsg{Registry: hotkey.DefaultRegistry(), Context: hotkey.Context{Page: hotkey.PageAgents}})
	view := m.View()
	for _, expected := range []string{"select", "spawn", "kill", "macro", "attach", "board", "keys"} {
		if !strings.Contains(view, expected) {
			t.Errorf("agents mode should contain %q, got: %q", expected, view)
		}
	}
	// Without ntfy/voice, those hints should not appear
	if strings.Contains(view, "notify") {
		t.Errorf("agents mode should not contain 'notify' without ntfy, got: %q", view)
	}
	if strings.Contains(view, "voice") {
		t.Errorf("agents mode should not contain 'voice' without voice enabled, got: %q", view)
	}
}

func TestAgentsModeNtfyHint(t *testing.T) {
	m := New()
	m.width = 120
	m, _ = m.Update(HotkeyContextMsg{Registry: hotkey.DefaultRegistry(), Context: hotkey.Context{Page: hotkey.PageAgents}})
	m, _ = m.Update(NtfyMsg{Configured: true})
	view := m.View()
	if !strings.Contains(view, "notify") {
		t.Errorf("agents mode should contain 'notify' when ntfy configured, got: %q", view)
	}
}

func TestAgentsModeVoiceHint(t *testing.T) {
	m := New()
	m.width = 120
	m, _ = m.Update(HotkeyContextMsg{Registry: hotkey.DefaultRegistry(), Context: hotkey.Context{Page: hotkey.PageAgents}})
	m, _ = m.Update(VoiceMsg{Enabled: true})
	view := m.View()
	if !strings.Contains(view, "voice") {
		t.Errorf("agents mode should contain 'voice' when voice enabled, got: %q", view)
	}
}

func TestReportModeHints(t *testing.T) {
	m := New()
	m.width = 120
	m, _ = m.Update(HotkeyContextMsg{Registry: hotkey.DefaultRegistry(), Context: hotkey.Context{Page: hotkey.PageReport}})
	view := m.View()
	for _, expected := range []string{"back", "copy", "keys"} {
		if !strings.Contains(view, expected) {
			t.Errorf("report mode should contain %q, got: %q", expected, view)
		}
	}
}

func TestReviewModeHintsIncludeDelete(t *testing.T) {
	m := New()
	m.width = 120
	m, _ = m.Update(HotkeyContextMsg{Registry: hotkey.DefaultRegistry(), Context: hotkey.Context{Page: hotkey.PageReview, Mode: hotkey.ModeTour}})
	view := m.View()
	for _, expected := range []string{"select", "reviewed", "ask", "delete"} {
		if !strings.Contains(view, expected) {
			t.Errorf("review mode should contain %q, got: %q", expected, view)
		}
	}
}

func TestTransientMessageExpiresWithoutClearingNewerMessage(t *testing.T) {
	m := New()
	m, cmd := m.Update(ErrorMsg{Text: "first failure"})
	if cmd == nil {
		t.Fatal("error should schedule expiry")
	}
	firstSeq := m.transientSeq
	m, _ = m.Update(ErrorMsg{Text: "newer failure"})
	m, _ = m.Update(ClearTransientMsg{Kind: "error", Seq: firstSeq})
	if m.errorText != "newer failure" {
		t.Fatalf("stale expiry cleared newer error: %q", m.errorText)
	}

	m, cmd = m.Update(InfoMsg{Text: "no pull request found"})
	if cmd == nil {
		t.Fatal("info should schedule expiry")
	}
	m, _ = m.Update(ClearTransientMsg{Kind: "info", Seq: m.transientSeq})
	if m.infoText != "" {
		t.Fatalf("info did not expire: %q", m.infoText)
	}
}

func TestProgressMessageTakesPriorityAndCanBeCleared(t *testing.T) {
	m := New()
	m.width = 120
	m, _ = m.Update(WarningMsg{Text: "clipboard unavailable"})
	m, _ = m.Update(ProgressMsg{Text: "importing REX-42..."})
	if view := m.View(); !strings.Contains(view, "importing REX-42...") {
		t.Fatalf("progress should be visible over warnings, got: %q", view)
	}
	m, _ = m.Update(ProgressMsg{})
	view := m.View()
	if strings.Contains(view, "importing REX-42...") {
		t.Fatalf("empty progress message should clear progress, got: %q", view)
	}
	if !strings.Contains(view, "clipboard unavailable") {
		t.Fatalf("clearing progress should reveal the prior warning, got: %q", view)
	}
}

func TestWarningMessage(t *testing.T) {
	m := New()
	m.width = 120
	m, _ = m.Update(WarningMsg{Text: "clipboard unavailable"})
	if m.warning != "clipboard unavailable" {
		t.Errorf("warning = %q, want 'clipboard unavailable'", m.warning)
	}
	view := m.View()
	if !strings.Contains(view, "clipboard unavailable") {
		t.Errorf("view should contain warning, got: %q", view)
	}
}

func TestErrorMessageDisplayed(t *testing.T) {
	m := New()
	m.width = 120
	m, _ = m.Update(ErrorMsg{Text: "auth error: check config"})
	view := m.View()
	if !strings.Contains(view, "auth error") {
		t.Errorf("view should contain error message, got: %q", view)
	}
}

func TestErrorMessageClearsAfterSync(t *testing.T) {
	m := New()
	m.width = 120
	m, _ = m.Update(ErrorMsg{Text: "offline"})
	m, _ = m.Update(SyncCompletedMsg{At: time.Now()})
	if m.errorText != "" {
		t.Errorf("error should clear after sync, got %q", m.errorText)
	}
}

func TestWorkspaceIndicator(t *testing.T) {
	m := New()
	m.width = 120
	m, _ = m.Update(WorkspaceMsg{Name: "Work", Color: "#4A9EEF"})
	if m.workspaceName != "Work" {
		t.Errorf("workspaceName = %q, want Work", m.workspaceName)
	}
	view := m.View()
	if !strings.Contains(view, "Work") {
		t.Errorf("view should contain workspace name 'Work', got: %q", view)
	}
}

func TestNarrowTerminalTruncatesHints(t *testing.T) {
	m := New()
	m.width = 30 // very narrow
	view := m.View()
	// Should still show sync state
	if !strings.Contains(view, "offline") {
		t.Errorf("narrow view should still show sync state, got: %q", view)
	}
}

func TestBarIsOneLineAsWideAsTheTerminal(t *testing.T) {
	m := New()
	m.width = 120
	m, _ = m.Update(HotkeyContextMsg{Registry: hotkey.DefaultRegistry(), Context: hotkey.Context{Page: hotkey.PageAgents}})
	m, _ = m.Update(NtfyMsg{Configured: true})
	m, _ = m.Update(VoiceMsg{Enabled: true})

	view := m.View()

	if strings.Contains(view, "\n") {
		t.Fatalf("the bar should be one line, got: %q", view)
	}
	if w := lipgloss.Width(view); w != 120 {
		t.Fatalf("the bar is %d wide, want 120: %q", w, view)
	}
}

func TestBarPutsThePagesKeysFirstAndTheGlobalOnesAtTheRightEdge(t *testing.T) {
	m := New()
	m.width = 160
	m, _ = m.Update(StatusShownElsewhereMsg{Elsewhere: true})

	view := m.View()

	if !strings.HasPrefix(strings.TrimSpace(view), "↵ detail") {
		t.Fatalf("the bar should open with the page's first key, got: %q", view)
	}
	if !strings.HasSuffix(strings.TrimRight(view, " "), "? keys") {
		t.Fatalf("the bar should end with the key reference, got: %q", view)
	}
	pageKeys, viewKeys := strings.Index(view, "search"), strings.Index(view, "agents")
	if gap := view[pageKeys:viewKeys]; !strings.Contains(gap, "      ") {
		t.Fatalf("the two groups should be apart, got: %q", view)
	}
}

func TestBarNamesTwoKeysForTheSameThingOnce(t *testing.T) {
	m := New()
	m.width = 160
	m, _ = m.Update(HotkeyContextMsg{Registry: hotkey.DefaultRegistry(), Context: hotkey.Context{Page: hotkey.PageAgents}})

	view := m.View()

	if !strings.Contains(view, "j/k select") {
		t.Fatalf("up and down should be one hint, got: %q", view)
	}
	if strings.Count(view, "select") != 1 {
		t.Fatalf("select is named more than once: %q", view)
	}
}

func TestBarLeavesTheSyncStateOutWhenTheViewShowsIt(t *testing.T) {
	m := New()
	m.width = 160
	m, _ = m.Update(WorkspaceMsg{Name: "Work", Color: "#4A9EEF"})
	m = m.SetWebServer("3080")
	m, _ = m.Update(StatusShownElsewhereMsg{Elsewhere: true})

	view := m.View()

	for _, shownElsewhere := range []string{"offline", "Work", "3080"} {
		if strings.Contains(view, shownElsewhere) {
			t.Errorf("the bar should leave %q to the view, got: %q", shownElsewhere, view)
		}
	}
	if got := m.SyncLabel(); !strings.Contains(got, "offline") {
		t.Errorf("SyncLabel() = %q, want the sync state", got)
	}
	if got := m.WebLabel(); got != "web :3080" {
		t.Errorf("WebLabel() = %q, want %q", got, "web :3080")
	}
}

func TestBarShowsAMessageBeforeTheKeys(t *testing.T) {
	m := New()
	m.width = 160
	m, _ = m.Update(StatusShownElsewhereMsg{Elsewhere: true})
	m, _ = m.Update(ErrorMsg{Text: "import failed"})

	view := m.View()

	message, keys := strings.Index(view, "import failed"), strings.Index(view, "detail")
	if message < 0 || keys < message {
		t.Fatalf("the message should come before the keys, got: %q", view)
	}
}

func TestStatusBarDoesNotOverflow(t *testing.T) {
	m := New()
	m.width = 80
	m, _ = m.Update(HotkeyContextMsg{Registry: hotkey.DefaultRegistry(), Context: hotkey.Context{Page: hotkey.PageAgents}})
	m, _ = m.Update(NtfyMsg{Configured: true})
	m, _ = m.Update(VoiceMsg{Enabled: true})
	m, _ = m.Update(WorkspaceMsg{Name: "Acme", Color: "#4A9EEF"})
	view := m.View()
	// The last rendered line is the content; ensure it fits within the terminal.
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if lipgloss.Width(line) > m.width {
			t.Errorf("status bar line %d wider than terminal: got %d, want <= %d: %q", i, lipgloss.Width(line), m.width, line)
		}
	}
}

// Pending approvals are named on every view, with the key that answers them,
// even when the view shows the sync state itself.
func TestBarCountsPendingApprovals(t *testing.T) {
	m := New()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m.statusElsewhere = true
	if strings.Contains(m.View(), "need") {
		t.Fatalf("bar mentions approvals with none pending: %q", m.View())
	}

	m = m.SetApprovals(2)
	if view := m.View(); !strings.Contains(view, "◆ 2 approvals need you · ! answer") {
		t.Fatalf("bar = %q", view)
	}
	m = m.SetApprovals(1)
	if view := m.View(); !strings.Contains(view, "◆ 1 approval needs you · ! answer") {
		t.Fatalf("bar = %q", view)
	}
	if lipgloss.Width(m.View()) != 140 {
		t.Fatalf("bar width = %d, want 140", lipgloss.Width(m.View()))
	}
}
