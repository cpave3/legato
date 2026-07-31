package overlay

import "testing"

func TestTextEntryOverlaysConsumePrintableGlobalKeys(t *testing.T) {
	if !ConsumesPrintableInput(NewSearch(80, 24)) {
		t.Fatal("search input should consume printable keys")
	}
	if ConsumesPrintableInput(NewMove("task", []string{"Ready"}, "Backlog")) {
		t.Fatal("move menu should yield printable global help key")
	}
}

func TestMixedModeOverlaysConsumeOnlyFocusedTextInput(t *testing.T) {
	action := NewAgentAction("task", "parent", "builder")
	if ConsumesPrintableInput(action) {
		t.Fatal("agent action menu should yield printable globals")
	}
	action.mode = agentActionMessageInput
	if !ConsumesPrintableInput(action) {
		t.Fatal("agent action message input should consume printable globals")
	}

	plan := PlanApprovalOverlay{}
	if ConsumesPrintableInput(plan) {
		t.Fatal("plan review should yield printable globals")
	}
	plan.mode = planModeRejecting
	if !ConsumesPrintableInput(plan) {
		t.Fatal("plan rejection input should consume printable globals")
	}

	link := LinkPROverlay{phase: phaseConfirm}
	if ConsumesPrintableInput(link) {
		t.Fatal("PR confirmation should yield printable globals")
	}
	link.phase = phaseInput
	if !ConsumesPrintableInput(link) {
		t.Fatal("PR input should consume printable globals")
	}
}

func TestMoveShortcutsReserveFixedOverlayControls(t *testing.T) {
	shortcuts := buildShortcuts([]string{"Queue", "Workspace", "Jump", "Keep"})
	for _, reserved := range []rune{'q', 'w', 'j', 'k'} {
		if column, ok := shortcuts[reserved]; ok {
			t.Fatalf("reserved key %q assigned to %q", reserved, column)
		}
	}
	if len(shortcuts) != 4 {
		t.Fatalf("fallback keys did not assign every column: %#v", shortcuts)
	}
}
