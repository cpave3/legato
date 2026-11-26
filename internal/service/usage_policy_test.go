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
