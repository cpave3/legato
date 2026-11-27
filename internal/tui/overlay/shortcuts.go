package overlay

import (
	"unicode"

	"github.com/cpave3/legato/internal/tui/hotkey"
)

// reservedIn reports whether a dynamic key would collide with a fixed binding
// of the overlay ctx names (docs/ai/dev-notes.md: content-derived letters
// query Registry.IsReserved for their overlay scope).
func reservedIn(ctx hotkey.Context) func(rune) bool {
	registry := hotkey.DefaultRegistry()
	return func(key rune) bool {
		_, found := registry.IsReserved(hotkey.Rune(key), ctx)
		return found
	}
}

// assignShortcuts gives the items of a list overlay their one-key shortcuts,
// as the move, workspace and move-workspace overlays do.
//
// labels are the items' labels in list order, and id(i) is what item i is
// known by: the shortcut map's values are ids, and items with equal ids share
// one shortcut (the move overlay keys columns by name, the workspace overlays
// rows by index).
//
// Each label's first byte, lowercased, becomes its item's key unless an
// earlier item took it or reserved says a fixed binding has it; an empty label
// gets no letter. Then each item with no key, in list order, gets the next of
// '1'..'9' that is not reserved, while any are left. That fallback does not
// look at the keys first letters took: a digit-leading label's key can be
// overwritten by a later item's fallback, and the label then gets a digit of
// its own if one is left when its turn comes.
func assignShortcuts[K comparable](labels []string, id func(i int) K, reserved func(rune) bool) map[rune]K {
	shortcuts := make(map[rune]K)
	used := make(map[rune]bool)

	for i, label := range labels {
		if len(label) == 0 {
			continue
		}
		key := unicode.ToLower(rune(label[0]))
		if !used[key] && !reserved(key) {
			shortcuts[key] = id(i)
			used[key] = true
		}
	}

	num := '1'
	for i := range labels {
		assigned := false
		for _, v := range shortcuts {
			if v == id(i) {
				assigned = true
				break
			}
		}
		for num <= '9' && reserved(num) {
			num++
		}
		if !assigned && num <= '9' {
			shortcuts[num] = id(i)
			num++
		}
	}

	return shortcuts
}

// shortcutLabel is the key shortcuts gives the item known by id, or "" when it
// has none.
func shortcutLabel[K comparable](shortcuts map[rune]K, id K) string {
	for k, v := range shortcuts {
		if v == id {
			return string(k)
		}
	}
	return ""
}

// byIndex makes an item known by its index in the list.
func byIndex(i int) int { return i }
