package service

import (
	"context"
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
	attention := NewAttentionService(f.db, events.New())
	if err := attention.RegisterProducer(NewUsageAttentionAdapter(f.db)); err != nil {
		t.Fatal(err)
	}
	policy := NewUsagePolicyService(UsagePolicyOptions{Store: f.db, Config: func(context.Context) (config.UsageConfig, error) { return cfg, nil }, Readings: f.svc.ReadingsSnapshot, Now: func() time.Time { return f.now }, OnSwitch: func(_ context.Context, state UsageProfileState, id int64) {
		NotifyUsageProfileSwitch(state, id, true, notifier)
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
	if state.Active.Pin != nil || state.Active.Profile.Name != "claude" {
		t.Fatalf("expired pin = %+v", state.Active)
	}
}

type usageTestNotifier struct{ count int }

func (n *usageTestNotifier) Configured() bool            { return true }
func (n *usageTestNotifier) CanNotify(string) bool       { return true }
func (n *usageTestNotifier) Notify(string, string) error { n.count++; return nil }
