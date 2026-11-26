package main

import (
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
)

func TestUsageIsPrintedPerProviderWithResetsAndFreshness(t *testing.T) {
	now := time.Date(2026, 11, 26, 8, 0, 0, 0, time.UTC)
	asOf := now.Add(-2 * time.Minute)
	old := now.Add(-3 * time.Hour)
	snap := service.SubscriptionUsage{Providers: []service.ProviderUsage{
		{Provider: "claude", State: service.UsageOK, Plan: "max", AsOf: &asOf, Windows: []service.UsageWindow{
			{Meter: service.UsageMeterFiveHour, UsedPercent: 42, ResetsAt: now.Add(2 * time.Hour)},
			{Meter: service.UsageMeterWeekly, UsedPercent: 91.5, ResetsAt: now.Add(5*24*time.Hour + time.Hour)},
			{Meter: service.UsageMeterWeeklyOpus, UsedPercent: 3},
		}},
		{Provider: "codex", State: service.UsageStale, Stale: true, Reason: service.UsageReasonUnreachable, AsOf: &old, Windows: []service.UsageWindow{
			{Meter: service.UsageMeterWeekly, UsedPercent: 5, ResetsAt: now.Add(-time.Hour)},
		}},
	}}
	want := `Claude (max), as of 2m ago
  5-hour         42%  resets in 2h
  weekly         92%  resets in 5d 1h
  weekly Opus     3%
Codex, stale: as of 3h ago (could not reach it)
  weekly          5%  reset 1h ago
`
	if got := formatSubscriptionUsage(snap, now); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}

	unavailable := service.SubscriptionUsage{Providers: []service.ProviderUsage{
		{Provider: "claude", State: service.UsageUnavailable, Reason: service.UsageReasonNotSignedIn},
		{Provider: "codex", State: service.UsageUnavailable, Reason: service.UsageReasonCodexMissing},
	}}
	want = `Claude: unavailable (sign in to it on this machine)
Codex: unavailable (the codex command is not installed)
`
	if got := formatSubscriptionUsage(unavailable, now); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if got := formatSubscriptionUsage(service.SubscriptionUsage{}, now); got != "Subscription usage is off (usage.claude.enabled and usage.codex.enabled).\n" {
		t.Fatalf("none: %q", got)
	}
}
