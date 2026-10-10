package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestBridgeStopsWhenItsClaudeSessionIsGone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var parent atomic.Int64
	parent.Store(100)

	go cancelWhenOrphaned(ctx, cancel, 100, func() int { return int(parent.Load()) }, time.Millisecond)

	select {
	case <-ctx.Done():
		t.Fatal("stopped while its parent was still alive")
	case <-time.After(20 * time.Millisecond):
	}

	parent.Store(1) // reparented: the Claude Code process died

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("kept running after its parent was gone")
	}
}
