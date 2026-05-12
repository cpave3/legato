package overlay

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func agentActionWorker() AgentActionOverlay {
	return NewAgentAction("st-abc", "parent-1", "backend")
}

func agentActionConductor() AgentActionOverlay {
	return NewAgentAction("parent-1", "parent-1", "conductor")
}

func updateAgentAction(m AgentActionOverlay, msg tea.Msg) AgentActionOverlay {
	tm, _ := m.Update(msg)
	return tm.(AgentActionOverlay)
}

func TestAgentActionWorkerOptions(t *testing.T) {
	m := agentActionWorker()
	if len(m.options) != 2 {
		t.Fatalf("expected 2 options, got %d", len(m.options))
	}
	if m.options[0].label != "Send message" {
		t.Errorf("option[0] = %q, want Send message", m.options[0].label)
	}
	if m.options[1].label != "Close worker" {
		t.Errorf("option[1] = %q, want Close worker", m.options[1].label)
	}
	if !m.options[1].isDanger {
		t.Error("Close worker should be danger")
	}
}

func TestAgentActionConductorOptions(t *testing.T) {
	m := agentActionConductor()
	if len(m.options) != 2 {
		t.Fatalf("expected 2 options, got %d", len(m.options))
	}
	if m.options[0].label != "Send message" {
		t.Errorf("option[0] = %q, want Send message", m.options[0].label)
	}
	if m.options[1].label != "Finish swarm" {
		t.Errorf("option[1] = %q, want Finish swarm", m.options[1].label)
	}
	if m.options[1].isDanger {
		t.Error("Finish swarm should not be danger")
	}
}

func TestAgentActionMenuCancel(t *testing.T) {
	m := agentActionWorker()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected cmd on esc")
	}
	msg := cmd()
	if _, ok := msg.(AgentActionCancelledMsg); !ok {
		t.Fatalf("expected AgentActionCancelledMsg, got %T", msg)
	}
}

func TestAgentActionMenuNavigation(t *testing.T) {
	m := agentActionWorker()
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.cursor)
	}
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1", m.cursor)
	}
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.cursor != 1 {
		t.Errorf("cursor at bottom = %d, want 1", m.cursor)
	}
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor)
	}
}

func TestAgentActionSelectSendMessage(t *testing.T) {
	m := agentActionWorker()
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != agentActionMessageInput {
		t.Fatalf("mode = %d, want agentActionMessageInput", m.mode)
	}
	view := m.View()
	if !strings.Contains(view, "Send Message") {
		t.Error("expected Send Message heading")
	}
}

func TestAgentActionSelectCloseWorker(t *testing.T) {
	m := agentActionWorker()
	// j to move to Close worker, then enter
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != agentActionConfirm {
		t.Fatalf("mode = %d, want agentActionConfirm", m.mode)
	}
	view := m.View()
	mustContain(t, view, "Close worker")
	mustContain(t, view, "y confirm")
}

func TestAgentActionSelectFinishSwarm(t *testing.T) {
	m := agentActionConductor()
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != agentActionFinishSummary {
		t.Fatalf("mode = %d, want agentActionFinishSummary", m.mode)
	}
	view := m.View()
	mustContain(t, view, "Finish Swarm")
}

func TestAgentActionMessageInputEsc(t *testing.T) {
	m := agentActionWorker()
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyEnter}) // go to input
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != agentActionMenu {
		t.Errorf("mode = %d, want menu", m.mode)
	}
	if m.text != "" {
		t.Errorf("text = %q, want empty", m.text)
	}
}

func TestAgentActionMessageInputAndSend(t *testing.T) {
	m := agentActionWorker()
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyEnter}) // input mode
	// type "hello"
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if m.text != "hi" {
		t.Errorf("text = %q, want hi", m.text)
	}
	// send
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected cmd on enter with non-empty text")
	}
	msg := cmd()
	sent, ok := msg.(AgentMessageSentMsg)
	if !ok {
		t.Fatalf("expected AgentMessageSentMsg, got %T", msg)
	}
	if sent.TaskID != "st-abc" {
		t.Errorf("TaskID = %q, want st-abc", sent.TaskID)
	}
	if sent.ParentTaskID != "parent-1" {
		t.Errorf("ParentTaskID = %q, want parent-1", sent.ParentTaskID)
	}
	if sent.Role != "backend" {
		t.Errorf("Role = %q, want backend", sent.Role)
	}
	if sent.Text != "hi" {
		t.Errorf("Text = %q, want hi", sent.Text)
	}
}

func TestAgentActionConfirmYes(t *testing.T) {
	m := agentActionWorker()
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}) // select Close worker
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyEnter})                     // confirm mode
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd == nil {
		t.Fatal("expected cmd on y")
	}
	msg := cmd()
	closeMsg, ok := msg.(AgentCloseConfirmedMsg)
	if !ok {
		t.Fatalf("expected AgentCloseConfirmedMsg, got %T", msg)
	}
	if closeMsg.TaskID != "st-abc" {
		t.Errorf("TaskID = %q, want st-abc", closeMsg.TaskID)
	}
}

func TestAgentActionConfirmNo(t *testing.T) {
	m := agentActionWorker()
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if m.mode != agentActionMenu {
		t.Errorf("mode = %d, want menu", m.mode)
	}
}

func TestAgentActionFinishSummaryAndSend(t *testing.T) {
	m := agentActionConductor()
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyEnter}) // finish summary mode
	// type "done"
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected cmd on enter")
	}
	msg := cmd()
	finish, ok := msg.(SwarmFinishConfirmedMsg)
	if !ok {
		t.Fatalf("expected SwarmFinishConfirmedMsg, got %T", msg)
	}
	if finish.ParentTaskID != "parent-1" {
		t.Errorf("ParentTaskID = %q, want parent-1", finish.ParentTaskID)
	}
	if finish.Summary != "done" {
		t.Errorf("Summary = %q, want done", finish.Summary)
	}
}

func TestAgentActionFinishSummaryEsc(t *testing.T) {
	m := agentActionConductor()
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != agentActionMenu {
		t.Errorf("mode = %d, want menu", m.mode)
	}
	if m.text != "" {
		t.Errorf("text = %q, want empty", m.text)
	}
}

func TestAgentActionEmptyTextNoSend(t *testing.T) {
	m := agentActionWorker()
	m = updateAgentAction(m, tea.KeyMsg{Type: tea.KeyEnter}) // input mode
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("expected nil cmd when text is empty")
	}
}
