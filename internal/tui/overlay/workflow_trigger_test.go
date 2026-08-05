package overlay

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestWorkflowConfirmationEnterConfirmsRequest(t *testing.T) {
	model := NewWorkflowConfirmation("wtr-1", "Close active agent")
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected confirm command")
	}
	msg, ok := cmd().(WorkflowTriggerConfirmMsg)
	if !ok || msg.RequestID != "wtr-1" {
		t.Fatalf("message = %#v", msg)
	}
}

func TestWorkflowConfirmationEscapeLeavesRequestPending(t *testing.T) {
	model := NewWorkflowConfirmation("wtr-1", "Close active agent")
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if _, ok := cmd().(WorkflowTriggerCancelledMsg); !ok {
		t.Fatalf("message = %#v", cmd())
	}
}
