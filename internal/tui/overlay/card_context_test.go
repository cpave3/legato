package overlay

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCardContextBasicActionsReuseAppIntents(t *testing.T) {
	for index, want := range []CardContextAction{CardContextOpen, CardContextMove, CardContextDelete} {
		model := NewCardContext("task-1")
		for range index {
			updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
			model = updated.(CardContextOverlay)
		}
		_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			t.Fatalf("action %d emitted no command", index)
		}
		msg, ok := cmd().(CardContextActionMsg)
		if !ok || msg.TaskID != "task-1" || msg.Action != want {
			t.Fatalf("action %d message = %#v", index, msg)
		}
	}
}
