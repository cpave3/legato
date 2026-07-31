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
