package overlay

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
)

func TestMoveWorkspaceOverlay_SelectWorkspace(t *testing.T) {
	ws := []service.Workspace{
		{ID: 1, Name: "Work"},
		{ID: 2, Name: "Personal"},
	}
	m := NewMoveWorkspace("t-1", ws, nil) // currently unassigned

	// Cursor starts at "None" (current)
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 (None)", m.cursor)
	}

	// Navigate to "Work" and select
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(MoveWorkspaceOverlay)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command")
	}
	msg := cmd()
	assigned, ok := msg.(WorkspaceAssignedMsg)
	if !ok {
		t.Fatalf("expected WorkspaceAssignedMsg, got %T", msg)
	}
	if assigned.TaskID != "t-1" {
		t.Errorf("taskID = %q, want t-1", assigned.TaskID)
	}
	if assigned.WorkspaceID == nil || *assigned.WorkspaceID != 1 {
		t.Errorf("workspaceID = %v, want 1", assigned.WorkspaceID)
	}
}

func TestMoveWorkspaceOverlay_SelectNone(t *testing.T) {
	ws := []service.Workspace{{ID: 1, Name: "Work"}}
	currentID := 1
	m := NewMoveWorkspace("t-1", ws, &currentID) // currently in Work

	// Use shortcut 'n' for None
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if cmd == nil {
		t.Fatal("expected command from shortcut")
	}
	msg := cmd()
	assigned, ok := msg.(WorkspaceAssignedMsg)
	if !ok {
		t.Fatalf("expected WorkspaceAssignedMsg, got %T", msg)
	}
	if assigned.WorkspaceID != nil {
		t.Errorf("workspaceID = %v, want nil", assigned.WorkspaceID)
	}
}

func TestMoveWorkspaceOverlay_SelectCurrentCancels(t *testing.T) {
	ws := []service.Workspace{{ID: 1, Name: "Work"}}
	currentID := 1
	m := NewMoveWorkspace("t-1", ws, &currentID)

	// Cursor should be on "Work" (current)
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1 (Work)", m.cursor)
	}

	// Enter on current → cancel
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command")
	}
	msg := cmd()
	if _, ok := msg.(WorkspaceAssignCancelledMsg); !ok {
		t.Fatalf("expected WorkspaceAssignCancelledMsg, got %T", msg)
	}
}

func TestMoveWorkspaceOverlay_Dismiss(t *testing.T) {
	m := NewMoveWorkspace("t-1", nil, nil)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected command")
	}
	msg := cmd()
	if _, ok := msg.(WorkspaceAssignCancelledMsg); !ok {
		t.Fatalf("expected WorkspaceAssignCancelledMsg, got %T", msg)
	}
}

func TestMoveOverlay_WOpensWorkspace(t *testing.T) {
	m := NewMove("t-1", []string{"Backlog", "Done"}, "Backlog")
	m.width = 80
	m.height = 40

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	if cmd == nil {
		t.Fatal("expected command from w key")
	}
	msg := cmd()
	mw, ok := msg.(OpenMoveWorkspaceMsg)
	if !ok {
		t.Fatalf("expected OpenMoveWorkspaceMsg, got %T", msg)
	}
	if mw.TaskID != "t-1" {
		t.Errorf("taskID = %q, want t-1", mw.TaskID)
	}
}
