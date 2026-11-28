package prompt

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The screens under testdata/claude-2.1.332 were captured with
// `tmux capture-pane -p` from a real Claude Code 2.1.332 session (LEG-688).

func claudeScreen(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "claude-2.1.332", name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func claudeState(t *testing.T, name string) PromptState {
	t.Helper()
	detection := DetectForAgent("claude-code", claudeScreen(t, name))
	if !detection.Blocking {
		t.Fatalf("%s: not read as waiting: %+v", name, detection)
	}
	return detection.State
}

func TestAClaudeQuestionIsReadWithItsOptionsAndItsTextRow(t *testing.T) {
	state := claudeState(t, "single-question")

	if state.Type != Menu {
		t.Fatalf("type = %q", state.Type)
	}
	want := []Action{
		{Label: "Red Red", Keys: "Enter"},
		{Label: "Green Green", Keys: "Down Enter"},
		{Label: "Blue Blue", Keys: "Down Down Enter"},
		// Typing on this row replaces it with the text; Enter then answers.
		{Label: "Type something.", Keys: "Down Down Down Enter", TakesText: true, TextSubmits: true},
		{Label: "Chat about this", Keys: "Down Down Down Down Enter"},
		{Label: "Cancel (Esc)", Keys: "Escape"},
	}
	if !reflect.DeepEqual(state.Actions, want) {
		t.Fatalf("actions = %#v", state.Actions)
	}
	if state.Context != "☐ Colour\nWhich colour?" {
		t.Fatalf("context = %q", state.Context)
	}
}

func TestEachTabOfAClaudeFormIsReadInTurn(t *testing.T) {
	first := claudeState(t, "two-questions-tab1")
	second := claudeState(t, "two-questions-tab2")
	review := claudeState(t, "two-questions-submit")

	if first.Type != Menu || first.Actions[1].Label != "Green Green" || !strings.Contains(first.Context, "Which colour?") {
		t.Fatalf("first tab = %+v", first)
	}
	if second.Type != Menu || second.Actions[0].Label != "Small Small" || !strings.Contains(second.Context, "Which size?") {
		t.Fatalf("second tab = %+v", second)
	}
	// The tabs differ, so a card that answered the first sees a new prompt.
	if reflect.DeepEqual(first, second) {
		t.Fatal("the two tabs read the same")
	}
	want := []Action{{Label: "Submit answers", Keys: "Enter"}, {Label: "Cancel", Keys: "Down Enter"}}
	if review.Type != Menu || !reflect.DeepEqual(review.Actions, want) {
		t.Fatalf("review = %#v", review)
	}
	// The answers given are indented under the review, not the transcript.
	if !strings.Contains(review.Context, "Review your answers") || !strings.Contains(review.Context, "→ Small") {
		t.Fatalf("review context = %q", review.Context)
	}
}

func TestAClaudeMultiSelectKeepsItsSubmitRowApart(t *testing.T) {
	state := claudeState(t, "multi-select")

	want := []Action{
		{Label: "[ ] Cat Cat", Keys: "Enter"},
		{Label: "[ ] Dog Dog", Keys: "Down Enter"},
		{Label: "[ ] Fish Fish", Keys: "Down Down Enter"},
		// Typing here ticks the row; Enter would untick it again.
		{Label: "[ ] Type something", Keys: "Down Down Down Enter", TakesText: true},
		{Label: "Submit", Keys: "Down Down Down Down Enter"},
		{Label: "Chat about this", Keys: "Down Down Down Down Down Enter"},
		{Label: "Cancel (Esc)", Keys: "Escape"},
	}
	if state.Type != Menu || !reflect.DeepEqual(state.Actions, want) {
		t.Fatalf("actions = %#v", state.Actions)
	}

	toggled := claudeState(t, "multi-select-toggled")
	if toggled.Actions[0].Label != "[✔] Cat Cat" {
		t.Fatalf("toggled = %#v", toggled.Actions[0])
	}
}

func TestAClaudeMultiSelectIsReadWithTheCursorOnSubmit(t *testing.T) {
	state := claudeState(t, "multi-select-on-submit")

	var keys []string
	for _, action := range state.Actions {
		keys = append(keys, action.Label+"="+action.Keys)
	}
	want := []string{
		"[✔] Cat Cat=Up Up Up Up Enter",
		"[ ] Dog Dog=Up Up Up Enter",
		"[ ] Fish Fish=Up Up Enter",
		"[ ] Type something=Up Enter",
		"Submit=Enter",
		"Chat about this=Down Enter",
		"Cancel (Esc)=Escape",
	}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys = %#v", keys)
	}
}

func TestAClaudePermissionPromptIsAnsweredWithTheCursor(t *testing.T) {
	state := claudeState(t, "permission")

	if state.Type != ToolApproval {
		t.Fatalf("type = %q", state.Type)
	}
	// A digit alone answers Claude Code's dialog, so "1 Enter" would press
	// Enter once more on whatever is drawn next.
	want := []Action{
		{Label: "Yes", Keys: "Enter"},
		{Label: "Yes, and always allow access to /tmp/claude-1000/l688 from this project", Keys: "Down Enter"},
		{Label: "Yes, and switch to auto mode · auto mode handles these prompts for you", Keys: "Down Down Enter"},
		{Label: "No", Keys: "Down Down Down Enter"},
		{Label: "Cancel (Esc)", Keys: "Escape"},
	}
	if !reflect.DeepEqual(state.Actions, want) {
		t.Fatalf("actions = %#v", state.Actions)
	}
	// What is being approved is in the context, inside its dashed frame.
	if !strings.Contains(state.Context, "touch hello.txt") || !strings.Contains(state.Context, "Do you want to proceed?") {
		t.Fatalf("context = %q", state.Context)
	}
}

func TestAPermissionPromptInASideBorderedBoxHasActions(t *testing.T) {
	screen := "╭──────────────────────────────╮\n" +
		"│ Bash command                 │\n" +
		"│   touch hello.txt            │\n" +
		"│ Do you want to proceed?      │\n" +
		"│ ❯ 1. Yes                     │\n" +
		"│   2. No                      │\n" +
		"╰──────────────────────────────╯\n"

	state := DetectForAgent("claude-code", screen).State

	if state.Type != ToolApproval || len(state.Actions) != 2 || state.Actions[1] != (Action{Label: "No", Keys: "Down Enter"}) {
		t.Fatalf("state = %#v", state)
	}
}

func TestAScrolledClaudeListIsReadFromWhatIsOnScreen(t *testing.T) {
	top := claudeState(t, "scrolled-top")
	var got []string
	for _, action := range top.Actions {
		got = append(got, action.Label+"="+action.Keys)
	}
	want := []string{
		"Apple A crisp, sweet fruit that comes in many varieties.=Enter",
		"Banana A soft, starchy fruit with a yellow peel.=Down Enter",
		"Cherry A small stone fruit, sweet or tart.=Down Down Enter",
		"Date A sticky, very sweet fruit from a palm tree.=Down Down Down Enter",
		// Option 5 is scrolled out of sight, so the sixth is five rows down.
		"Chat about this=Down Down Down Down Down Enter",
		"Cancel (Esc)=Escape",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("top = %#v", got)
	}

	bottom := claudeState(t, "scrolled-bottom")
	got = nil
	for _, action := range bottom.Actions {
		got = append(got, action.Label+"="+action.Keys)
	}
	want = []string{
		"Banana A soft, starchy fruit with a yellow peel.=Up Up Up Enter",
		"Cherry A small stone fruit, sweet or tart.=Up Up Enter",
		"Date A sticky, very sweet fruit from a palm tree.=Up Enter",
		"Type something.=Enter",
		"Chat about this=Down Enter",
		"Cancel (Esc)=Escape",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bottom = %#v", got)
	}
	if !bottom.Actions[3].TakesText {
		t.Fatalf("text row = %#v", bottom.Actions[3])
	}
}
