package overlay

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
)

func historyKey(m SessionHistoryOverlay, key string) (SessionHistoryOverlay, tea.Msg) {
	var msg tea.KeyMsg
	switch key {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	model, cmd := m.Update(msg)
	var out tea.Msg
	if cmd != nil {
		out = cmd()
	}
	return model.(SessionHistoryOverlay), out
}

var historyEntries = []service.SessionEntry{
	{ID: 1, TaskID: "t-1", TaskTitle: "scn testing gh app", AgentKind: "claude-code", State: "ended", EndReason: "dismissed", CWD: "/repo", Restorable: true, FirstMessage: "wire the app", LastMessage: "Wired."},
	{ID: 2, TaskID: "t-2", TaskTitle: "deleted work", AgentKind: "codex", State: "ended", EndReason: "stopped", Restorable: true, NewTaskOnly: true, Reason: "its task is gone, so it can be restored into a new task only"},
}

func TestTheHistoryOverlayRestoresTheSelectedConversation(t *testing.T) {
	m := NewSessionHistory()
	m = m.SetEntries(historyEntries, nil)
	m, out := historyKey(m, "enter")
	if got, ok := out.(SessionRestoreMsg); !ok || got.ID != 1 || got.Target != service.RestoreIntoTask {
		t.Fatalf("enter = %#v, want a restore of the first into its task", out)
	}
	m, _ = historyKey(m, "j")
	if _, out := historyKey(m, "n"); out.(SessionRestoreMsg).ID != 2 || out.(SessionRestoreMsg).Target != service.RestoreIntoNewTask {
		t.Fatalf("n = %#v, want a restore of the second into a new task", out)
	}
	// One its task cannot take says why instead of asking the server.
	if _, out := historyKey(m, "enter"); out != (SessionHistoryRefusedMsg{Reason: historyEntries[1].Reason}) {
		t.Fatalf("enter on a new-task-only entry = %#v", out)
	}
	if _, out := historyKey(m, "esc"); out != (SessionHistoryCancelledMsg{}) {
		t.Fatalf("esc = %#v", out)
	}
}

func TestTheHistoryOverlaySearchesAsOneTypes(t *testing.T) {
	m := NewSessionHistory().SetEntries(historyEntries, nil)
	m, _ = historyKey(m, "/")
	m, out := historyKey(m, "g")
	if out != (SessionHistorySearchMsg{Query: "g"}) {
		t.Fatalf("typed g = %#v", out)
	}
	m, out = historyKey(m, "h")
	if out != (SessionHistorySearchMsg{Query: "gh"}) {
		t.Fatalf("typed h = %#v", out)
	}
	// While searching, keys that are bindings elsewhere are text.
	if m, out = historyKey(m, "n"); out != (SessionHistorySearchMsg{Query: "ghn"}) {
		t.Fatalf("typed n = %#v, want it in the query", out)
	}
	m, out = historyKey(m, "backspace")
	if out != (SessionHistorySearchMsg{Query: "gh"}) {
		t.Fatalf("backspace = %#v", out)
	}
	m, _ = historyKey(m, "enter") // ends the search
	if _, out := historyKey(m, "enter"); out.(SessionRestoreMsg).ID != 1 {
		t.Fatalf("enter after the search = %#v, want a restore", out)
	}
}

func TestTheHistoryOverlayShowsTheSelectedEntrysMessages(t *testing.T) {
	m := NewSessionHistory().SetEntries(historyEntries, nil)
	if got := m.Selected(); got == nil || got.FirstMessage != "wire the app" || got.LastMessage != "Wired." {
		t.Fatalf("selected = %#v", got)
	}
}

func TestTheHistoryOverlayTakesTypingOnlyWhileSearching(t *testing.T) {
	m := NewSessionHistory().SetEntries(historyEntries, nil)
	if ConsumesPrintableInput(m) {
		t.Fatal("the list takes typing; ? should open help there")
	}
	m, _ = historyKey(m, "/")
	if !ConsumesPrintableInput(m) {
		t.Fatal("the search field does not take typing")
	}
}
