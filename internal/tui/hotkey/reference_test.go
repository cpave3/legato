package hotkey

import (
	"flag"
	"os"
	"strings"
	"testing"
)

var updateReference = flag.Bool("update", false, "rewrite the keyboard reference page of the docs site")

// The docs site's keyboard page is written from the registry, so it lists
// the keys the TUI dispatches and nothing else.
const referencePage = "../../../site/src/content/docs/reference/keyboard.md"

func TestKeyboardReferenceListsKeysByPage(t *testing.T) {
	page := keyboardReference(DefaultBindings())

	for _, want := range []string{
		"## Everywhere\n",
		"| `q/ctrl+c` | Quit Legato |",
		"## Board\n",
		"| `m` | Move task |",
		"| `enter` | Open task detail |",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("keyboard reference lacks %q", want)
		}
	}
	if strings.Index(page, "## Everywhere") > strings.Index(page, "## Board") {
		t.Error("keys that work everywhere should come before the board's")
	}
}

func TestKeyboardReferencePageIsCurrent(t *testing.T) {
	want := keyboardReference(DefaultBindings())
	if *updateReference {
		if err := os.WriteFile(referencePage, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(referencePage)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/tui/hotkey -update)", err)
	}
	if string(got) != want {
		t.Fatalf("%s is out of date with the hotkey registry (run go test ./internal/tui/hotkey -update)", referencePage)
	}
}
