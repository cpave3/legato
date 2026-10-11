package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/agents"
	"github.com/cpave3/legato/internal/tui/hotkey"
	"github.com/cpave3/legato/internal/tui/theme"
)

type fakeUsage struct {
	usage service.Usage
	err   error
	calls int
}

func (f *fakeUsage) Usage(context.Context) (service.Usage, error) {
	f.calls++
	return f.usage, f.err
}

func newUsageApp(usage service.UsageService) App {
	app := NewApp(&fakeBoardService{}, nil, &fakeAgentService{agents: twoAgents()}, nil, &fakeReportService{}, theme.NewIcons("unicode"), nil, "", nil, nil, "", nil, nil)
	if usage != nil {
		app.SetUsageService(usage)
	}
	model, _ := app.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	return model.(App)
}

// agentsView opens the agents view with its agents loaded and returns what
// it draws.
func agentsView(t *testing.T, app App) string {
	t.Helper()
	model, _ := app.dispatchHotkey(hotkey.Binding{ID: hotkey.ActionViewAgents, Target: hotkey.TargetRoot}, tea.KeyMsg{})
	model, _ = model.(App).Update(agents.AgentsRefreshedMsg{Agents: twoAgents()})
	return model.(App).View()
}

func TestAgentsViewShowsTheTokensUsedTodayWhenUsageIsConfigured(t *testing.T) {
	usage := &fakeUsage{usage: service.Usage{TokensToday: 1_400_000, InputTokensToday: 1_390_000, OutputTokensToday: 10_000}}
	app := newUsageApp(usage)

	var loaded *usageLoadedMsg
	for _, msg := range messagesOf(app.Init()) {
		if m, ok := msg.(usageLoadedMsg); ok {
			loaded = &m
		}
	}
	if loaded == nil {
		t.Fatal("starting the app should ask for the usage")
	}
	model, next := app.Update(*loaded)

	if view := agentsView(t, model.(App)); !strings.Contains(view, "tokens today") || !strings.Contains(view, "↓ out") || !strings.Contains(view, "10k") {
		t.Errorf("the agents view should show the tokens used today: %q", view)
	}
	if next == nil {
		t.Error("the usage should be asked for again later")
	}
}

func TestUsageThatCannotBeReadKeepsWhatWasShown(t *testing.T) {
	app := newUsageApp(&fakeUsage{})
	model, _ := app.Update(usageLoadedMsg{usage: service.Usage{TokensToday: 1_400_000}})

	model, _ = model.(App).Update(usageLoadedMsg{err: errors.New("connection refused")})

	if view := agentsView(t, model.(App)); !strings.Contains(view, "1.4M") {
		t.Errorf("a failed read should not blank the last known usage: %q", view)
	}
}

func TestWithoutAUsageServiceNothingIsAskedOrShown(t *testing.T) {
	app := newUsageApp(nil)

	for _, msg := range messagesOf(app.Init()) {
		if _, ok := msg.(usageLoadedMsg); ok {
			t.Fatal("no usage should be asked for")
		}
	}
	if view := agentsView(t, app); strings.Contains(view, "tokens today") {
		t.Errorf("the agents view should not speak of tokens: %q", view)
	}
}
