package overlay

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestMoveOverlayRendersColumns(t *testing.T) {
	m := NewMove("REX-1", []string{"Backlog", "Doing", "Review", "Done"}, "Doing")
	view := m.View()
	mustContain(t, view, "Backlog")
	mustContain(t, view, "Doing")
	mustContain(t, view, "Review")
	mustContain(t, view, "Done")
}

func TestMoveOverlaySkipsCurrent(t *testing.T) {
	m := NewMove("REX-1", []string{"Backlog", "Doing", "Review", "Done"}, "Doing")
	// The current column should be visually marked but still shown
	view := m.View()
	mustContain(t, view, "Doing")
}

func TestMoveOverlayNavigation(t *testing.T) {
	m := NewMove("REX-1", []string{"Backlog", "Doing", "Review", "Done"}, "Doing")
	// Should start at cursor 0
	if m.cursor != 0 {
		t.Errorf("initial cursor = %d, want 0", m.cursor)
	}

	// j moves down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m2 := updated.(MoveOverlay)
	if m2.cursor != 1 {
		t.Errorf("after j: cursor = %d, want 1", m2.cursor)
	}

	// k moves up
	updated, _ = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m3 := updated.(MoveOverlay)
	if m3.cursor != 0 {
		t.Errorf("after k: cursor = %d, want 0", m3.cursor)
	}
}

func TestMoveOverlaySelectColumn(t *testing.T) {
	m := NewMove("REX-1", []string{"Backlog", "Doing", "Review", "Done"}, "Doing")
	// Move to "Review" (index 2) and press enter
	m.cursor = 2
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected cmd from enter")
	}
	msg := cmd()
	result, ok := msg.(MoveSelectedMsg)
	if !ok {
		t.Fatalf("expected MoveSelectedMsg, got %T", msg)
	}
	if result.TicketID != "REX-1" {
		t.Errorf("ticketID = %q, want REX-1", result.TicketID)
	}
	if result.TargetColumn != "Review" {
		t.Errorf("targetColumn = %q, want Review", result.TargetColumn)
	}
}

func TestMoveOverlayEscCancels(t *testing.T) {
	m := NewMove("REX-1", []string{"Backlog", "Doing"}, "Doing")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected cmd from esc")
	}
	msg := cmd()
	if _, ok := msg.(MoveCancelledMsg); !ok {
		t.Errorf("expected MoveCancelledMsg, got %T", msg)
	}
}

func mustContain(t *testing.T, s, substr string) {
	t.Helper()
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return
		}
	}
	t.Errorf("view should contain %q", substr)
}
