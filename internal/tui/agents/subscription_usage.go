package agents

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

// SetSubscriptionUsage sets the subscriptions' usage the
// view shows; nil when none is read.
func (m *Model) SetSubscriptionUsage(usage *service.SubscriptionUsage) {
	m.subscriptionUsage = usage
}

// subscriptionRow is one provider's usage in brief: its name, its windows
// ("5h 42% wk 91%") or why there are none, and the colour to draw them in.
type subscriptionRow struct {
	label string
	value string
	color lipgloss.Color
}

func (m Model) subscriptionRows() []subscriptionRow {
	if m.subscriptionUsage == nil {
		return nil
	}
	var rows []subscriptionRow
	for _, p := range m.subscriptionUsage.Providers {
		row := subscriptionRow{label: service.UsageProviderName(p.Provider), color: theme.TextSecondary}
		if p.State == service.UsageUnavailable {
			row.value, row.color = "unavailable", theme.TextFaint
			rows = append(rows, row)
			continue
		}
		var parts []string
		most := 0.0
		for _, w := range p.Windows {
			parts = append(parts, fmt.Sprintf("%s %d%%", service.UsageMeterShort(w.Meter), int(math.Round(w.UsedPercent))))
			most = max(most, w.UsedPercent)
		}
		row.value = strings.Join(parts, " ")
		switch {
		case p.Stale:
			row.label += " (stale)"
			row.color = theme.TextFaint
		case most >= 90:
			row.color = theme.Red
		case most >= 75:
			row.color = theme.Amber
		}
		rows = append(rows, row)
	}
	return rows
}
