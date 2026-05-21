package overlay

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSwarmCancelOverlayShowsTitle(t *testing.T) {
	m := NewSwarmCancel("TASK-1", "Fix login bug")
	view := m.View()
	mustContain(t, view, "Fix login bug")
	mustContain(t, view, "Cancel swarm")
	mustContain(t, view, "cannot be undone")
}

func TestSwarmCancelOverlayConfirm(t *testing.T) {
	m := NewSwarmCancel("TASK-1", "Fix login bug")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd == nil {
		t.Fatal("expected cmd from y")
	}
	msg := cmd()
	result, ok := msg.(SwarmCancelConfirmedMsg)
	if !ok {
		t.Fatalf("expected SwarmCancelConfirmedMsg, got %T", msg)
	}
	if result.ParentTaskID != "TASK-1" {
		t.Errorf("ParentTaskID = %q, want TASK-1", result.ParentTaskID)
	}
}

func TestSwarmCancelOverlayCancel_N(t *testing.T) {
	m := NewSwarmCancel("TASK-1", "Fix login bug")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if cmd == nil {
		t.Fatal("expected cmd from n")
	}
	msg := cmd()
	if _, ok := msg.(SwarmCancelCancelledMsg); !ok {
		t.Fatalf("expected SwarmCancelCancelledMsg, got %T", msg)
	}
}

func TestSwarmCancelOverlayCancel_Esc(t *testing.T) {
	m := NewSwarmCancel("TASK-1", "Fix login bug")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected cmd from esc")
	}
	msg := cmd()
	if _, ok := msg.(SwarmCancelCancelledMsg); !ok {
		t.Fatalf("expected SwarmCancelCancelledMsg, got %T", msg)
	}
}

func TestSwarmCancelOverlayCancel_Q(t *testing.T) {
	m := NewSwarmCancel("TASK-1", "Fix login bug")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("expected cmd from q")
	}
	msg := cmd()
	if _, ok := msg.(SwarmCancelCancelledMsg); !ok {
		t.Fatalf("expected SwarmCancelCancelledMsg, got %T", msg)
	}
}

func TestSwarmCancelOverlayWindowSize(t *testing.T) {
	m := NewSwarmCancel("TASK-1", "Fix login bug")
	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if cmd != nil {
		t.Fatal("WindowSizeMsg should not produce a command")
	}
	m2 := updated.(SwarmCancelOverlay)
	if m2.width != 120 {
		t.Errorf("width = %d, want 120", m2.width)
	}
	if m2.height != 40 {
		t.Errorf("height = %d, want 40", m2.height)
	}
}
