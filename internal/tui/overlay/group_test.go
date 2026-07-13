package overlay

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func updateGroup(t *testing.T, model GroupOverlay, keys ...tea.KeyMsg) (GroupOverlay, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, key := range keys {
		updated, next := model.Update(key)
		model = updated.(GroupOverlay)
		cmd = next
	}
	return model, cmd
}

func selectedMsg(t *testing.T, cmd tea.Cmd) GroupSelectedMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected selection command")
	}
	msg, ok := cmd().(GroupSelectedMsg)
	if !ok {
		t.Fatalf("expected GroupSelectedMsg, got %T", cmd())
	}
	return msg
}

func TestGroupEnterSelectsCurrentGroup(t *testing.T) {
	model := NewGroup("task-1", "Backend", []string{"Frontend", "Backend"})

	_, cmd := updateGroup(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	msg := selectedMsg(t, cmd)
	if msg.TaskID != "task-1" || msg.Group == nil || *msg.Group != "Backend" {
		t.Fatalf("unexpected selection: %#v", msg)
	}
}

func TestGroupIncludesExplicitUngroupedOption(t *testing.T) {
	model := NewGroup("task-1", "Backend", []string{"Backend"})
	_, cmd := updateGroup(t, model,
		tea.KeyMsg{Type: tea.KeyUp},
		tea.KeyMsg{Type: tea.KeyEnter},
	)

	msg := selectedMsg(t, cmd)
	if msg.Group != nil {
		t.Fatalf("expected nil group, got %#v", msg.Group)
	}
	if !strings.Contains(model.View(), "Ungrouped") {
		t.Fatal("expected visible Ungrouped option")
	}
}

func TestGroupNavigatesOptions(t *testing.T) {
	model := NewGroup("task-1", "", []string{"Frontend", "Backend"})
	_, cmd := updateGroup(t, model,
		tea.KeyMsg{Type: tea.KeyDown},
		tea.KeyMsg{Type: tea.KeyDown},
		tea.KeyMsg{Type: tea.KeyEnter},
	)

	msg := selectedMsg(t, cmd)
	if msg.Group == nil || *msg.Group != "Backend" {
		t.Fatalf("expected Backend, got %#v", msg.Group)
	}
}

func TestGroupTypingEntersAndSubmitsFreeText(t *testing.T) {
	model := NewGroup("task-1", "", nil)
	_, cmd := updateGroup(t, model,
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Platform")},
		tea.KeyMsg{Type: tea.KeySpace},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Team")},
		tea.KeyMsg{Type: tea.KeyEnter},
	)

	msg := selectedMsg(t, cmd)
	if msg.Group == nil || *msg.Group != "Platform Team" {
		t.Fatalf("expected free-text group, got %#v", msg.Group)
	}
}

func TestGroupTypingJKEntersTextInsteadOfNavigating(t *testing.T) {
	model := NewGroup("task-1", "", []string{"Backend"})
	_, cmd := updateGroup(t, model,
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}},
		tea.KeyMsg{Type: tea.KeyEnter},
	)

	msg := selectedMsg(t, cmd)
	if msg.Group == nil || *msg.Group != "jk" {
		t.Fatalf("expected jk custom group, got %#v", msg.Group)
	}
}

func TestGroupTabSwitchesBetweenCustomInputAndOptions(t *testing.T) {
	model := NewGroup("task-1", "", []string{"Backend"})
	model, _ = updateGroup(t, model,
		tea.KeyMsg{Type: tea.KeyTab},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("New")},
		tea.KeyMsg{Type: tea.KeyTab},
	)
	_, cmd := updateGroup(t, model,
		tea.KeyMsg{Type: tea.KeyDown},
		tea.KeyMsg{Type: tea.KeyEnter},
	)

	msg := selectedMsg(t, cmd)
	if msg.Group == nil || *msg.Group != "Backend" {
		t.Fatalf("expected option selection after tab, got %#v", msg.Group)
	}
}

func TestGroupEscapeCancels(t *testing.T) {
	model := NewGroup("task-1", "", nil)
	_, cmd := updateGroup(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected cancellation command")
	}
	if _, ok := cmd().(GroupCancelledMsg); !ok {
		t.Fatalf("expected GroupCancelledMsg, got %T", cmd())
	}
}

func TestGroupViewExplainsKeys(t *testing.T) {
	view := NewGroup("task-1", "", nil).View()
	for _, text := range []string{"tab list/custom", "type custom", "enter apply", "esc cancel"} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected view to contain %q", text)
		}
	}
}
