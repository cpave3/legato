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

func TestHelpOverlayShowsWorktreeBindingOnlyWhenEnabled(t *testing.T) {
	if strings.Contains(NewHelpWithMode(80, 24, HelpModeBoard).View(), "Create task worktree") {
		t.Fatal("disabled help shows worktree binding")
	}
	if !strings.Contains(NewHelpWithMode(80, 24, HelpModeBoard).WithWorktrees(true).View(), "Create task worktree") {
		t.Fatal("enabled help omits worktree binding")
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

func TestHelpOverlayShowsShortlistForMode(t *testing.T) {
	tests := []struct {
		name     string
		mode     HelpMode
		title    string
		expected []string
	}{
		{"board", HelpModeBoard, "Board — Quick Reference", []string{"detail", "move", "new"}},
		{"detail", HelpModeDetail, "Detail — Quick Reference", []string{"back", "edit", "copy"}},
		{"agents", HelpModeAgents, "Agents — Quick Reference", []string{"select", "spawn", "kill"}},
		{"report", HelpModeReport, "Report — Quick Reference", []string{"back", "copy"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewHelpWithMode(80, 24, tt.mode)
			view := m.View()
			if !strings.Contains(view, tt.title) {
				t.Errorf("view should contain shortlist title %q, got: %q", tt.title, view)
			}
			for _, expected := range tt.expected {
				if !strings.Contains(view, expected) {
					t.Errorf("view should contain %q in shortlist, got: %q", expected, view)
				}
			}
		})
	}
}
