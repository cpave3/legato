package service

import (
	"context"
	"errors"
	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"testing"
	"time"
)

func TestUsagePolicyPersistsTwoSwitchesAndPinExpiry(t *testing.T) {
	f := newUsageFixture(t)
	ctx := context.Background()
	order := 1
	cfg := config.UsageConfig{Profiles: []config.UsageProfile{{Name: "claude", Agent: "claude-code"}, {Name: "codex", Agent: "chimera"}}, Rules: []config.UsageRule{{Name: "save", Order: &order, Meter: "claude.weekly", AtLeast: 90, Profile: "codex"}}, FallbackProfile: "claude"}
	notifier := &usageTestNotifier{}
	notificationsEnabled := true
	attention := NewAttentionService(f.db, events.New())
	if err := attention.RegisterProducer(NewUsageAttentionAdapter(f.db)); err != nil {
		t.Fatal(err)
	}
	policy := NewUsagePolicyService(UsagePolicyOptions{Store: f.db, Config: func(context.Context) (config.UsageConfig, error) { return cfg, nil }, Readings: f.svc.ReadingsSnapshot, Now: func() time.Time { return f.now }, OnSwitch: func(_ context.Context, state UsageProfileState, id int64) {
		NotifyUsageProfileSwitch(state, id, notificationsEnabled, notifier)
	}})
	f.svc.SetPolicy(policy, policy.Pin)
	f.claude.set(ProviderUsage{Windows: claudeWindows(50)}, nil)
	f.svc.Poll(ctx)
	if err := policy.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(5 * time.Minute)
	f.claude.set(ProviderUsage{Windows: claudeWindows(91)}, nil)
	f.svc.Poll(ctx)
	if err := policy.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	state, err := policy.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Active.Profile.Name != "codex" {
		t.Fatalf("state = %+v", state)
	}
	f.now = f.now.Add(5 * time.Minute)
	f.claude.set(ProviderUsage{}, errors.New("offline"))
	f.svc.Poll(ctx)
	if notifier.count != 1 {
		t.Fatal("stale data switched defaults")
	}
	// Recreate the coordinator to prove that the reset latch is durable.
	policy = NewUsagePolicyService(policy.opts)
	f.svc.SetPolicy(policy, policy.Pin)
	f.now = time.Date(2026, 12, 1, 9, 1, 0, 0, time.UTC)
	f.claude.set(ProviderUsage{Windows: claudeWindows(3)}, nil)
	f.svc.Poll(ctx)
	if err := policy.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	state, err = policy.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Active.Profile.Name != "claude" || len(state.Switches) != 2 || notifier.count != 2 {
		t.Fatalf("state=%+v notifications=%d", state, notifier.count)
	}
	if err := attention.Reconcile(ctx, "usage"); err != nil {
		t.Fatal(err)
	}
	items, err := attention.List(ctx, AttentionFilter{Producer: "usage"})
	if err != nil || len(items) != 2 {
		t.Fatalf("Attention items=%+v err=%v", items, err)
	}
	if err := attention.Reconcile(ctx, "usage"); err != nil {
		t.Fatal(err)
	}
	if notifier.count != 2 {
		t.Fatal("reconciliation notified again")
	}
	notificationsEnabled = false
	until := f.now.Add(time.Minute)
	if err := policy.Pin(ctx, &UsagePinRequest{Profile: "codex", Until: &until}); err != nil {
		t.Fatal(err)
	}
	f.now = until
	if err := policy.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	state, err = policy.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if notifier.count != 2 {
		t.Fatal("notifications.usage=false still notified")
	}
	if len(state.Switches) != 4 {
		t.Fatalf("disabled notifications lost switch history: %+v", state.Switches)
	}
	if state.Active.Pin != nil || state.Active.Profile.Name != "claude" {
		t.Fatalf("expired pin = %+v", state.Active)
	}
}

type usageTestNotifier struct{ count int }

func (n *usageTestNotifier) Configured() bool            { return true }
func (n *usageTestNotifier) CanNotify(string) bool       { return true }
func (n *usageTestNotifier) Notify(string, string) error { n.count++; return nil }

func TestUsageResetPinCapturesOneDeadlineAndRejectsUnknownResets(t *testing.T) {
	f := newUsageFixture(t)
	ctx := context.Background()
	cfg := config.UsageConfig{Profiles: []config.UsageProfile{{Name: "normal", Agent: "claude-code"}, {Name: "save", Agent: "chimera"}}, FallbackProfile: "normal"}
	policy := NewUsagePolicyService(UsagePolicyOptions{Store: f.db, Config: func(context.Context) (config.UsageConfig, error) { return cfg, nil }, Readings: f.svc.ReadingsSnapshot, Now: func() time.Time { return f.now }})
	if err := policy.Pin(ctx, &UsagePinRequest{Profile: "save", UntilReset: "claude.weekly"}); err == nil {
		t.Fatal("pin accepted an unknown reset")
	}
	f.claude.set(ProviderUsage{Windows: claudeWindows(50)}, nil)
	f.svc.Poll(ctx)
	if err := policy.Pin(ctx, &UsagePinRequest{Profile: "save", UntilReset: "claude.weekly"}); err != nil {
		t.Fatal(err)
	}
	first, err := policy.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	deadline := *first.Active.Pin.Until
	f.now = deadline.Add(time.Minute)
	windows := claudeWindows(3)
	windows[1].ResetsAt = deadline.Add(7 * 24 * time.Hour)
	f.claude.set(ProviderUsage{Windows: windows}, nil)
	f.svc.Poll(ctx)
	if err := policy.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := policy.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.Active.Pin != nil || after.Active.Profile.Name != "normal" {
		t.Fatalf("reset pin slid to the next deadline: %+v", after.Active)
	}
}

func TestUnpinWithNoFreshDataKeepsTheCurrentProfile(t *testing.T) {
	f := newUsageFixture(t)
	ctx := context.Background()
	order := 1
	cfg := config.UsageConfig{Profiles: []config.UsageProfile{{Name: "normal", Agent: "claude-code"}, {Name: "save", Agent: "chimera"}}, FallbackProfile: "normal", Rules: []config.UsageRule{{Name: "limit", Order: &order, Meter: "claude.weekly", AtLeast: 90, Profile: "save"}}}
	policy := NewUsagePolicyService(UsagePolicyOptions{Store: f.db, Config: func(context.Context) (config.UsageConfig, error) { return cfg, nil }, Readings: f.svc.ReadingsSnapshot, Now: func() time.Time { return f.now }})
	f.claude.set(ProviderUsage{Windows: claudeWindows(50)}, nil)
	f.svc.Poll(ctx)
	if err := policy.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := policy.Pin(ctx, &UsagePinRequest{Profile: "save"}); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Hour)
	if err := policy.Pin(ctx, nil); err != nil {
		t.Fatal(err)
	}
	state, err := policy.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Active.Pin != nil || state.Active.Profile.Name != "save" {
		t.Fatalf("no-data unpin switched: %+v", state.Active)
	}
}

func TestUsagePolicyExpiresPinsOnPollWithReadersOff(t *testing.T) {
	f := newUsageFixture(t)
	ctx := context.Background()
	usage := NewSubscriptionUsageService(SubscriptionUsageOptions{Store: f.db, Now: func() time.Time { return f.now }})
	cfg := config.UsageConfig{Profiles: []config.UsageProfile{{Name: "normal", Agent: "claude-code"}, {Name: "save", Agent: "chimera"}}, FallbackProfile: "normal"}
	policy := NewUsagePolicyService(UsagePolicyOptions{Store: f.db, Config: func(context.Context) (config.UsageConfig, error) { return cfg, nil }, Readings: usage.ReadingsSnapshot, Now: func() time.Time { return f.now }})
	usage.SetPolicy(policy, policy.Pin)
	usage.Poll(ctx)
	expiry := f.now.Add(time.Minute)
	if err := usage.Pin(ctx, &UsagePinRequest{Profile: "save", Until: &expiry}); err != nil {
		t.Fatal(err)
	}
	f.now = expiry
	usage.Poll(ctx)
	snap, err := usage.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Providers) != 0 || snap.Policy.Active.Pin != nil || snap.Policy.Active.Profile.Name != "normal" {
		t.Fatalf("readers-off poll = %+v", snap)
	}
}

func TestUsagePolicyRecordsTheFirstReadingAboveAThresholdAsASwitch(t *testing.T) {
	f := newUsageFixture(t)
	ctx := context.Background()
	order := 1
	notifier := &usageTestNotifier{}
	cfg := config.UsageConfig{Profiles: []config.UsageProfile{{Name: "normal", Agent: "claude-code"}, {Name: "save", Agent: "chimera"}}, FallbackProfile: "normal", Rules: []config.UsageRule{{Name: "limit", Order: &order, Meter: "claude.weekly", AtLeast: 90, Profile: "save"}}}
	policy := NewUsagePolicyService(UsagePolicyOptions{Store: f.db, Config: func(context.Context) (config.UsageConfig, error) { return cfg, nil }, Readings: f.svc.ReadingsSnapshot, Now: func() time.Time { return f.now }, OnSwitch: func(_ context.Context, state UsageProfileState, id int64) {
		NotifyUsageProfileSwitch(state, id, true, notifier)
	}})
	f.svc.SetPolicy(policy, policy.Pin)
	f.claude.set(ProviderUsage{Windows: claudeWindows(91)}, nil)
	f.svc.Poll(ctx)
	snap, err := f.svc.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Policy.Switches) != 1 || snap.Policy.Switches[0].From != "normal" || snap.Policy.Switches[0].To != "save" || notifier.count != 1 {
		t.Fatalf("startup threshold switch=%+v notifications=%d", snap.Policy, notifier.count)
	}
}
