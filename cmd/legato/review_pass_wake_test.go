package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/service"
)

// startReviewPassWake runs the loop on channels of its own, with a timer that
// never fires in a test, and counts its ticks.
func startReviewPassWake(t *testing.T, gap time.Duration) (moved, runs chan events.Event, ticks *atomic.Int32) {
	t.Helper()
	moved, runs = make(chan events.Event, 16), make(chan events.Event, 16)
	ticks = &atomic.Int32{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go reviewPassWake(ctx, moved, runs, time.Hour, gap, func(context.Context) { ticks.Add(1) })
	return moved, runs, ticks
}

func workflowChange(kind string) events.Event {
	return events.Event{Type: events.EventWorkflowChanged, Payload: events.WorkflowChangedPayload{RunID: "wr-1", Kind: kind}}
}

func TestAReviewLoopsOwnChangeDoesNotTickIt(t *testing.T) {
	_, runs, ticks := startReviewPassWake(t, 10*time.Millisecond)
	runs <- workflowChange(service.ReviewLoopChangedKind)
	time.Sleep(100 * time.Millisecond)
	if got := ticks.Load(); got != 0 {
		t.Fatalf("ticks = %d", got)
	}
}

// A run moving on can start a review loop, whose first pass should not wait
// for the timer; a burst of its step changes is one tick, not one each.
func TestABurstOfRunChangesTicksOnce(t *testing.T) {
	_, runs, ticks := startReviewPassWake(t, 80*time.Millisecond)
	for _, kind := range []string{service.WorkflowChangeStepCompleted, service.WorkflowChangeStepStarted, service.WorkflowChangeStepWaiting, "advanced"} {
		runs <- workflowChange(kind)
	}
	time.Sleep(40 * time.Millisecond)
	if got := ticks.Load(); got != 0 {
		t.Fatalf("ticked before the gap: %d", got)
	}
	time.Sleep(120 * time.Millisecond)
	if got := ticks.Load(); got != 1 {
		t.Fatalf("ticks = %d, want 1", got)
	}
	// A later change is a tick of its own.
	runs <- workflowChange(service.WorkflowChangeStepWaiting)
	time.Sleep(160 * time.Millisecond)
	if got := ticks.Load(); got != 2 {
		t.Fatalf("ticks = %d, want 2", got)
	}
}

func TestACardMoveTicksAtOnce(t *testing.T) {
	moved, _, ticks := startReviewPassWake(t, time.Hour)
	moved <- events.Event{Type: events.EventCardMoved}
	deadline := time.Now().Add(time.Second)
	for ticks.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := ticks.Load(); got != 1 {
		t.Fatalf("ticks = %d", got)
	}
}
