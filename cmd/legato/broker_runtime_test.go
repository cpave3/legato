package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/broker"
	"github.com/cpave3/legato/internal/engine/store"
)


func TestOneHostServesTheBrokerAndAnotherTakesOver(t *testing.T) {
	dir := shortDir(t)
	db, err := store.New(filepath.Join(t.TempDir(), "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	first := startBrokerHost(ctx, db, dir, brokerHostOptions{Retry: 50 * time.Millisecond})
	second := startBrokerHost(ctx, db, dir, brokerHostOptions{Retry: 50 * time.Millisecond})
	defer second.Stop()
	if !first.Owner() || second.Owner() {
		t.Fatalf("owners: first %v, second %v", first.Owner(), second.Owner())
	}
	if first.SocketPath() != filepath.Join(dir, "broker.sock") || second.SocketPath() != first.SocketPath() {
		t.Fatalf("paths: %q %q", first.SocketPath(), second.SocketPath())
	}
	// It answers: a token that names nothing is refused by the host's rules.
	var be *broker.Error
	if err := broker.Call(ctx, first.SocketPath(), "lgb_none", "agent.state", nil, nil); !errors.As(err, &be) || be.Code != "unauthorized" {
		t.Fatalf("call: %v", err)
	}
	first.Stop()
	deadline := time.Now().Add(2 * time.Second)
	for !second.Owner() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !second.Owner() {
		t.Fatal("the second host did not take over")
	}
	if err := broker.Call(ctx, second.SocketPath(), "lgb_none", "agent.state", nil, nil); !errors.As(err, &be) {
		t.Fatalf("after takeover: %v", err)
	}
}
