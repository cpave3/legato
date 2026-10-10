package prompt

import (
	"reflect"
	"testing"
)

// The screens below are what the tools drew, captured from real sessions.

const codexUpdatePrompt = `
  Update available · 0.157.1 → 0.162.1
  Release notes: https://github.com/openai/codex/releases/latest

› 1. Update now (runs ` + "`sh -c 'curl -fsSL https://chatgpt.com/codex/install.sh | CODEX_NON_INTERACTIVE=1 sh'`" + `)
  2. Skip
  3. Skip until next version

  enter continue · esc skip
`

const codexApprovalPrompt = `
• Running touch /var/tmp/legato-codex-probe

  Would you like to run the following command?

  Environment: local
  Reason: The sandbox blocked writing to /var/tmp.

  $ touch /var/tmp/legato-codex-probe

  1. Yes, proceed (y)
› 2. Yes, and don't ask again for commands that start with ` + "`touch /var/tmp/legato-codex-probe`" + ` (p)
  3. No, and tell Codex what to do differently (esc)

  Press enter to confirm or esc to cancel
`

const claudeTrustPrompt = `
────────────────────────────────────────────────────────────────────────────
 Accessing workspace:

 /home/me/Projects/new-repo

 Quick safety check: Is this a project you created or one you trust?

 ❯ 1. Yes, I trust this folder
   2. No, exit

 Enter to confirm · Esc to cancel
`

const claudeWrappedOption = `
 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and always allow access to /tmp/claude/scratchpad/spike/log
      from this project
   3. No

 Esc to cancel · Tab to amend
`

func TestAMenuTheToolIsShowingIsReadWithItsOptions(t *testing.T) {
	detection := DetectForAgent("codex", codexUpdatePrompt)

	if !detection.Blocking || detection.State.Type != Menu {
		t.Fatalf("detection = %+v, want a blocking menu", detection)
	}
	want := []Action{
		// The option under the cursor is confirmed as it stands; the others
		// are reached from it, which works in any menu driven by arrows.
		{Label: "Update now (runs `sh -c 'curl -fsSL https://chatgpt.com/codex/install.sh | CODEX_NON_INTERACTIVE=1 sh'`)", Keys: "Enter"},
		{Label: "Skip", Keys: "Down Enter"},
		{Label: "Skip until next version", Keys: "Down Down Enter"},
		{Label: "Cancel (Esc)", Keys: "Escape"},
	}
	if !reflect.DeepEqual(detection.State.Actions, want) {
		t.Fatalf("actions = %#v", detection.State.Actions)
	}
	if got := detection.State.Context; got != "Update available · 0.157.1 → 0.162.1\nRelease notes: https://github.com/openai/codex/releases/latest" {
		t.Fatalf("context = %q", got)
	}
}

func TestMenuOptionsAreReachedFromWhereverTheCursorIs(t *testing.T) {
	state := DetectForAgent("codex", codexApprovalPrompt).State

	if state.Type != Menu || len(state.Actions) != 4 {
		t.Fatalf("state = %+v", state)
	}
	keys := []string{state.Actions[0].Keys, state.Actions[1].Keys, state.Actions[2].Keys}
	if want := []string{"Up Enter", "Enter", "Down Enter"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys = %v, want %v", keys, want)
	}
	// What the menu is about is what stands above it.
	if want := "Reason: The sandbox blocked writing to /var/tmp.\n$ touch /var/tmp/legato-codex-probe"; len(state.Context) < len(want) || state.Context[len(state.Context)-len(want):] != want {
		t.Fatalf("context = %q", state.Context)
	}
}

func TestAMenuIsReadWhicheverToolDrewIt(t *testing.T) {
	state := DetectForAgent("claude-code", claudeTrustPrompt).State

	if state.Type != Menu {
		t.Fatalf("state = %+v", state)
	}
	labels := []string{}
	for _, action := range state.Actions {
		labels = append(labels, action.Label)
	}
	if want := []string{"Yes, I trust this folder", "No, exit", "Cancel (Esc)"}; !reflect.DeepEqual(labels, want) {
		t.Fatalf("labels = %v", labels)
	}
	if want := "Accessing workspace:\n/home/me/Projects/new-repo\nQuick safety check: Is this a project you created or one you trust?"; state.Context != want {
		t.Fatalf("context = %q", state.Context)
	}
}

func TestAnOptionWrappedOntoASecondLineIsReadWhole(t *testing.T) {
	// A tool approval Legato already recognises keeps its own reading; the
	// menu reader is what gets its wrapped option right.
	state, ok := detectMenu(claudeWrappedOption)

	if !ok || len(state.Actions) != 4 {
		t.Fatalf("state = %+v", state)
	}
	if got := state.Actions[1].Label; got != "Yes, and always allow access to /tmp/claude/scratchpad/spike/log from this project" {
		t.Fatalf("second option = %q", got)
	}
	if state.Actions[2].Label != "No" || state.Actions[2].Keys != "Down Down Enter" {
		t.Fatalf("third option = %+v", state.Actions[2])
	}
}

func TestANumberedListThatIsNotAMenuIsLeftAlone(t *testing.T) {
	for name, screen := range map[string]string{
		// A reply that happens to contain a list, above an idle prompt.
		"a list in a reply": "• Here is the plan:\n\n  1. Read the file\n  2. Change it\n  3. Run the tests\n\n› Ask Codex to do anything\n",
		// One numbered line under a cursor is the person typing.
		"a typed line": "› 1. remember to update the docs\n",
		// A list that does not start at one is not a menu's options.
		"a list from the middle": "› 3. Third\n  4. Fourth\n",
	} {
		if state, ok := detectMenu(screen); ok {
			t.Errorf("%s: read as a menu: %+v", name, state)
		}
	}
	if detection := DetectForAgent("codex", "• Here is the plan:\n\n  1. Read the file\n  2. Change it\n\n› Ask Codex to do anything\n"); detection.Blocking || detection.State.Type == Menu {
		t.Fatalf("an idle session was read as waiting: %+v", detection)
	}
}

func TestAMenuWithNoWayToCancelOffersNone(t *testing.T) {
	state, ok := detectMenu("  Pick a theme\n\n› 1. Dark\n  2. Light\n\n  enter to confirm\n")

	if !ok || len(state.Actions) != 2 {
		t.Fatalf("state = %+v", state)
	}
}
