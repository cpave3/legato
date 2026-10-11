package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/agents"
	"github.com/cpave3/legato/internal/tui/hotkey"
	"github.com/cpave3/legato/internal/tui/statusbar"
	"github.com/cpave3/legato/internal/tui/theme"
)

type fakeAgentUI struct {
	shown map[string]service.AgentUIShown
}

func (f *fakeAgentUI) Shown(_ context.Context, taskID string) (service.AgentUIShown, error) {
	return f.shown[taskID], nil
}

func twoAgents() []service.AgentSession {
	return []service.AgentSession{
		{ID: 1, TaskID: "task-1", TmuxSession: "legato-task-1", Status: "running"},
		{ID: 2, TaskID: "task-2", TmuxSession: "legato-task-2", Status: "running"},
	}
}

// messagesOf runs a command and returns every message it produces, looking
// inside batches. Timers and listeners that would block are not run.
func messagesOf(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(200 * time.Millisecond):
		return nil
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, child := range batch {
			out = append(out, messagesOf(child)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func newAgentUIApp(bus *events.Bus, ui AgentUIService, webURL string) App {
	app := NewApp(&fakeBoardService{}, nil, &fakeAgentService{agents: twoAgents()}, nil, &fakeReportService{}, theme.NewIcons("unicode"), bus, "", nil, nil, "", nil, nil)
	if ui != nil {
		app.SetAgentUIService(ui, webURL)
	}
	return app
}

// The TUI cannot draw a card or an artifact. When the agent list is loaded
// it learns what each agent showed and where the web UI has it.
func TestAgentListLoadsWhatEachAgentShowedWithALinkToTheWebUI(t *testing.T) {
	ui := &fakeAgentUI{shown: map[string]service.AgentUIShown{
		"task-1": {Cards: 2, Artifacts: 1, LatestID: "ui-0123456789", LatestTitle: "Build status"},
	}}
	app := newAgentUIApp(nil, ui, "http://localhost:3080")

	_, cmd := app.Update(agents.AgentsRefreshedMsg{Agents: twoAgents()})

	var refreshed *agents.ShownRefreshedMsg
	for _, msg := range messagesOf(cmd) {
		if shown, ok := msg.(agents.ShownRefreshedMsg); ok {
			refreshed = &shown
		}
	}
	if refreshed == nil {
		t.Fatal("loading the agent list did not load what the agents showed")
	}
	got := refreshed.Shown["task-1"]
	if got.Cards != 2 || got.Artifacts != 1 || got.LatestTitle != "Build status" {
		t.Fatalf("task-1 = %#v", got)
	}
	if got.URL != "http://localhost:3080/agents?task=task-1&artifact=ui-0123456789" {
		t.Fatalf("task-1 link = %q", got.URL)
	}
	if other := refreshed.Shown["task-2"]; other.Count() != 0 || other.URL != "" {
		t.Fatalf("task-2 has shown nothing, got %#v", other)
	}
}

func TestWithoutAWebUIWhatAnAgentShowedHasNoLink(t *testing.T) {
	ui := &fakeAgentUI{shown: map[string]service.AgentUIShown{"task-1": {Cards: 1, LatestID: "ui-0123456789"}}}
	app := newAgentUIApp(nil, ui, "")

	_, cmd := app.Update(agents.AgentsRefreshedMsg{Agents: twoAgents()})

	for _, msg := range messagesOf(cmd) {
		if shown, ok := msg.(agents.ShownRefreshedMsg); ok {
			if got := shown.Shown["task-1"]; got.Cards != 1 || got.URL != "" {
				t.Fatalf("task-1 = %#v, want one card and no link", got)
			}
			return
		}
	}
	t.Fatal("what the agents showed was not loaded")
}

func TestWithoutTheServiceTheAgentListLoadsNothingMore(t *testing.T) {
	app := newAgentUIApp(nil, nil, "")

	_, cmd := app.Update(agents.AgentsRefreshedMsg{Agents: twoAgents()})

	for _, msg := range messagesOf(cmd) {
		if _, ok := msg.(agents.ShownRefreshedMsg); ok {
			t.Fatal("loaded what agents showed with no service to ask")
		}
	}
	if app.hotkeyContext().Capabilities[hotkey.CapabilityAgentUI] {
		t.Fatal("the open key is offered with nothing behind it")
	}
}

// An agent showing something reaches the TUI as an event. It reloads the
// counts and says so, because nothing else in the TUI would.
func TestAnAgentShowingSomethingIsAnnouncedAndReloaded(t *testing.T) {
	bus := events.New()
	ui := &fakeAgentUI{shown: map[string]service.AgentUIShown{"task-2": {Artifacts: 1, LatestID: "ui-aaaaaaaaaa", LatestTitle: "Sync design"}}}
	app := newAgentUIApp(bus, ui, "http://localhost:3080")
	model, _ := app.Update(agents.AgentsRefreshedMsg{Agents: twoAgents()})
	app = model.(App)
	if app.agentUISub == nil {
		t.Fatal("the app does not listen for what agents show")
	}

	bus.Publish(events.Event{Type: events.EventAgentUIChanged, Payload: events.AgentUIChangedPayload{TaskID: "task-2", ItemID: "ui-aaaaaaaaaa", Placement: "panel", Revision: 1, Kind: "shown"}})
	msg := app.listenAgentUIUpdates()()
	model, cmd := app.Update(msg)
	app = model.(App)

	var announced, reloaded bool
	for _, produced := range messagesOf(cmd) {
		switch m := produced.(type) {
		case statusbar.InfoMsg:
			announced = strings.Contains(m.Text, "task-2") && strings.Contains(m.Text, "artifact")
		case agents.ShownRefreshedMsg:
			reloaded = m.Shown["task-2"].Artifacts == 1
		}
	}
	if !announced {
		t.Fatal("the TUI did not say that task-2 showed an artifact")
	}
	if !reloaded {
		t.Fatal("what the agents showed was not reloaded")
	}

}

// A removal changes the counts and is not news.
func TestAnAgentLosingSomethingIsReloadedWithoutAnAnnouncement(t *testing.T) {
	// No bus: the app would otherwise start listening again, and that
	// listener never returns in a test.
	app := newAgentUIApp(nil, &fakeAgentUI{}, "http://localhost:3080")
	model, _ := app.Update(agents.AgentsRefreshedMsg{Agents: twoAgents()})
	app = model.(App)

	_, cmd := app.Update(agentUIUpdateMsg{Payload: events.AgentUIChangedPayload{TaskID: "task-2", ItemID: "ui-aaaaaaaaaa", Placement: "panel", Kind: "deleted"}})

	reloaded := false
	for _, produced := range messagesOf(cmd) {
		switch m := produced.(type) {
		case statusbar.InfoMsg:
			t.Fatalf("a removal was announced: %q", m.Text)
		case agents.ShownRefreshedMsg:
			reloaded = true
		}
	}
	if !reloaded {
		t.Fatal("what the agents showed was not reloaded after a removal")
	}
}

func TestOpeningWhatAnAgentShowedOpensItsLinkInTheBrowser(t *testing.T) {
	app := newAgentUIApp(nil, &fakeAgentUI{}, "http://localhost:3080")
	var opened []string
	app.openURL = func(url string) error { opened = append(opened, url); return nil }

	_, cmd := app.Update(agents.OpenShownMsg{TaskID: "task-1", URL: "http://localhost:3080/agents?task=task-1&artifact=ui-0123456789", Count: 2})
	messages := messagesOf(cmd)

	if len(opened) != 1 || opened[0] != "http://localhost:3080/agents?task=task-1&artifact=ui-0123456789" {
		t.Fatalf("opened = %v", opened)
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %#v", messages)
	}
	if info, ok := messages[0].(statusbar.InfoMsg); !ok || !strings.Contains(info.Text, "web UI") {
		t.Fatalf("message = %#v, want a note that it opened", messages[0])
	}
}

func TestOpeningSaysWhyWhenNothingCanBeOpened(t *testing.T) {
	cases := map[string]struct {
		msg      agents.OpenShownMsg
		openErr  error
		wantText string
		wantErr  bool
	}{
		"nothing shown":         {msg: agents.OpenShownMsg{TaskID: "task-1"}, wantText: "has not shown anything"},
		"web UI is off":         {msg: agents.OpenShownMsg{TaskID: "task-1", Count: 1}, wantText: "web.enabled", wantErr: true},
		"browser will not open": {msg: agents.OpenShownMsg{TaskID: "task-1", Count: 1, URL: "http://localhost:3080/agents?task=task-1"}, openErr: errors.New("no browser"), wantText: "http://localhost:3080/agents?task=task-1", wantErr: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			app := newAgentUIApp(nil, &fakeAgentUI{}, "http://localhost:3080")
			opened := 0
			app.openURL = func(string) error { opened++; return tc.openErr }

			_, cmd := app.Update(tc.msg)
			messages := messagesOf(cmd)

			if len(messages) != 1 {
				t.Fatalf("messages = %#v", messages)
			}
			text, isErr := "", false
			switch m := messages[0].(type) {
			case statusbar.InfoMsg:
				text = m.Text
			case statusbar.ErrorMsg:
				text, isErr = m.Text, true
			default:
				t.Fatalf("message = %T", m)
			}
			if !strings.Contains(text, tc.wantText) || isErr != tc.wantErr {
				t.Fatalf("message = %q (error %v), want it to contain %q (error %v)", text, isErr, tc.wantText, tc.wantErr)
			}
			if tc.msg.URL == "" && opened != 0 {
				t.Fatal("tried to open a browser with no address")
			}
		})
	}
}

// The whole path, as a person meets it: in the agents view, with an agent
// that showed something selected, the o key opens its link.
func TestOKeyInTheAgentsViewOpensWhatTheSelectedAgentShowed(t *testing.T) {
	ui := &fakeAgentUI{shown: map[string]service.AgentUIShown{"task-1": {Cards: 1, LatestID: "ui-0123456789", LatestTitle: "Weather"}}}
	app := newAgentUIApp(nil, ui, "http://localhost:3080")
	var opened []string
	app.openURL = func(url string) error { opened = append(opened, url); return nil }
	app = app.setMode(viewAgents)
	app.agentView.SetSize(120, 40)

	// Loading the agent list loads what the agents showed.
	model, cmd := app.Update(agents.AgentsRefreshedMsg{Agents: twoAgents()})
	app = model.(App)
	for _, msg := range messagesOf(cmd) {
		model, _ = app.Update(msg)
		app = model.(App)
	}
	if view := app.agentView.View(); !strings.Contains(view, "1 card") || !strings.Contains(view, "http://localhost:3080/agents?task=task-1&artifact=ui-0123456789") {
		t.Fatalf("the agents view does not show the link:\n%s", view)
	}

	model, cmd = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	app = model.(App)
	for _, msg := range messagesOf(cmd) {
		_, next := app.Update(msg)
		messagesOf(next)
	}

	if len(opened) != 1 || opened[0] != "http://localhost:3080/agents?task=task-1&artifact=ui-0123456789" {
		t.Fatalf("opened = %v", opened)
	}
}

// statusText is what the status bar displays, at a width that fits it.
func statusText(app App) string {
	bar, _ := app.statusBar.Update(tea.WindowSizeMsg{Width: 200})
	return bar.View()
}

// feed runs a command and gives every message it produces back to the app,
// as the Bubble Tea runtime does.
func feed(app App, cmd tea.Cmd) App {
	for _, msg := range messagesOf(cmd) {
		model, _ := app.Update(msg)
		app = model.(App)
	}
	return app
}

// Producing an announcement is not the same as showing it: the person must
// be able to read it on the status bar.
func TestWhatAnAgentShowedIsReadableOnTheStatusBar(t *testing.T) {
	ui := &fakeAgentUI{shown: map[string]service.AgentUIShown{"task-2": {Artifacts: 1, LatestID: "ui-aaaaaaaaaa"}}}
	app := newAgentUIApp(nil, ui, "http://localhost:3080")
	app = app.setMode(viewAgents)

	model, cmd := app.Update(agentUIUpdateMsg{Payload: events.AgentUIChangedPayload{TaskID: "task-2", ItemID: "ui-aaaaaaaaaa", Placement: "panel", Revision: 1, Kind: "shown"}})
	app = feed(model.(App), cmd)

	if text := statusText(app); !strings.Contains(text, "task-2 showed an artifact in the web UI") || !strings.Contains(text, "o opens it") {
		t.Fatalf("status bar = %q", text)
	}
}

func TestTheResultOfOpeningIsReadableOnTheStatusBar(t *testing.T) {
	app := newAgentUIApp(nil, &fakeAgentUI{}, "http://localhost:3080")
	app.openURL = func(string) error { return nil }

	model, cmd := app.Update(agents.OpenShownMsg{TaskID: "task-1", URL: "http://localhost:3080/agents?task=task-1", Count: 1})
	app = feed(model.(App), cmd)
	if text := statusText(app); !strings.Contains(text, "Opened in the web UI") {
		t.Fatalf("status bar after opening = %q", text)
	}

	model, cmd = app.Update(agents.OpenShownMsg{TaskID: "task-1"})
	app = feed(model.(App), cmd)
	if text := statusText(app); !strings.Contains(text, "task-1 has not shown anything") {
		t.Fatalf("status bar for an agent that showed nothing = %q", text)
	}
}
