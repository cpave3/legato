package main

import (
	"context"
	"log"

	"github.com/cpave3/legato/config"
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
