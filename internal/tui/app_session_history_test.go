package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/agents"
	"github.com/cpave3/legato/internal/tui/hotkey"
	"github.com/cpave3/legato/internal/tui/overlay"
	"github.com/cpave3/legato/internal/tui/statusbar"
)

type fakeSessionHistory struct {
	queries  []string
	restored []string
	refuse   string
}

func (f *fakeSessionHistory) List(_ context.Context, q service.SessionQuery) (service.SessionPage, error) {
	f.queries = append(f.queries, q.Q)
	return service.SessionPage{Sessions: []service.SessionEntry{
		{ID: 7, TaskID: "task-1", TaskTitle: "scn testing gh app", AgentKind: "claude-code", State: "ended", EndReason: "dismissed", Restorable: true},
	}}, nil
}

func (f *fakeSessionHistory) Restore(_ context.Context, id int64, target string) (string, error) {
	if f.refuse != "" {
		return "", &service.RestoreRefusal{Reason: f.refuse}
	}
	f.restored = append(f.restored, target)
	return "task-1", nil
}

// feedAll runs every message cmd gives through the app, as Bubble Tea would.
func feedAll(t *testing.T, app App, cmd tea.Cmd) (App, []tea.Msg) {
	t.Helper()
	var seen []tea.Msg
	for _, msg := range messagesOf(cmd) {
		seen = append(seen, msg)
		model, next := app.Update(msg)
		app = model.(App)
		var more []tea.Msg
		app, more = feedAll(t, app, next)
		seen = append(seen, more...)
	}
	return app, seen
}

func TestTheAgentsViewOpensTheHistoryAndRestoresFromIt(t *testing.T) {
	history := &fakeSessionHistory{}
	app := newAgentUIApp(nil, nil, "")
	if app.hotkeyContext().Capabilities[hotkey.CapabilityHistory] {
		t.Fatal("the history key is offered with no history")
	}
	app.SetSessionHistory(history)
	if !app.hotkeyContext().Capabilities[hotkey.CapabilityHistory] {
		t.Fatal("the history key is not offered")
	}

	model, cmd := app.Update(agents.OpenSessionHistoryMsg{})
	app, _ = feedAll(t, model.(App), cmd)
	shown, ok := app.activeOverlay.(overlay.SessionHistoryOverlay)
	if app.overlayType != overlaySessionHistory || !ok || shown.Selected() == nil || shown.Selected().ID != 7 {
		t.Fatalf("overlay = %v %#v, want the history with its entry", app.overlayType, app.activeOverlay)
	}

	model, cmd = app.Update(overlay.SessionHistorySearchMsg{Query: "gh"})
	app, _ = feedAll(t, model.(App), cmd)
	if len(history.queries) != 2 || history.queries[1] != "gh" {
		t.Fatalf("queries = %q", history.queries)
	}

	model, cmd = app.Update(overlay.SessionRestoreMsg{ID: 7, TaskID: "task-1", Target: service.RestoreIntoTask})
	app, seen := feedAll(t, model.(App), cmd)
	if app.overlayType != overlayNone || len(history.restored) != 1 || history.restored[0] != service.RestoreIntoTask {
		t.Fatalf("after restore: overlay %v, restored %q", app.overlayType, history.restored)
	}
	if !said(seen, statusbar.InfoMsg{Text: "Restored the conversation in task-1"}) {
		t.Fatalf("messages = %#v", seen)
	}
}

func TestARefusedRestoreSaysWhyInTheStatusBar(t *testing.T) {
	history := &fakeSessionHistory{refuse: "the task's agent is running: stop it first, or restore into a new task"}
	app := newAgentUIApp(nil, nil, "")
	app.SetSessionHistory(history)
	_, cmd := app.Update(overlay.SessionRestoreMsg{ID: 7, TaskID: "task-1", Target: service.RestoreIntoTask})
	if !said(messagesOf(cmd), statusbar.ErrorMsg{Text: "Cannot restore: " + history.refuse}) {
		t.Fatal("the refusal was not shown")
	}
}

func said(msgs []tea.Msg, want tea.Msg) bool {
	for _, msg := range msgs {
		if msg == want {
			return true
		}
	}
	return false
}
