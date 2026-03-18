package overlay

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
)

func TestSearchOverlayEscDismisses(t *testing.T) {
	m := NewSearch(80, 24)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(SearchCancelledMsg); !ok {
		t.Fatalf("expected SearchCancelledMsg, got %T", msg)
	}
}

func TestSearchOverlayNavigatesResults(t *testing.T) {
	m := NewSearch(80, 24)
	m = m.SetResults([]service.Card{
		{ID: "REX-1", Summary: "First", Status: "Backlog"},
		{ID: "REX-2", Summary: "Second", Status: "Doing"},
		{ID: "REX-3", Summary: "Third", Status: "Review"},
	})
	// Initially at index 0
	if m.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", m.cursor)
	}
	// j moves down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(SearchOverlay)
	if m.cursor != 1 {
		t.Errorf("after j: cursor = %d, want 1", m.cursor)
	}
	// k moves back up
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(SearchOverlay)
	if m.cursor != 0 {
		t.Errorf("after k: cursor = %d, want 0", m.cursor)
	}
}

func TestSearchOverlaySelectResult(t *testing.T) {
	m := NewSearch(80, 24)
	m = m.SetResults([]service.Card{
		{ID: "REX-1", Summary: "First", Status: "Backlog"},
		{ID: "REX-2", Summary: "Second", Status: "Doing"},
	})
	// Move to second result and select
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(SearchOverlay)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should produce a command")
	}
	msg := cmd()
	selected, ok := msg.(SearchSelectedMsg)
	if !ok {
		t.Fatalf("expected SearchSelectedMsg, got %T", msg)
	}
	if selected.CardID != "REX-2" {
		t.Errorf("selected card = %q, want REX-2", selected.CardID)
	}
}

func TestSearchOverlayEnterWithNoResultsDoesNothing(t *testing.T) {
	m := NewSearch(80, 24)
	// No results set
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("enter with no results should not produce a command")
	}
}

func TestSearchOverlayQueryReturned(t *testing.T) {
	m := NewSearch(80, 24)
	if m.Query() != "" {
		t.Errorf("initial query = %q, want empty", m.Query())
	}
}

func TestSearchOverlayView(t *testing.T) {
	m := NewSearch(80, 24)
	m = m.SetResults([]service.Card{
		{ID: "REX-1", Summary: "First", Status: "Backlog"},
	})
	view := m.View()
	if view == "" {
		t.Fatal("view should not be empty")
	}
	if !containsStr(view, "REX-1") {
		t.Error("view should contain result card ID")
	}
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
