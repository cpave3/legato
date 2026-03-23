package overlay

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestOpenURLOverlayGitHubKey(t *testing.T) {
	m := NewOpenURL("https://jira.example.com/TEST-1", "https://github.com/o/r/pull/42", 100, 30)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if cmd == nil {
		t.Fatal("expected cmd from g key")
	}
	msg := cmd()
	result, ok := msg.(OpenURLSelectedMsg)
	if !ok {
		t.Fatalf("expected OpenURLSelectedMsg, got %T", msg)
	}
	if result.URL != "https://github.com/o/r/pull/42" {
		t.Errorf("URL = %q, want PR URL", result.URL)
	}
}

func TestOpenURLOverlayJiraKey(t *testing.T) {
	m := NewOpenURL("https://jira.example.com/TEST-1", "https://github.com/o/r/pull/42", 100, 30)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if cmd == nil {
		t.Fatal("expected cmd from j key")
	}
	msg := cmd()
	result, ok := msg.(OpenURLSelectedMsg)
	if !ok {
		t.Fatalf("expected OpenURLSelectedMsg, got %T", msg)
	}
	if result.URL != "https://jira.example.com/TEST-1" {
		t.Errorf("URL = %q, want Jira URL", result.URL)
	}
}

func TestOpenURLOverlayEscCancels(t *testing.T) {
	m := NewOpenURL("https://jira.example.com/TEST-1", "https://github.com/o/r/pull/42", 100, 30)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected cmd from esc")
	}
	msg := cmd()
	if _, ok := msg.(OpenURLCancelledMsg); !ok {
		t.Fatalf("expected OpenURLCancelledMsg, got %T", msg)
	}
}
