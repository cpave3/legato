package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/engine/macros"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

type fakeMacroStarter struct {
	list    []service.MacroEntry
	started []service.MacroStart
	result  service.MacroStartResult
	err     error
}

func (f *fakeMacroStarter) List(context.Context) ([]service.MacroEntry, error) { return f.list, nil }
func (f *fakeMacroStarter) Start(_ context.Context, req service.MacroStart) (service.MacroStartResult, error) {
	f.started = append(f.started, req)
	return f.result, f.err
}

func macroEntry(name, appliesTo string) service.MacroEntry {
	return service.MacroEntry{Macro: macros.Macro{Name: name, Keys: name, AppliesTo: appliesTo}}
}

func openCardMacros(t *testing.T, board *fakeBoardService, starter *fakeMacroStarter) App {
	t.Helper()
	app := NewApp(board, nil, nil, nil, nil, theme.NewIcons("unicode"), nil, "", nil, nil, "", nil, nil)
	app.SetMacroStarter(starter)
	app, _ = updateApp(app, app.board.Init()())
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	app, _ = updateApp(app, cmd())
	// Macros is the fifth item.
	for range 4 {
		app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyDown})
	}
	app, cmd = updateApp(app, tea.KeyMsg{Type: tea.KeyEnter})
	app, cmd = updateApp(app, cmd())
	app, _ = updateApp(app, cmd())
	return app
}

func TestTheCardMenuStartsAMacroOnTheTask(t *testing.T) {
	starter := &fakeMacroStarter{list: []service.MacroEntry{macroEntry("Implement", "task"), macroEntry("Plan", "task"), macroEntry("Review", "pr")}, result: service.MacroStartResult{Started: true}}
	app := openCardMacros(t, &fakeBoardService{}, starter)
	view := app.activeOverlay.View()
	if !strings.Contains(view, "Plan") || strings.Contains(view, "Review") {
		t.Fatalf("task macros: %s", view)
	}
	if strings.Index(view, "Plan") > strings.Index(view, "Implement") {
		t.Fatalf("the suggested Plan must come first: %s", view)
	}
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyEnter})
	app, cmd = updateApp(app, cmd())
	app, _ = updateApp(app, cmd())
	if len(starter.started) != 1 || starter.started[0].Name != "Plan" || starter.started[0].PR != nil {
		t.Fatalf("started = %+v", starter.started)
	}
	if app.overlayType != overlayNone {
		t.Fatal("a started macro must close the menu")
	}
}

func TestTheCardMenuOffersMacrosOnEachPullRequest(t *testing.T) {
	board := &fakeBoardService{pullRequests: []service.PRMetaView{{Repo: "acme/web", PRNumber: 12, PRURL: "https://github.com/acme/web/pull/12", State: "OPEN"}}}
	starter := &fakeMacroStarter{list: []service.MacroEntry{macroEntry("Plan", "task"), macroEntry("Review", "pr"), macroEntry("Address feedback", "pr")}, result: service.MacroStartResult{Queued: true}}
	app := openCardMacros(t, board, starter)
	view := app.activeOverlay.View()
	if !strings.Contains(view, "Address feedback · #12") || !strings.Contains(view, "Review · #12") {
		t.Fatalf("PR macros: %s", view)
	}
	// Plan, then Address feedback · #12 (the suggested for the task's own PR).
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyDown})
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyEnter})
	app, cmd = updateApp(app, cmd())
	app, _ = updateApp(app, cmd())
	if len(starter.started) != 1 || starter.started[0].Name != "Address feedback" || starter.started[0].PR == nil || starter.started[0].PR.Number != 12 {
		t.Fatalf("started = %+v", starter.started)
	}
}
