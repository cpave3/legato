package overlay

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func TestWorkspaceOverlay_SelectAll(t *testing.T) {
	ws := []service.Workspace{
		{ID: 1, Name: "Work", Color: "#4A9EEF"},
		{ID: 2, Name: "Personal", Color: "#7BC47F"},
	}
	current := store.WorkspaceView{Kind: store.ViewWorkspace, WorkspaceID: 1}
	m := NewWorkspace(ws, current)

	// Cursor should be on "Work" (current)
	if m.cursor != 2 {
		t.Errorf("cursor = %d, want 2 (Work)", m.cursor)
	}

	// Navigate up to "All" and select
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(WorkspaceOverlay)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(WorkspaceOverlay)
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 (All)", m.cursor)
	}

	// Enter to select
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command")
	}
	msg := cmd()
	sel, ok := msg.(WorkspaceSelectedMsg)
	if !ok {
		t.Fatalf("expected WorkspaceSelectedMsg, got %T", msg)
	}
	if sel.View.Kind != store.ViewAll {
		t.Errorf("view kind = %d, want ViewAll", sel.View.Kind)
	}
}

func TestWorkspaceOverlay_SelectWorkspace(t *testing.T) {
	ws := []service.Workspace{
		{ID: 1, Name: "Work"},
		{ID: 2, Name: "Personal"},
	}
	current := store.WorkspaceView{Kind: store.ViewAll}
	m := NewWorkspace(ws, current)

	// Use shortcut 'p' for Personal
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if cmd == nil {
		t.Fatal("expected command from shortcut")
	}
	msg := cmd()
	sel, ok := msg.(WorkspaceSelectedMsg)
	if !ok {
		t.Fatalf("expected WorkspaceSelectedMsg, got %T", msg)
	}
	if sel.View.Kind != store.ViewWorkspace || sel.View.WorkspaceID != 2 {
		t.Errorf("view = %+v, want ViewWorkspace(2)", sel.View)
	}
}

func TestWorkspaceOverlay_Dismiss(t *testing.T) {
	m := NewWorkspace(nil, store.WorkspaceView{Kind: store.ViewAll})

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected command")
	}
	msg := cmd()
	if _, ok := msg.(WorkspaceCancelledMsg); !ok {
		t.Fatalf("expected WorkspaceCancelledMsg, got %T", msg)
	}
}

func TestWorkspaceOverlay_JKNavigation(t *testing.T) {
	ws := []service.Workspace{{ID: 1, Name: "Work"}}
	m := NewWorkspace(ws, store.WorkspaceView{Kind: store.ViewAll})

	// Start at cursor=0 (All, which is current)
	if m.cursor != 0 {
		t.Errorf("initial cursor = %d, want 0", m.cursor)
	}

	// j to move down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(WorkspaceOverlay)
	if m.cursor != 1 {
		t.Errorf("after j: cursor = %d, want 1", m.cursor)
	}

	// j again
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(WorkspaceOverlay)
	if m.cursor != 2 {
		t.Errorf("after j: cursor = %d, want 2", m.cursor)
	}

	// j at bottom stays
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(WorkspaceOverlay)
	if m.cursor != 2 {
		t.Errorf("after j at bottom: cursor = %d, want 2", m.cursor)
	}

	// k to move up
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(WorkspaceOverlay)
	if m.cursor != 1 {
		t.Errorf("after k: cursor = %d, want 1", m.cursor)
	}
}
