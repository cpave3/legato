package agents

import (
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

// worked is the end of a conversation in which the agent read, changed and
// tested something, and is still running a command.
func worked() service.ConversationTail {
	return service.ConversationTail{
		Status: "running",
		Entries: []service.ConversationEntry{
			{Kind: service.EntryUser, Text: "add a --json flag to yg list"},
			{Kind: service.EntryAssistant, Text: "Let me look."},
			{Kind: service.EntryTool, Tool: "read", Text: "list.ts", State: service.ToolDone},
			{Kind: service.EntryTool, Tool: "bash", Text: "Run the list tests", Command: "pnpm vitest run yg/list", State: service.ToolFailed},
			{Kind: service.EntryAssistant, Text: "**Fixing** the `--json` branch."},
			{Kind: service.EntryTool, Tool: "bash", Text: "Build it", Command: "pnpm build", State: service.ToolRunning},
		},
	}
}

func told(t *testing.T, taskID string, tail service.ConversationTail) Model {
	t.Helper()
	m := ensemble(t)
	m, _ = m.Update(ConversationMsg{TaskID: taskID, Tail: tail})
	return m
}

func TestTileShowsWhatANativeAgentSaidAndDid(t *testing.T) {
	m := told(t, "busy-1", worked())
	m.SetSpinner(2)

	view := m.View()

	for _, want := range []string{
		"› add a --json flag to yg list",
		"Let me look.",
		"● read list.ts",
		"✗ bash Run the list tests",
		"Fixing the --json branch.",
		theme.Spinner(2) + " bash Build it",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the tile should contain %q: %q", want, view)
		}
	}
	if strings.Contains(view, "**") || strings.Contains(view, "`") {
		t.Errorf("the agent's words should be shown without their markup: %q", view)
	}
}

func TestTheConversationTakesThePlaceOfThePaneAndThePaneOfTheConversation(t *testing.T) {
	m := ensemble(t)
	m, _ = m.Update(PreviewMsg{TaskID: "busy-1", Output: "bypass permissions on\n"})
	m, _ = m.Update(ConversationMsg{TaskID: "busy-1", Tail: worked()})

	if view := m.View(); strings.Contains(view, "bypass permissions on") || !strings.Contains(view, "Let me look.") {
		t.Fatalf("a conversation should replace what the pane showed: %q", view)
	}

	// The agent was downgraded to its terminal: the pane is all there is.
	m, _ = m.Update(PreviewMsg{TaskID: "busy-1", Output: "bypass permissions on\n"})
	if view := m.View(); !strings.Contains(view, "bypass permissions on") || strings.Contains(view, "Let me look.") {
		t.Fatalf("the pane should replace a conversation that can no longer be read: %q", view)
	}
}

func TestTileKeepsTheNewestOfAConversationThatDoesNotFit(t *testing.T) {
	tail := service.ConversationTail{}
	for _, name := range []string{"first", "second", "third", "fourth", "fifth", "sixth", "seventh", "eighth", "ninth", "tenth", "eleventh", "twelfth"} {
		tail.Entries = append(tail.Entries, service.ConversationEntry{Kind: service.EntryTool, Tool: "bash", Text: name + " step", State: service.ToolDone})
	}
	m := New(theme.NewIcons("unicode"))
	m.SetAgents(mixedAgents())
	m.SetSize(170, 26) // two rows of tiles, a few lines in each
	m, _ = m.Update(key('e'))
	m, _ = m.Update(ConversationMsg{TaskID: "busy-1", Tail: tail})

	view := m.View()
	if !strings.Contains(view, "twelfth step") || strings.Contains(view, "first step") {
		t.Errorf("the tile should end with the newest step and give up the oldest: %q", view)
	}
}

func TestTileGivesTheLastThingSaidRoomAndCutsItWhereItEnds(t *testing.T) {
	long := "The work is done and committed. " + strings.Repeat("More detail follows here. ", 40)
	m := told(t, "idle-1", service.ConversationTail{Entries: []service.ConversationEntry{
		{Kind: service.EntryUser, Text: "ship it"},
		{Kind: service.EntryAssistant, Text: long},
	}})

	view := m.View()
	if !strings.Contains(view, "› ship it") || !strings.Contains(view, "The work is done and committed.") {
		t.Errorf("what was asked and how the answer begins should both show: %q", view)
	}
	if !strings.Contains(view, "…") {
		t.Errorf("an answer that is cut should say so: %q", view)
	}
}

func TestTileSaysWhatAnAgentWaitsToBeAllowed(t *testing.T) {
	m := told(t, "ask-1", service.ConversationTail{
		Status:     "waiting_for_permission",
		Permission: &service.ConversationPermission{Tool: "bash", Command: "rm -rf dist && pnpm build"},
	})

	if view := m.View(); !strings.Contains(view, "Wants to run:") || !strings.Contains(view, "rm -rf dist && pnpm build") {
		t.Errorf("the tile should say what the agent wants to run: %q", view)
	}
}

func TestTileSaysWhatAnAgentAsks(t *testing.T) {
	m := told(t, "ask-1", service.ConversationTail{
		Status:   "waiting_for_input",
		Question: &service.ConversationQuestion{Text: "Which color?", Options: []string{"Red", "Blue"}},
	})

	view := m.View()
	if !strings.Contains(view, "Asks:") || !strings.Contains(view, "Which color?") || !strings.Contains(view, "Red · Blue") {
		t.Errorf("the tile should show the question and its options: %q", view)
	}
}

// Claude Code raises a request in Legato only while the web panel is open.
// Without one, the call the agent waits on is the last it started.
func TestAWaitingAgentsUnfinishedCallIsShownAsTheCommandItWaitsOn(t *testing.T) {
	m := told(t, "ask-1", service.ConversationTail{Entries: []service.ConversationEntry{
		{Kind: service.EntryTool, Tool: "bash", Text: "Clean and rebuild", Command: "rm -rf dist && pnpm build", State: service.ToolRunning},
	}})

	view := m.View()
	if !strings.Contains(view, "◆ bash rm -rf dist && pnpm build") {
		t.Errorf("the call should be marked as waiting and name its command: %q", view)
	}
	if strings.Contains(view, "Clean and rebuild") {
		t.Errorf("what is to be allowed is the command, not the agent's words for it: %q", view)
	}
}

func TestToolsOfAPluginAreNamedByTheirOwnName(t *testing.T) {
	m := told(t, "busy-1", service.ConversationTail{Entries: []service.ConversationEntry{
		{Kind: service.EntryTool, Tool: "mcp__plugin_legato-tools_legato__show_card", Text: "Build status", State: service.ToolDone},
	}})

	if view := m.View(); !strings.Contains(view, "● show_card Build status") || strings.Contains(view, "mcp__") {
		t.Errorf("got: %q", view)
	}
}

func TestTileOfAnAgentThatHasSaidNothingSaysSo(t *testing.T) {
	m := told(t, "idle-1", service.ConversationTail{})

	if view := m.View(); !strings.Contains(view, "No messages yet.") {
		t.Errorf("got: %q", view)
	}
}

func TestTileShowsHowFullTheContextIs(t *testing.T) {
	tail := worked()
	tail.ContextUsed, tail.ContextWindow = 138_000, 200_000
	m := told(t, "busy-1", tail)

	if view := m.View(); !strings.Contains(view, "ctx ▰▰▰▰▱ 69%") {
		t.Errorf("the tile should show the context as a bar and a share: %q", view)
	}

	m = told(t, "busy-1", worked())
	if view := m.View(); strings.Contains(view, "ctx ") {
		t.Errorf("with the context unknown the tile should not speak of it: %q", view)
	}
}

func TestTileShowsItsTimeOnce(t *testing.T) {
	m := ensemble(t)
	m.SetDurations(map[string]DurationData{
		"busy-1": {Working: 3*time.Hour + 34*time.Minute},
		"idle-1": {Working: 88 * time.Minute, Waiting: 5 * time.Minute},
	})

	view := m.View()
	if got := strings.Count(view, "3h 34m"); got != 1 {
		t.Errorf("a running agent's time is shown %d times, want once, in its header: %q", got, view)
	}
	if strings.Contains(view, "1h 28m") {
		t.Errorf("the foot of a tile should not repeat times: %q", view)
	}
}

func steps(names ...string) []service.ConversationEntry {
	var entries []service.ConversationEntry
	for _, name := range names {
		entries = append(entries, service.ConversationEntry{Kind: service.EntryTool, Tool: "bash", Text: name + " step", State: service.ToolDone})
	}
	return entries
}

func TestALongRunOfToolCallsGivesWayToWhatWasSaidAroundIt(t *testing.T) {
	entries := []service.ConversationEntry{{Kind: service.EntryUser, Text: "do it"}}
	entries = append(entries, steps("one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten")...)
	entries = append(entries, service.ConversationEntry{Kind: service.EntryAssistant, Text: "All done."})
	m := told(t, "busy-1", service.ConversationTail{Entries: entries})

	view := m.View()

	// Seven lines: what was asked, the newest calls of the run, the answer.
	for _, want := range []string{"› do it", "⋮ 6 earlier calls", "seven step", "ten step", "All done."} {
		if !strings.Contains(view, want) {
			t.Errorf("the tile should contain %q: %q", want, view)
		}
	}
	if strings.Contains(view, "six step") {
		t.Errorf("the older calls of the run should give way: %q", view)
	}
}

func TestAShortRunOfToolCallsIsShownWhole(t *testing.T) {
	entries := append(steps("one", "two", "three", "four"), service.ConversationEntry{Kind: service.EntryAssistant, Text: "All done."})
	m := told(t, "busy-1", service.ConversationTail{Entries: entries})

	if view := m.View(); !strings.Contains(view, "one step") || strings.Contains(view, "⋮") {
		t.Errorf("with room for every call none should be hidden: %q", view)
	}
}

func TestARunThatBeganBeforeWhatWasReadDoesNotCountItsCalls(t *testing.T) {
	var names []string
	for i := 0; i < ConversationEntries; i++ {
		names = append(names, "another")
	}
	m := told(t, "busy-1", service.ConversationTail{Entries: steps(names...)})

	if view := m.View(); !strings.Contains(view, "⋮ earlier calls") {
		t.Errorf("how many calls came before is not known, so no number should be given: %q", view)
	}
}
