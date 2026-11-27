package overlay

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// These tests pin the dynamic shortcuts of the move, workspace and
// move-workspace overlays through their constructors, Update and View: which
// key each row shows, and that pressing it chooses that row. They pin today's
// quirks too, such as a fallback digit taking over a digit a label's first
// character claimed (not fixed here; that needs its own decision).

// shortcutProbeKeys are the keys pressed to learn what an overlay dispatches.
var shortcutProbeKeys = []rune("abcdefghijklmnopqrstuvwxyz0123456789ãéÉ")

// renderedShortcuts reads the shortcut shown on each of the first n item rows
// of an overlay's View. Item rows follow the border, a blank line, the title
// and another blank line.
func renderedShortcuts(t *testing.T, view string, n int) []string {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) < 4+n {
		t.Fatalf("view has %d lines, want at least %d:\n%s", len(lines), 4+n, view)
	}
	shown := make([]string, n)
	for i := 0; i < n; i++ {
		row := strings.TrimPrefix(lines[4+i], "│")
		// Two spaces of padding, then the "> " or "  " cursor prefix.
		row = strings.TrimPrefix(row, "  ")
		if len(row) < 2 {
			t.Fatalf("item row %d is too short: %q", i, lines[4+i])
		}
		row = row[2:]
		end := strings.Index(row, "  ")
		if end < 0 {
			t.Fatalf("item row %d has no shortcut gap: %q", i, lines[4+i])
		}
		shown[i] = row[:end]
	}
	return shown
}

// assertShownMatchesDispatch checks that every shown shortcut dispatches its
// own row, and that every dispatching key is shown on a row it dispatches.
// same reports whether row i is what a key's dispatch chose.
func assertShownMatchesDispatch(t *testing.T, shown []string, dispatch map[rune]int, same func(row, chosen int) bool) {
	t.Helper()
	for row, key := range shown {
		if key == "" {
			continue
		}
		chosen, ok := dispatch[[]rune(key)[0]]
		if !ok {
			t.Errorf("row %d shows %q, which dispatches nothing", row, key)
			continue
		}
		if !same(row, chosen) {
			t.Errorf("row %d shows %q, which dispatches row %d", row, key, chosen)
		}
	}
	for key, chosen := range dispatch {
		found := false
		for row, s := range shown {
			if s == string(key) && same(row, chosen) {
				found = true
			}
		}
		if !found {
			t.Errorf("key %q dispatches row %d but no such row shows it (shown %q)", key, chosen, shown)
		}
	}
}

func moveDispatch(m MoveOverlay) map[rune]string {
	chosen := map[rune]string{}
	for _, key := range shortcutProbeKeys {
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if cmd == nil {
			continue
		}
		if msg, ok := cmd().(MoveSelectedMsg); ok {
			chosen[key] = msg.TargetColumn
		}
	}
	return chosen
}

func TestMoveOverlayShortcutsShownAndDispatched(t *testing.T) {
	cases := []struct {
		name    string
		columns []string
		want    []string // shortcut shown per row
	}{
		{
			name:    "first letters, case collisions, reserved letters, empty label, digit overwrite, byte-first Unicode",
			columns: []string{"Workspace", "Backlog", "bravo", "", "Done", "2 Later", "Queue", "Été", "Jump", "Keep"},
			// w, q, j, k are reserved in the move overlay; "bravo" collides
			// with "Backlog"; "" gets a digit; "2 Later" claims '2' by its
			// first character, then loses it to "bravo"'s fallback and takes
			// '4'; "Été" is keyed by its first byte, lowercased.
			want: []string{"1", "b", "2", "3", "d", "4", "5", "ã", "6", "7"},
		},
		{
			name:    "more than nine fallbacks leave the rest without a shortcut",
			columns: []string{"x1", "x2", "x3", "x4", "x5", "x6", "x7", "x8", "x9", "x10", "x11", "x12"},
			want:    []string{"x", "1", "2", "3", "4", "5", "6", "7", "8", "9", "", ""},
		},
		{
			name:    "duplicate names share one shortcut",
			columns: []string{"Done", "Backlog", "Done"},
			want:    []string{"d", "b", "d"},
		},
		{
			name: "a duplicate name whose digit was overwritten takes a new digit",
			// "2x" claims '2'; "ac"'s fallback overwrites it; the second "2x"
			// then finds the name unassigned and takes '3', which both rows show.
			columns: []string{"2x", "a", "ab", "ac", "2x"},
			want:    []string{"3", "a", "1", "2", "3"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The current column is none of them, so every row shows its key.
			m := NewMove("T-1", tc.columns, "not-a-column")
			shown := renderedShortcuts(t, m.View(), len(tc.columns))
			if !reflect.DeepEqual(shown, tc.want) {
				t.Fatalf("shown shortcuts = %q, want %q", shown, tc.want)
			}
			byName := moveDispatch(m)
			dispatch := map[rune]int{}
			for key, name := range byName {
				dispatch[key] = -1
				for i, col := range tc.columns {
					if col == name {
						dispatch[key] = i
						break
					}
				}
			}
			assertShownMatchesDispatch(t, shown, dispatch, func(row, chosen int) bool {
				return chosen >= 0 && tc.columns[row] == tc.columns[chosen]
			})
		})
	}
}

func workspaceDispatch(m WorkspaceOverlay) map[rune]int {
	chosen := map[rune]int{}
	for _, key := range shortcutProbeKeys {
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if cmd == nil {
			continue
		}
		msg, ok := cmd().(WorkspaceSelectedMsg)
		if !ok {
			continue
		}
		chosen[key] = -1
		for i, e := range m.entries {
			if e.view == msg.View {
				chosen[key] = i
			}
		}
	}
	return chosen
}

func moveWorkspaceDispatch(m MoveWorkspaceOverlay) map[rune]int {
	chosen := map[rune]int{}
	for _, key := range shortcutProbeKeys {
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if cmd == nil {
			continue
		}
		msg, ok := cmd().(WorkspaceAssignedMsg)
		if !ok {
			continue
		}
		chosen[key] = -1
		for i, e := range m.entries {
			if (e.id == nil && msg.WorkspaceID == nil) || (e.id != nil && msg.WorkspaceID != nil && *e.id == *msg.WorkspaceID) {
				chosen[key] = i
			}
		}
	}
	return chosen
}

func workspacesNamed(names ...string) []service.Workspace {
	ws := make([]service.Workspace, len(names))
	for i, name := range names {
		ws[i] = service.Workspace{ID: i + 1, Name: name}
	}
	return ws
}

var workspaceShortcutCases = []struct {
	name  string
	names []string
	// shortcut shown per workspace row, after the overlay's own leading rows
	wantWorkspace     []string // after "All" (a) and "Unassigned" (u)
	wantMoveWorkspace []string // after "None" (n)
}{
	{
		name:  "first letters, case collisions, reserved letters, empty label, digit overwrite, byte-first Unicode",
		names: []string{"Work", "work", "Personal", "", "2026 Q1", "Été", "Queue", "Jump", "Keep"},
		// q, j, k are reserved in both overlays, w in neither. "work"
		// collides with "Work". "" gets a digit. "2026 Q1" claims '2' by its
		// first character, then loses it to the empty label's fallback and
		// takes '3'. "Été" is keyed by its first byte, lowercased.
		wantWorkspace:     []string{"w", "1", "p", "2", "3", "ã", "4", "5", "6"},
		wantMoveWorkspace: []string{"w", "1", "p", "2", "3", "ã", "4", "5", "6"},
	},
	{
		name:              "more than nine fallbacks leave the rest without a shortcut",
		names:             []string{"x1", "x2", "x3", "x4", "x5", "x6", "x7", "x8", "x9", "x10", "x11", "x12"},
		wantWorkspace:     []string{"x", "1", "2", "3", "4", "5", "6", "7", "8", "9", "", ""},
		wantMoveWorkspace: []string{"x", "1", "2", "3", "4", "5", "6", "7", "8", "9", "", ""},
	},
	{
		name:  "workspaces colliding with the overlay's own rows",
		names: []string{"Alpha", "Upstream", "Nebula"},
		// "Alpha" and "Upstream" collide with "All" and "Unassigned" in the
		// workspace switcher; "Nebula" with "None" in the assignment overlay.
		wantWorkspace:     []string{"1", "2", "n"},
		wantMoveWorkspace: []string{"a", "u", "1"},
	},
}

func TestWorkspaceOverlayShortcutsShownAndDispatched(t *testing.T) {
	for _, tc := range workspaceShortcutCases {
		t.Run(tc.name, func(t *testing.T) {
			// The current view is none of the rows, so every row shows its key.
			m := NewWorkspace(workspacesNamed(tc.names...), store.WorkspaceView{Kind: store.ViewWorkspace, WorkspaceID: 999})
			shown := renderedShortcuts(t, m.View(), len(m.entries))
			want := append([]string{"a", "u"}, tc.wantWorkspace...)
			if !reflect.DeepEqual(shown, want) {
				t.Fatalf("shown shortcuts = %q, want %q", shown, want)
			}
			assertShownMatchesDispatch(t, shown, workspaceDispatch(m), func(row, chosen int) bool { return row == chosen })
		})
	}
}

func TestMoveWorkspaceOverlayShortcutsShownAndDispatched(t *testing.T) {
	for _, tc := range workspaceShortcutCases {
		t.Run(tc.name, func(t *testing.T) {
			current := 999
			m := NewMoveWorkspace("T-1", workspacesNamed(tc.names...), &current)
			shown := renderedShortcuts(t, m.View(), len(m.entries))
			want := append([]string{"n"}, tc.wantMoveWorkspace...)
			if !reflect.DeepEqual(shown, want) {
				t.Fatalf("shown shortcuts = %q, want %q", shown, want)
			}
			assertShownMatchesDispatch(t, shown, moveWorkspaceDispatch(m), func(row, chosen int) bool { return row == chosen })
		})
	}
}

// No digit is reserved in the move or workspace overlays today, so a reserved
// digit cannot be reached through them; this pins the fallback's skip on the
// shared assignment directly.
func TestAssignShortcutsFallbackSkipsReservedDigits(t *testing.T) {
	labels := []string{"a", "ab", "ac", "ad"}
	reserved := func(key rune) bool { return key == '1' || key == '3' }
	got := assignShortcuts(labels, byIndex, reserved)
	want := map[rune]int{'a': 0, '2': 1, '4': 2, '5': 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("shortcuts = %v, want %v", got, want)
	}

	allDigits := func(key rune) bool { return key >= '1' && key <= '9' }
	got = assignShortcuts(labels, byIndex, allDigits)
	if want := map[rune]int{'a': 0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("with every digit reserved, shortcuts = %v, want %v", got, want)
	}
}
