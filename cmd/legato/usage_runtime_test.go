package main

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// Only one process of a data folder polls the subscriptions' usage.
func TestOneProcessHoldsTheUsagePoller(t *testing.T) {
	dir := t.TempDir()
	release, err := claimUsagePoller(dir)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := claimUsagePoller(dir); err == nil {
		again()
		t.Fatal("a second claim succeeded while the first holds it")
	}
	release()
	again, err := claimUsagePoller(dir)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	again()
}

func TestUsageReadersFollowTheSettings(t *testing.T) {
	off := false
	cfg := config.Defaults()
	if got := usageReaders(cfg); len(got) != 2 || got[0].Provider() != "claude" || got[1].Provider() != "codex" {
		t.Fatalf("readers = %v", got)
	}
	cfg.Usage.Codex.Enabled = &off
	if got := usageReaders(cfg); len(got) != 1 || got[0].Provider() != "claude" {
		t.Fatalf("codex off: readers = %v", got)
	}
	cfg.Usage.Claude.Enabled = &off
	if subscriptionUsageFor(cfg, nil, nil) != nil {
		t.Fatal("a service with both providers off")
	}
}

type countingUsageReader struct{ reads atomic.Int32 }

func (r *countingUsageReader) Provider() string { return service.UsageProviderClaude }
func (r *countingUsageReader) Read(context.Context) (service.ProviderUsage, error) {
	r.reads.Add(1)
	return service.ProviderUsage{Windows: []service.UsageWindow{{Meter: service.UsageMeterWeekly, UsedPercent: 1}}}, nil
}

// A process that started while another polled takes the poller over once
// that one lets go of usage.lock.
func TestASurvivingProcessTakesOverThePoller(t *testing.T) {
	old := usageClaimRetry
	usageClaimRetry = 20 * time.Millisecond
	t.Cleanup(func() { usageClaimRetry = old })

	dir := t.TempDir()
	db, err := store.New(filepath.Join(dir, "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := config.Defaults()
	cfg.DataDir = dir
	reader := &countingUsageReader{}
	svc := service.NewSubscriptionUsageService(service.SubscriptionUsageOptions{Store: db, Readers: []service.SubscriptionUsageReader{reader}})

	release, err := claimUsagePoller(dir)
	if err != nil {
		t.Fatal(err)
	}
	stop := startSubscriptionUsage(cfg, svc, events.New())
	time.Sleep(150 * time.Millisecond)
	if n := reader.reads.Load(); n != 0 {
		t.Fatalf("reads = %d while another process polls", n)
	}
	release()
	deadline := time.Now().Add(3 * time.Second)
	for reader.reads.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if reader.reads.Load() == 0 {
		t.Fatal("the poller was not taken over after the lock was released")
	}
	stopped := make(chan struct{})
	go func() { stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("stopping hung")
	}
	if again, err := claimUsagePoller(dir); err != nil {
		t.Fatalf("the lock was kept after stopping: %v", err)
	} else {
		again()
	}
}
