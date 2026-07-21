package statusbar

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	m, _ = m.Update(ModeMsg{Mode: ModeDetail})
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
	m, _ = m.Update(ModeMsg{Mode: ModeAgents})
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
	m, _ = m.Update(ModeMsg{Mode: ModeAgents})
	m, _ = m.Update(NtfyMsg{Configured: true})
	view := m.View()
	if !strings.Contains(view, "notify") {
		t.Errorf("agents mode should contain 'notify' when ntfy configured, got: %q", view)
	}
}

func TestAgentsModeVoiceHint(t *testing.T) {
	m := New()
	m.width = 120
	m, _ = m.Update(ModeMsg{Mode: ModeAgents})
	m, _ = m.Update(VoiceMsg{Enabled: true})
	view := m.View()
	if !strings.Contains(view, "voice") {
		t.Errorf("agents mode should contain 'voice' when voice enabled, got: %q", view)
	}
}

func TestReportModeHints(t *testing.T) {
	m := New()
	m.width = 120
	m, _ = m.Update(ModeMsg{Mode: ModeReport})
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
	m, _ = m.Update(ModeMsg{Mode: ModeReview})
	view := m.View()
	for _, expected := range []string{"open", "reviewed", "ask", "delete"} {
		if !strings.Contains(view, expected) {
			t.Errorf("review mode should contain %q, got: %q", expected, view)
		}
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

func TestStatusBarContentStaysSingleLine(t *testing.T) {
	m := New()
	m.width = 120
	m, _ = m.Update(ModeMsg{Mode: ModeAgents})
	m, _ = m.Update(NtfyMsg{Configured: true})
	m, _ = m.Update(VoiceMsg{Enabled: true})
	view := m.View()
	// The status bar renders a top border plus one content line. Wrapping the
	// content would produce more than 2 total lines.
	lines := strings.Split(view, "\n")
	if len(lines) > 2 {
		t.Errorf("status bar should be border + one content line, got %d lines: %q", len(lines), view)
	}
}

func TestStatusBarDoesNotOverflow(t *testing.T) {
	m := New()
	m.width = 80
	m, _ = m.Update(ModeMsg{Mode: ModeAgents})
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
