package hotkey

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDefaultBindingsValidate(t *testing.T) {
	if _, err := NewRegistry(DefaultBindings()); err != nil {
		t.Fatal(err)
	}
}

func TestMoveTaskToWorkspaceIsDiscoverableOnBoardAndDetail(t *testing.T) {
	registry := DefaultRegistry()
	for _, page := range []Page{PageBoard, PageDetail} {
		ctx := Context{Page: page}
		binding, ok := registry.Resolve(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}}, ctx)
		if !ok || binding.Help != "Move task to workspace" {
			t.Fatalf("page %s binding = %+v, found = %v", page, binding, ok)
		}
		found := false
		for _, displayed := range registry.Bindings(ctx, FullHelp) {
			if displayed.ID == binding.ID {
				found = true
			}
		}
		if !found {
			t.Fatalf("page %s workspace move is absent from full help", page)
		}
	}
}
