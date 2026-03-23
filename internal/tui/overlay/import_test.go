package overlay

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
)

func TestImportOverlayTypingUpdatesQuery(t *testing.T) {
	m := NewImport(100, 30, nil)
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	im := m2.(ImportOverlay)
	if im.Query() != "a" {
		t.Errorf("query = %q, want 'a'", im.Query())
	}
	if cmd == nil {
		t.Fatal("expected ImportQueryChangedMsg cmd")
	}
	msg := cmd()
	if qm, ok := msg.(ImportQueryChangedMsg); !ok || qm.Query != "a" {
		t.Errorf("expected ImportQueryChangedMsg with query 'a', got %T", msg)
	}
}

func TestImportOverlaySetResults(t *testing.T) {
	m := NewImport(100, 30, nil)
	results := []service.RemoteSearchResult{
		{ID: "REX-1", Summary: "Fix bug", Status: "To Do", Priority: "High"},
		{ID: "REX-2", Summary: "Add feature", Status: "In Progress", Priority: "Medium"},
	}
	m = m.SetResults(results)
	view := m.View()
	mustContain(t, view, "REX-1")
	mustContain(t, view, "Fix bug")
	mustContain(t, view, "REX-2")
}

func TestImportOverlaySelectResult(t *testing.T) {
	m := NewImport(100, 30, nil)
	m = m.SetResults([]service.RemoteSearchResult{
		{ID: "REX-1", Summary: "Fix bug"},
		{ID: "REX-2", Summary: "Add feature"},
	})
	// Navigate to second item and select
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	im := m2.(ImportOverlay)
	_, cmd := im.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected cmd from enter")
	}
	msg := cmd()
	result, ok := msg.(ImportSelectedMsg)
	if !ok {
		t.Fatalf("expected ImportSelectedMsg, got %T", msg)
	}
	if result.TicketID != "REX-2" {
		t.Errorf("ticketID = %q, want REX-2", result.TicketID)
	}
}

func TestImportOverlaySelectCarriesWorkspaceID(t *testing.T) {
	wsID := 42
	m := NewImport(100, 30, &wsID)
	m = m.SetResults([]service.RemoteSearchResult{
		{ID: "REX-1", Summary: "Fix bug"},
	})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected cmd from enter")
	}
	msg := cmd()
	result, ok := msg.(ImportSelectedMsg)
	if !ok {
		t.Fatalf("expected ImportSelectedMsg, got %T", msg)
	}
	if result.WorkspaceID == nil || *result.WorkspaceID != 42 {
		t.Errorf("WorkspaceID = %v, want 42", result.WorkspaceID)
	}
}

func TestImportOverlayEscCancels(t *testing.T) {
	m := NewImport(100, 30, nil)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected cmd from esc")
	}
	msg := cmd()
	if _, ok := msg.(ImportCancelledMsg); !ok {
		t.Fatalf("expected ImportCancelledMsg, got %T", msg)
	}
}

func TestImportOverlayBackspace(t *testing.T) {
	m := NewImport(100, 30, nil)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m3, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m4, cmd := m3.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	im := m4.(ImportOverlay)
	if im.Query() != "a" {
		t.Errorf("query after backspace = %q, want 'a'", im.Query())
	}
	if cmd == nil {
		t.Fatal("backspace should emit query changed")
	}
}
