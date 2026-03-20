package overlay

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDeleteOverlayShowsTaskInfo(t *testing.T) {
	m := NewDelete("T-1", "Fix login bug", false)
	view := m.View()
	mustContain(t, view, "T-1")
	mustContain(t, view, "Fix login bug")
	mustContain(t, view, "Delete")
}

func TestDeleteOverlayShowsRemoteWarning(t *testing.T) {
	m := NewDelete("REX-123", "Remote task", true)
	view := m.View()
	mustContain(t, view, "remote")
}

func TestDeleteOverlayLocalNoRemoteWarning(t *testing.T) {
	m := NewDelete("T-1", "Local task", false)
	view := m.View()
	// Should not mention remote
	for i := 0; i <= len(view)-len("remote"); i++ {
		if view[i:i+len("remote")] == "remote" {
			t.Error("local task delete should not mention remote")
			return
		}
	}
}

func TestDeleteOverlayConfirm(t *testing.T) {
	m := NewDelete("T-1", "Fix login bug", false)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd == nil {
		t.Fatal("expected cmd from y")
	}
	msg := cmd()
	result, ok := msg.(DeleteConfirmedMsg)
	if !ok {
		t.Fatalf("expected DeleteConfirmedMsg, got %T", msg)
	}
	if result.TaskID != "T-1" {
		t.Errorf("taskID = %q, want T-1", result.TaskID)
	}
}

func TestDeleteOverlayCancel_N(t *testing.T) {
	m := NewDelete("T-1", "Fix login bug", false)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if cmd == nil {
		t.Fatal("expected cmd from n")
	}
	msg := cmd()
	if _, ok := msg.(DeleteCancelledMsg); !ok {
		t.Fatalf("expected DeleteCancelledMsg, got %T", msg)
	}
}

func TestDeleteOverlayCancel_Esc(t *testing.T) {
	m := NewDelete("T-1", "Fix login bug", false)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected cmd from esc")
	}
	msg := cmd()
	if _, ok := msg.(DeleteCancelledMsg); !ok {
		t.Fatalf("expected DeleteCancelledMsg, got %T", msg)
	}
}
