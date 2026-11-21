package main

import (
	"context"
	"log"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// newSessionHistory is the session history, kept for agents.history.keep.
func newSessionHistory(cfg *config.Config, db *store.Store) *service.SessionHistoryService {
	keep, forever := cfg.Agents.History.KeepDuration()
	if forever {
		keep = 0
	}
	return service.NewSessionHistoryService(db, service.SessionHistoryOptions{Keep: keep})
}

// sweepSessionHistory removes the session history's old entries at the
// start and once a day, while this process serves the broker, so one
// Legato does it.
func (rt *approvalRuntime) sweepSessionHistory(history *service.SessionHistoryService) {
	if rt == nil || history == nil || rt.brokerOwner == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	sweep := func(ctx context.Context) {
		removed, err := history.Sweep(ctx)
		if err != nil {
			log.Printf("session history: sweeping old entries: %v", err)
		} else if removed > 0 {
			log.Printf("session history: removed %d entries past agents.history.keep", removed)
		}
	}
	go func() {
		if rt.brokerOwner() {
			sweep(ctx)
		}
		whileBrokerOwner(ctx, rt.brokerOwner, service.SessionHistorySweepEvery, sweep)
	}()
	stop := rt.stop
	rt.stop = func() {
		cancel()
		stop()
	}
}

// captureConversationText keeps the first prompt and the last message of
// each conversation as its turns end, in the process that serves the
// broker.
func (rt *approvalRuntime) captureConversationText(db *store.Store, bus *events.Bus, agents service.AgentService) {
	reader, ok := agents.(service.AgentConversationReader)
	if rt == nil || !ok || bus == nil || rt.brokerOwner == nil {
		return
	}
	capture := service.NewConversationTextCapture(db, reader, service.ConversationTextOptions{Owner: rt.brokerOwner})
	stopCapture := capture.StartEventLoop(context.Background(), bus)
	stop := rt.stop
	rt.stop = func() {
		stopCapture()
		stop()
	}
}
