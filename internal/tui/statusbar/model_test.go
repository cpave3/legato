package statusbar

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
	if !strings.Contains(view, "h/l") {
		t.Errorf("view should contain key hint h/l, got: %q", view)
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

func TestNarrowTerminalTruncatesHints(t *testing.T) {
	m := New()
	m.width = 30 // very narrow
	view := m.View()
	// Should still show sync state
	if !strings.Contains(view, "offline") {
		t.Errorf("narrow view should still show sync state, got: %q", view)
	}
}
