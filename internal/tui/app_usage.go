package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/agents"
)

// usageInterval is how often the token usage is read again.
const usageInterval = time.Minute

// SetUsageService makes the agents view show how many tokens were used
// today. Without one, the view does not speak of tokens.
func (a *App) SetUsageService(svc service.UsageService) {
	a.usageSvc = svc
}

// usageLoadedMsg carries the token usage, or why it could not be read.
type usageLoadedMsg struct {
	usage service.Usage
	err   error
}

// usageTickMsg says it is time to read the token usage again.
type usageTickMsg struct{}

// loadUsage reads the token usage. It returns nil with no service to ask.
func (a App) loadUsage() tea.Cmd {
	if a.usageSvc == nil {
		return nil
	}
	svc := a.usageSvc
	return func() tea.Msg {
		usage, err := svc.Usage(context.Background())
		return usageLoadedMsg{usage: usage, err: err}
	}
}

// handleUsageLoaded shows the usage that was read and arranges the next
// read. A read that failed leaves what is shown as it is: the number a
// minute old is nearer the truth than none.
func (a App) handleUsageLoaded(msg usageLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err == nil {
		a.agentView.SetUsage(&agents.Usage{
			TokensToday: msg.usage.TokensToday,
			InputToday:  msg.usage.InputTokensToday,
			OutputToday: msg.usage.OutputTokensToday,
			Hourly:      msg.usage.Hourly,
		})
	}
	return a, tea.Tick(usageInterval, func(time.Time) tea.Msg { return usageTickMsg{} })
}
