package main

import (
	"testing"

	"github.com/cpave3/legato/config"
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
