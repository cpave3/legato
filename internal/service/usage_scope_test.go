package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
)

// usageScopeConfig is Cameron's case with synthetic names: one repository
// defaults to Work, cpave3/* and everything else to Default, and a rule that
// only Work's repositories obey moves them to Codex near Work's limit.
func usageScopeConfig() config.UsageConfig {
	order := 1
	return config.UsageConfig{
		Profiles: []config.UsageProfile{
			{Name: "Default", Agent: "claude-code"},
			{Name: "Work", Agent: "claude-code", Account: "work"},
			{Name: "Codex", Agent: "codex"},
		},
		FallbackProfile: "Default",
		Repositories: []config.UsageRepositoryDefault{
			{Repository: "autopilot-dev/autopilot", Profile: "Work"},
			{Repository: "cpave3/*", Profile: "Default"},
		},
		Rules: []config.UsageRule{{Name: "work-weekly", Order: &order, Meter: "claude.weekly", AtLeast: 90, Profile: "Codex", WhenDefault: []string{"Work"}}},
	}
}

func usageDefaultsFor(t *testing.T, f *usageFixture, cfg config.UsageConfig, repo string) UsageSelection {
	t.Helper()
	sel, err := UsageDefaultsFor(context.Background(), f.db, cfg, repo)
	if err != nil {
		t.Fatal(err)
	}
	return sel
}

func TestRepositoryScopesSwitchAndReturnToTheirOwnDefault(t *testing.T) {
	f := newUsageFixture(t)
	ctx := context.Background()
	cfg := usageScopeConfig()
	reset := f.now.Add(time.Hour)
	windows := func(v float64) []UsageWindow {
		w := claudeWindows(v)
		for i := range w {
			w[i].ResetsAt = reset
		}
		return w
	}
	policy := NewUsagePolicyService(UsagePolicyOptions{Store: f.db, Config: func(context.Context) (config.UsageConfig, error) { return cfg, nil }, Readings: f.svc.ReadingsSnapshot, Now: func() time.Time { return f.now }})
	f.svc.SetPolicy(policy, policy.Pin)

	f.claude.set(ProviderUsage{Windows: windows(50)}, nil)
	f.svc.Poll(ctx)
	if sel := usageDefaultsFor(t, f, cfg, "autopilot-dev/autopilot"); sel.Profile.Name != "Work" || sel.Profile.Account != "work" || sel.Source != UsageSourceRepository || sel.Matched != "autopilot-dev/autopilot" {
		t.Fatalf("work repository = %+v", sel)
	}
	if sel := usageDefaultsFor(t, f, cfg, "cpave3/legato"); sel.Profile.Name != "Default" || sel.Source != UsageSourceOwner || sel.Matched != "cpave3/*" {
		t.Fatalf("owner repository = %+v", sel)
	}
	if sel := usageDefaultsFor(t, f, cfg, "someone/else"); sel.Profile.Name != "Default" || sel.Source != UsageSourceFallback {
		t.Fatalf("unmatched repository = %+v", sel)
	}
	snap, _ := policy.Snapshot(ctx)
	if len(snap.Switches) != 0 {
		t.Fatalf("baselines recorded switches: %+v", snap.Switches)
	}

	// Work's limit: only Work's repositories switch.
	f.now = f.now.Add(5 * time.Minute)
	f.claude.set(ProviderUsage{Windows: windows(91)}, nil)
	f.svc.Poll(ctx)
	if sel := usageDefaultsFor(t, f, cfg, "autopilot-dev/autopilot"); sel.Profile.Name != "Codex" || sel.Source != UsageSourceRule || sel.Baseline != "Work" || sel.Reason.Rule != "work-weekly" {
		t.Fatalf("work repository at its limit = %+v", sel)
	}
	for _, repo := range []string{"cpave3/legato", "someone/else", ""} {
		if sel := usageDefaultsFor(t, f, cfg, repo); sel.Profile.Name != "Default" {
			t.Fatalf("%q followed a rule it is not eligible for: %+v", repo, sel)
		}
	}
	snap, _ = policy.Snapshot(ctx)
	if len(snap.Switches) != 1 || snap.Switches[0].Scope != "work" || snap.Switches[0].From != "Work" || snap.Switches[0].To != "Codex" {
		t.Fatalf("switches = %+v", snap.Switches)
	}
	if snap.Active.Profile.Name != "Default" {
		t.Fatalf("global scope moved: %+v", snap.Active)
	}
	if len(snap.Scopes) != 1 || snap.Scopes[0].Baseline != "Work" || snap.Scopes[0].Active.Profile.Name != "Codex" || len(snap.Scopes[0].Repositories) != 1 {
		t.Fatalf("scopes = %+v", snap.Scopes)
	}

	// Stale evidence holds, even across a new coordinator.
	f.now = f.now.Add(5 * time.Minute)
	f.claude.set(ProviderUsage{}, errors.New("offline"))
	f.svc.Poll(ctx)
	policy = NewUsagePolicyService(policy.opts)
	f.svc.SetPolicy(policy, policy.Pin)
	f.now = reset.Add(time.Minute)
	f.svc.Poll(ctx)
	if sel := usageDefaultsFor(t, f, cfg, "autopilot-dev/autopilot"); sel.Profile.Name != "Codex" {
		t.Fatalf("stale reading after reset released the latch: %+v", sel)
	}

	// A fresh low reading after the reset returns the scope to Work, not
	// to the global fallback.
	f.now = f.now.Add(31 * time.Minute) // past the failure's back-off
	f.claude.set(ProviderUsage{Windows: windows(3)}, nil)
	f.svc.Poll(ctx)
	if sel := usageDefaultsFor(t, f, cfg, "autopilot-dev/autopilot"); sel.Profile.Name != "Work" || sel.Source != UsageSourceRepository {
		t.Fatalf("after reset = %+v", sel)
	}
	snap, _ = policy.Snapshot(ctx)
	if len(snap.Switches) != 2 || snap.Switches[0].To != "Work" || snap.Switches[0].Scope != "work" {
		t.Fatalf("switches = %+v", snap.Switches)
	}
}

func TestTheOperatorPinOverridesEveryRepositoryDefault(t *testing.T) {
	f := newUsageFixture(t)
	ctx := context.Background()
	cfg := usageScopeConfig()
	policy := NewUsagePolicyService(UsagePolicyOptions{Store: f.db, Config: func(context.Context) (config.UsageConfig, error) { return cfg, nil }, Readings: f.svc.ReadingsSnapshot, Now: func() time.Time { return f.now }})
	f.svc.SetPolicy(policy, policy.Pin)
	f.claude.set(ProviderUsage{Windows: claudeWindows(50)}, nil)
	f.svc.Poll(ctx)
	until := f.now.Add(time.Minute)
	if err := policy.Pin(ctx, &UsagePinRequest{Profile: "codex", Until: &until}); err != nil {
		t.Fatal(err)
	}
	for _, repo := range []string{"autopilot-dev/autopilot", "cpave3/legato", ""} {
		if sel := usageDefaultsFor(t, f, cfg, repo); sel.Profile.Name != "Codex" || sel.Source != UsageSourcePin {
			t.Fatalf("%q under a pin = %+v", repo, sel)
		}
	}
	f.now = until
	if err := policy.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	if sel := usageDefaultsFor(t, f, cfg, "autopilot-dev/autopilot"); sel.Profile.Name != "Work" || sel.Source != UsageSourceRepository {
		t.Fatalf("after the pin expired = %+v", sel)
	}
	if sel := usageDefaultsFor(t, f, cfg, "cpave3/legato"); sel.Profile.Name != "Default" {
		t.Fatalf("after the pin expired = %+v", sel)
	}
}

func TestARepositoryDefaultAppliesBeforeItsFirstEvaluationAndIsCollectedWhenRemoved(t *testing.T) {
	f := newUsageFixture(t)
	ctx := context.Background()
	cfg := usageScopeConfig()
	cfg.Repositories = nil
	policy := NewUsagePolicyService(UsagePolicyOptions{Store: f.db, Config: func(context.Context) (config.UsageConfig, error) { return cfg, nil }, Readings: f.svc.ReadingsSnapshot, Now: func() time.Time { return f.now }})
	f.svc.SetPolicy(policy, policy.Pin)
	f.claude.set(ProviderUsage{Windows: claudeWindows(50)}, nil)
	f.svc.Poll(ctx)
	if sel := usageDefaultsFor(t, f, cfg, "autopilot-dev/autopilot"); sel.Profile.Name != "Default" || sel.Source != UsageSourceFallback {
		t.Fatalf("no repository defaults = %+v", sel)
	}
	snap, _ := policy.Snapshot(ctx)
	if len(snap.Scopes) != 0 {
		t.Fatalf("scopes without repository defaults = %+v", snap.Scopes)
	}
	// Saved, not yet evaluated: the baseline already applies.
	cfg = usageScopeConfig()
	if sel := usageDefaultsFor(t, f, cfg, "autopilot-dev/autopilot"); sel.Profile.Name != "Work" || sel.Source != UsageSourceRepository {
		t.Fatalf("before evaluation = %+v", sel)
	}
	if err := policy.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	snap, _ = policy.Snapshot(ctx)
	if len(snap.Scopes) != 1 || len(snap.Switches) != 0 {
		t.Fatalf("seeded scope = %+v switches %+v", snap.Scopes, snap.Switches)
	}
	cfg.Repositories = nil
	if err := policy.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	snap, _ = policy.Snapshot(ctx)
	if len(snap.Scopes) != 0 {
		t.Fatalf("removed repository default kept its scope: %+v", snap.Scopes)
	}
	if sel := usageDefaultsFor(t, f, cfg, "autopilot-dev/autopilot"); sel.Profile.Name != "Default" {
		t.Fatalf("after removal = %+v", sel)
	}
}

func TestAScopeWhoseProfileCannotLaunchKeepsItsStateWhileOthersMove(t *testing.T) {
	f := newUsageFixture(t)
	ctx := context.Background()
	cfg := usageScopeConfig()
	order := 2
	cfg.Rules = append(cfg.Rules, config.UsageRule{Name: "all-weekly", Order: &order, Meter: "claude.weekly", AtLeast: 95, Profile: "Default"})
	cfg.Profiles[2].Tier = "missing"
	policy := NewUsagePolicyService(UsagePolicyOptions{Store: f.db, Config: func(context.Context) (config.UsageConfig, error) { return cfg, nil }, Readings: f.svc.ReadingsSnapshot, Now: func() time.Time { return f.now }, Validate: func(p config.UsageProfile) error {
		if p.Tier == "missing" {
			return errors.New("tier missing is not installed")
		}
		return nil
	}})
	f.svc.SetPolicy(policy, policy.Pin)
	f.claude.set(ProviderUsage{Windows: claudeWindows(50)}, nil)
	f.svc.Poll(ctx)
	f.claude.set(ProviderUsage{Windows: claudeWindows(91)}, nil)
	f.now = f.now.Add(5 * time.Minute)
	f.svc.Poll(ctx)
	snap, err := policy.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Scopes) != 1 || snap.Scopes[0].Active.Profile.Name != "Work" || snap.Scopes[0].Active.Error == "" {
		t.Fatalf("failing scope = %+v", snap.Scopes)
	}
	if snap.Active.Profile.Name != "Default" || snap.Active.Error != "" {
		t.Fatalf("global scope = %+v", snap.Active)
	}
}
