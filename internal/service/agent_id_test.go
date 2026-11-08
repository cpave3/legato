package service

import "testing"

func TestAnAgentIDSplitsIntoItsTaskAndSlot(t *testing.T) {
	for _, tc := range []struct{ id, task, slot string }{
		{"abc12345", "abc12345", "main"},
		{"abc12345_reviewer", "abc12345", "reviewer"},
		{"LEG-42", "LEG-42", "main"},
		{"LEG-42_reviewer", "LEG-42", "reviewer"},
		{"_reviewer", "_reviewer", "main"},
		{"a_Not_A_Slot", "a_Not_A_Slot", "main"},
	} {
		if task, slot := SplitAgentID(tc.id); task != tc.task || slot != tc.slot {
			t.Errorf("SplitAgentID(%q) = %q, %q; want %q, %q", tc.id, task, slot, tc.task, tc.slot)
		}
	}
}
