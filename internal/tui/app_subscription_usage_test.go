package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

type fakeSubscriptionUsage struct {
	usage service.SubscriptionUsage
	calls int
}

func (f *fakeSubscriptionUsage) Snapshot(context.Context) (service.SubscriptionUsage, error) {
	f.calls++
	return f.usage, nil
}

func newSubscriptionUsageApp(source SubscriptionUsageSource) App {
	app := NewApp(&fakeBoardService{}, nil, &fakeAgentService{agents: twoAgents()}, nil, &fakeReportService{}, theme.NewIcons("unicode"), nil, "", nil, nil, "", nil, nil)
	if source != nil {
		app.SetSubscriptionUsage(source)
	}
	model, _ := app.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	return model.(App)
}

func loadedSubscriptionUsage(t *testing.T, app App) App {
	t.Helper()
	for _, msg := range messagesOf(app.Init()) {
		if m, ok := msg.(subscriptionUsageLoadedMsg); ok {
			model, next := app.Update(m)
			if next == nil {
				t.Error("the usage should be read again later")
			}
			return model.(App)
		}
	}
	t.Fatal("starting the app should read the subscription usage")
	return app
}

func TestAgentsViewShowsEachSubscriptionsUsage(t *testing.T) {
	asOf := time.Now()
	old := time.Now().Add(-3 * time.Hour)
	source := &fakeSubscriptionUsage{usage: service.SubscriptionUsage{Providers: []service.ProviderUsage{
		{Provider: "claude", State: service.UsageOK, AsOf: &asOf, Windows: []service.UsageWindow{
			{Meter: service.UsageMeterFiveHour, UsedPercent: 42}, {Meter: service.UsageMeterWeekly, UsedPercent: 91.4},
		}},
		{Provider: "codex", State: service.UsageStale, Stale: true, AsOf: &old, Windows: []service.UsageWindow{{Meter: service.UsageMeterWeekly, UsedPercent: 5}}},
	}}}
	app := loadedSubscriptionUsage(t, newSubscriptionUsageApp(source))

	view := agentsView(t, app)
	for _, want := range []string{"Claude", "5h 42% wk 91%", "Codex (stale)", "wk 5%"} {
		if !strings.Contains(view, want) {
			t.Errorf("the list should show %q: %q", want, view)
		}
	}
}

func TestAnUnavailableSubscriptionSaysSo(t *testing.T) {
	source := &fakeSubscriptionUsage{usage: service.SubscriptionUsage{Providers: []service.ProviderUsage{
		{Provider: "claude", State: service.UsageUnavailable, Reason: service.UsageReasonNotSignedIn},
	}}}
	view := agentsView(t, loadedSubscriptionUsage(t, newSubscriptionUsageApp(source)))
	if !strings.Contains(view, "Claude") || !strings.Contains(view, "unavailable") {
		t.Errorf("the list should say Claude's usage is unavailable: %q", view)
	}
}

func TestWithoutSubscriptionUsageNothingIsReadOrShown(t *testing.T) {
	app := newSubscriptionUsageApp(nil)
	for _, msg := range messagesOf(app.Init()) {
		if _, ok := msg.(subscriptionUsageLoadedMsg); ok {
			t.Fatal("no usage should be read")
		}
	}
	if view := agentsView(t, app); strings.Contains(view, "Claude") {
		t.Errorf("the list should not speak of subscriptions: %q", view)
	}
}
