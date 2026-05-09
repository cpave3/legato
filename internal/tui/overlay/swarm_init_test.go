package overlay

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSwarmInitPreFillsSuggestedDir(t *testing.T) {
	m := NewSwarmInit("p-1", "Parent task", "/tmp/work")
	if m.workingDir != "/tmp/work" {
		t.Errorf("workingDir = %q, want %q", m.workingDir, "/tmp/work")
	}
}

func TestSwarmInitEnterEmitsStartMsg(t *testing.T) {
	m := NewSwarmInit("p-1", "Parent task", t.TempDir())
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a command on enter")
	}
	msg := cmd()
	got, ok := msg.(SwarmStartMsg)
	if !ok {
		t.Fatalf("got %T, want SwarmStartMsg", msg)
	}
	if got.ParentTaskID != "p-1" {
		t.Errorf("ParentTaskID = %q, want p-1", got.ParentTaskID)
	}
	// Sanity: state preserved on the returned model so subsequent updates work.
	if _, ok := updated.(SwarmInitOverlay); !ok {
		t.Fatalf("returned model = %T, want SwarmInitOverlay", updated)
	}
}

func TestSwarmInitEnterRejectsMissingDir(t *testing.T) {
	m := NewSwarmInit("p-1", "Parent task", "/this/path/should/not/exist/abc123")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		// If a command did run, it had better not be SwarmStartMsg.
		if _, ok := cmd().(SwarmStartMsg); ok {
			t.Fatal("validation passed on a non-existent dir")
		}
	}
	m2 := updated.(SwarmInitOverlay)
	if m2.err == "" {
		t.Error("expected validation error to be set on overlay")
	}
}

func TestSwarmInitEscEmitsCancel(t *testing.T) {
	m := NewSwarmInit("p-1", "Parent task", t.TempDir())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected a command on esc")
	}
	if _, ok := cmd().(SwarmInitCancelledMsg); !ok {
		t.Fatalf("got %T, want SwarmInitCancelledMsg", cmd())
	}
}

func TestSwarmInitTypingAppendsAndClearsError(t *testing.T) {
	m := NewSwarmInit("p-1", "Parent task", "")
	m.err = "stale error"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m2 := updated.(SwarmInitOverlay)
	if m2.workingDir != "a" {
		t.Errorf("workingDir = %q, want %q", m2.workingDir, "a")
	}
	if m2.err != "" {
		t.Errorf("err = %q, want empty after typing", m2.err)
	}
}

func TestSwarmInitBackspaceTrimsAndClearsError(t *testing.T) {
	m := NewSwarmInit("p-1", "Parent task", "abc")
	m.err = "stale error"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m2 := updated.(SwarmInitOverlay)
	if m2.workingDir != "ab" {
		t.Errorf("workingDir = %q, want %q", m2.workingDir, "ab")
	}
	if m2.err != "" {
		t.Errorf("err = %q, want empty after backspace", m2.err)
	}
}
