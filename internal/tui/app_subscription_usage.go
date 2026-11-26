package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
)

// subscriptionUsageInterval is how often the kept subscription usage is
// read again: the database's cache, never a provider.
const subscriptionUsageInterval = time.Minute

// SubscriptionUsageSource is the subscriptions' usage as Legato last read it
// (service.SubscriptionUsageService).
type SubscriptionUsageSource interface {
	Snapshot(ctx context.Context) (service.SubscriptionUsage, error)
}

// SetSubscriptionUsage makes the agents view show how much of the Claude
// and Codex subscriptions' usage limits is used.
func (a *App) SetSubscriptionUsage(source SubscriptionUsageSource) {
	a.subscriptionUsage = source
}

// subscriptionUsageLoadedMsg carries the usage, or why it could not be read.
type subscriptionUsageLoadedMsg struct {
	usage service.SubscriptionUsage
	err   error
}

type subscriptionUsageTickMsg struct{}

func (a App) loadSubscriptionUsage() tea.Cmd {
	if a.subscriptionUsage == nil {
		return nil
	}
	source := a.subscriptionUsage
	return func() tea.Msg {
		usage, err := source.Snapshot(context.Background())
		return subscriptionUsageLoadedMsg{usage: usage, err: err}
	}
}

// handleSubscriptionUsageLoaded shows what was read and arranges the next
// read; a failed read leaves what is shown.
func (a App) handleSubscriptionUsageLoaded(msg subscriptionUsageLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err == nil {
		usage := msg.usage
		a.agentView.SetSubscriptionUsage(&usage)
	}
	return a, tea.Tick(subscriptionUsageInterval, func(time.Time) tea.Msg { return subscriptionUsageTickMsg{} })
}
