package service

import (
	"github.com/cpave3/legato/config"
	"testing"
	"time"
)

func TestUsageRuleHoldsUntilAFreshLowReadingAfterReset(t *testing.T) {
	now := time.Date(2026, 11, 26, 8, 0, 0, 0, time.UTC)
	reset := now.Add(time.Hour)
	order := 1
	rules := []config.UsageRule{{Name: "conserve", Order: &order, Meter: "claude.weekly", AtLeast: 90, Profile: "codex"}}
	profiles := []config.UsageProfile{{Name: "claude", Agent: "claude-code"}, {Name: "codex", Agent: "chimera", Tier: "codex"}}
	reading := func(value float64, asOf time.Time, state UsageState) []ProviderUsage {
		return []ProviderUsage{{Provider: "claude", State: state, AsOf: &asOf, Windows: []UsageWindow{{Meter: "weekly", UsedPercent: value, ResetsAt: reset}}}}
	}
	state := UsageRuleLatch{Current: "claude"}
	profile, _, state := EvaluateUsageRules(rules, profiles, "claude", reading(91, now, UsageOK), state, now)
	if profile != "codex" {
		t.Fatalf("threshold selected %q", profile)
	}
	for _, step := range []struct {
		value    float64
		asOf, at time.Time
		status   UsageState
		want     string
	}{
		{3, now, now.Add(time.Minute), UsageOK, "codex"},
		{3, now, reset.Add(time.Minute), UsageOK, "codex"},
		{3, reset, reset.Add(time.Minute), UsageStale, "codex"},
		{3, reset, reset.Add(time.Minute), UsageOK, "claude"},
	} {
		profile, _, state = EvaluateUsageRules(rules, profiles, "claude", reading(step.value, step.asOf, step.status), state, step.at)
		if profile != step.want {
			t.Fatalf("at %v as of %v state %s selected %q, want %q", step.at, step.asOf, step.status, profile, step.want)
		}
	}
}

func TestUsageRulesOrderMissingDataAndPolicyEdits(t *testing.T) {
	now := time.Now().UTC()
	reset := now.Add(time.Hour)
	order := 1
	profiles := []config.UsageProfile{{Name: "base", Agent: "claude-code"}, {Name: "save", Agent: "chimera"}, {Name: "reserve", Agent: "codex"}}
	rules := []config.UsageRule{{Name: "z", Order: &order, Meter: "claude.weekly", AtLeast: 90, Profile: "save"}, {Name: "a", Order: &order, Meter: "codex.weekly", AtLeast: 80, Profile: "reserve"}}
	readings := []ProviderUsage{{Provider: "claude", State: UsageOK, AsOf: &now, Windows: []UsageWindow{{Meter: "weekly", UsedPercent: 95, ResetsAt: reset}}}, {Provider: "codex", State: UsageOK, AsOf: &now, Windows: []UsageWindow{{Meter: "weekly", UsedPercent: 85, ResetsAt: reset}}}}
	selected, _, latch := EvaluateUsageRules(rules, profiles, "base", readings, UsageRuleLatch{Current: "base"}, now)
	if selected != "reserve" {
		t.Fatalf("tie order selected %s", selected)
	}
	readings[1].State = UsageUnavailable
	selected, _, _ = EvaluateUsageRules(rules, profiles, "base", readings, UsageRuleLatch{Current: "base"}, now)
	if selected != "save" {
		t.Fatalf("valid match with another unavailable meter = %s", selected)
	}
	readings[0].Windows[0].UsedPercent = 5
	selected, _, _ = EvaluateUsageRules(rules, profiles, "base", readings, UsageRuleLatch{Current: "save"}, now)
	if selected != "save" {
		t.Fatalf("missing data fell back to %s", selected)
	}
	// Editing the latched rule invalidates it, rather than interpreting old
	// evidence with a new threshold/profile. Fresh low data selects fallback.
	rules[1].AtLeast = 99
	readings[1].State = UsageOK
	readings[1].Windows[0].UsedPercent = 5
	selected, _, latch = EvaluateUsageRules(rules, profiles, "base", readings, latch, now)
	if selected != "base" || latch.Rule != "" {
		t.Fatalf("edited rule kept old latch: %s %+v", selected, latch)
	}
	selected, _, _ = EvaluateUsageRules(rules, profiles, "", readings, latch, now)
	if selected != "" {
		t.Fatalf("no fallback did not return static defaults: %s", selected)
	}
}

// A rule on OpenCode Go's weekly window behaves like one on Claude's: the
// threshold switches, the latch holds until the meter's own reset, and a
// fresh reading below it releases.
func TestUsageRuleOnOpenCodeWeeklyHoldsUntilItsReset(t *testing.T) {
	now := time.Date(2026, 11, 27, 8, 0, 0, 0, time.UTC)
	reset := now.Add(3 * 24 * time.Hour)
	order := 1
	rules := []config.UsageRule{{Name: "conserve", Order: &order, Meter: "opencode.weekly", AtLeast: 75, Profile: "save"}}
	profiles := []config.UsageProfile{{Name: "base", Agent: "claude-code"}, {Name: "save", Agent: "chimera"}}
	reading := func(value float64, asOf time.Time, state UsageState) []ProviderUsage {
		return []ProviderUsage{{Provider: UsageProviderOpenCode, State: state, AsOf: &asOf, Windows: []UsageWindow{
			{Meter: UsageMeterWeekly, UsedPercent: value, ResetsAt: reset},
			{Meter: UsageMeterMonthly, UsedPercent: value, ResetsAt: reset.Add(24 * time.Hour)},
		}}}
	}
	state := UsageRuleLatch{Current: "base"}
	profile, reason, state := EvaluateUsageRules(rules, profiles, "base", reading(80, now, UsageOK), state, now)
	if profile != "save" || reason.Rule != "conserve" || !reason.Reset.Equal(reset) {
		t.Fatalf("profile = %q, reason = %+v", profile, reason)
	}
	// Below the threshold, but before the reset: held.
	profile, _, state = EvaluateUsageRules(rules, profiles, "base", reading(3, reset.Add(-time.Hour), UsageOK), state, reset.Add(-time.Hour))
	if profile != "save" {
		t.Fatalf("released before reset: %q", profile)
	}
	// After the reset, a fresh low reading releases the latch.
	profile, _, state = EvaluateUsageRules(rules, profiles, "base", reading(3, reset.Add(time.Minute), UsageOK), state, reset.Add(time.Minute))
	if profile != "base" {
		t.Fatalf("held after a fresh low reading: %q", profile)
	}
}

// A rule on OpenCode Go's monthly meter is as qualified as one on any other
// window; missing Go evidence holds the current defaults.
func TestAMissingOpenCodeReadingHoldsTheCurrentDefaults(t *testing.T) {
	now := time.Now().UTC()
	order := 1
	rules := []config.UsageRule{{Name: "monthly-cap", Order: &order, Meter: "opencode.monthly", AtLeast: 80, Profile: "save"}}
	profiles := []config.UsageProfile{{Name: "base", Agent: "claude-code"}, {Name: "save", Agent: "chimera"}}
	unavailable := []ProviderUsage{{Provider: UsageProviderOpenCode, State: UsageUnavailable, Reason: UsageReasonNotRead}}
	profile, reason, _ := EvaluateUsageRules(rules, profiles, "base", unavailable, UsageRuleLatch{Current: "save"}, now)
	if profile != "save" || reason.Text != "Usage data is unavailable or stale; keeping current defaults" {
		t.Fatalf("profile = %q, reason = %q", profile, reason.Text)
	}
	ok := []ProviderUsage{{Provider: UsageProviderOpenCode, State: UsageOK, AsOf: &now, Windows: []UsageWindow{{Meter: UsageMeterMonthly, UsedPercent: 95, ResetsAt: now.Add(4 * 24 * time.Hour)}}}}
	profile, _, _ = EvaluateUsageRules(rules, profiles, "base", ok, UsageRuleLatch{Current: "base"}, now)
	if profile != "save" {
		t.Fatalf("profile = %q", profile)
	}
}
