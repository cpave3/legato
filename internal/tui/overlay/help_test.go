package overlay

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHelpOverlayEscDismisses(t *testing.T) {
	m := NewHelp(80, 24)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(HelpClosedMsg); !ok {
		t.Fatalf("expected HelpClosedMsg, got %T", msg)
	}
}

func TestHelpOverlayQuestionMarkDismisses(t *testing.T) {
	m := NewHelp(80, 24)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if cmd == nil {
		t.Fatal("? should dismiss help")
	}
	msg := cmd()
	if _, ok := msg.(HelpClosedMsg); !ok {
		t.Fatalf("expected HelpClosedMsg, got %T", msg)
	}
}

func TestHelpOverlayIgnoresOtherKeys(t *testing.T) {
	m := NewHelp(80, 24)
	for _, key := range []rune{'j', 'k', 'h', 'l', 'a', 'z'} {
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if cmd != nil {
			t.Errorf("key %c should be ignored, but got a command", key)
		}
	}
}

func TestHelpOverlayShowsKeybindings(t *testing.T) {
	m := NewHelp(80, 24)
	view := m.View()
	// Navigation section
	for _, expected := range []string{"h/l", "j/k", "g/G", "1-5"} {
		if !strings.Contains(view, expected) {
			t.Errorf("view should contain %q", expected)
		}
	}
	// Actions section
	for _, expected := range []string{"enter", "m", "y", "Y", "/", "esc"} {
		if !strings.Contains(view, expected) {
			t.Errorf("view should contain %q", expected)
		}
	}
	// General section
	for _, expected := range []string{"?", "q"} {
		if !strings.Contains(view, expected) {
			t.Errorf("view should contain %q", expected)
		}
	}
}

func TestHelpOverlayShowsSections(t *testing.T) {
	m := NewHelp(80, 24)
	view := m.View()
	for _, section := range []string{"Navigation", "Actions", "General"} {
		if !strings.Contains(view, section) {
			t.Errorf("view should contain section %q", section)
		}
	}
}
