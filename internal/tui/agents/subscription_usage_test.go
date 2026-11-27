package agents

import (
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

// The foot rows name each provider as the config provider table does, and
// label its meters from it: OpenCode Go's monthly window shows as "mo".
func TestSubscriptionRowsLabelOpenCodeGoFromTheProviderTable(t *testing.T) {
	asOf := time.Date(2026, 11, 27, 8, 0, 0, 0, time.UTC)
	m := New(theme.NewIcons("unicode"))
	m.SetSubscriptionUsage(&service.SubscriptionUsage{Providers: []service.ProviderUsage{
		{Provider: service.UsageProviderOpenCode, State: service.UsageOK, AsOf: &asOf, Windows: []service.UsageWindow{
			{Meter: service.UsageMeterFiveHour, UsedPercent: 30},
			{Meter: service.UsageMeterWeekly, UsedPercent: 66},
			{Meter: service.UsageMeterMonthly, UsedPercent: 50},
		}},
	}})
	rows := m.subscriptionRows()
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].label != "OpenCode Go" {
		t.Fatalf("label = %q", rows[0].label)
	}
	got := rows[0].value
	for _, part := range []string{"5h 30%", "wk 66%", "mo 50%"} {
		if !strings.Contains(got, part) {
			t.Fatalf("value %q misses %q", got, part)
		}
	}

	// An unavailable row says so with the reason's words.
	m.SetSubscriptionUsage(&service.SubscriptionUsage{Providers: []service.ProviderUsage{
		{Provider: service.UsageProviderOpenCode, State: service.UsageUnavailable, Reason: service.UsageReasonMissingKey},
	}})
	rows = m.subscriptionRows()
	if len(rows) != 1 || rows[0].label != "OpenCode Go" || rows[0].value != "unavailable" {
		t.Fatalf("rows = %+v", rows)
	}
}
