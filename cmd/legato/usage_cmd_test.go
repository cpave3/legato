package main

import (
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
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
	if got := formatSubscriptionUsage(service.SubscriptionUsage{}, now); got != "Subscription usage is off (usage.claude.enabled, usage.codex.enabled and usage.opencode.enabled).\n" {
		t.Fatalf("none: %q", got)
	}
}

// OpenCode Go prints beside the others, with its monthly window and its own
// reasons.
func TestUsagePrintsOpenCodeGoWithItsMonthlyMeter(t *testing.T) {
	now := time.Date(2026, 11, 27, 8, 0, 0, 0, time.UTC)
	asOf := now.Add(-2 * time.Minute)
	snap := service.SubscriptionUsage{Providers: []service.ProviderUsage{
		{Provider: "opencode", State: service.UsageOK, AsOf: &asOf, Windows: []service.UsageWindow{
			{Meter: service.UsageMeterFiveHour, UsedPercent: 30, ResetsAt: now.Add(5 * time.Hour)},
			{Meter: service.UsageMeterWeekly, UsedPercent: 66, ResetsAt: now.Add(3 * 24 * time.Hour)},
			{Meter: service.UsageMeterMonthly, UsedPercent: 50, ResetsAt: now.Add(4 * 24 * time.Hour)},
		}},
	}}
	want := `OpenCode Go, as of 2m ago
  5-hour         30%  resets in 5h
  weekly         66%  resets in 3d
  monthly        50%  resets in 4d
`
	if got := formatSubscriptionUsage(snap, now); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}

	missingKey := service.SubscriptionUsage{Providers: []service.ProviderUsage{
		{Provider: "opencode", State: service.UsageUnavailable, Reason: service.UsageReasonMissingKey},
	}}
	want = "OpenCode Go: unavailable (name its API key in Settings › Usage)\n"
	if got := formatSubscriptionUsage(missingKey, now); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUsageTextListsEachRepositoryDefaultsCurrentProfile(t *testing.T) {
	snap := service.SubscriptionUsage{Policy: &service.UsagePolicySnapshot{
		Active: service.UsageProfileState{Initialized: true, Profile: config.UsageProfile{Name: "Default"}, Reason: service.UsageProfileReason{Text: "No usage rule matches; using fallback defaults"}},
		Scopes: []service.UsagePolicyScope{{Key: "work", Baseline: "Work", Repositories: []string{"autopilot-dev/autopilot"},
			Active: service.UsageProfileState{Profile: config.UsageProfile{Name: "Codex"}, Reason: service.UsageProfileReason{Text: "claude.weekly reached 90% (work-weekly)"}}}},
	}}
	got := formatSubscriptionUsage(snap, time.Now())
	want := "New work in autopilot-dev/autopilot (default Work): Codex — claude.weekly reached 90% (work-weekly)\n"
	if !strings.Contains(got, want) {
		t.Fatalf("text = %q", got)
	}
}
