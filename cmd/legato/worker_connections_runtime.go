package main

import (
	"context"
	"sync"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// mainHandoffs is this Legato as a main instance: the workers it paired
// with, its handoffs to them, and the live connection to each worker that
// holds its away tasks. One per process, shared by its web servers and its
// manual sync.
type mainHandoffs struct {
	db          *store.Store
	workers     *service.WorkerService
	handoffs    *service.TaskHandoffService
	connections *service.WorkerConnections
	stop        func()

	// attention is the handoff Attention producer, made by the first
	// Attention service registered; watchStop stops its periodic check.
	attentionOnce sync.Once
	attention     *service.HandoffAttentionAdapter
	watchStop     func()
}

// startMainHandoffs builds this Legato's main side of handoff and starts
// its worker connections: while a worker holds away tasks, the main keeps
// an Events connection open to it and applies the rows it sends. See
// docs/ai/integrations.md § Live away state.
func startMainHandoffs(cfg *config.Config, db *store.Store, bus *events.Bus) *mainHandoffs {
	workers := service.NewWorkerService(db, service.WorkerServiceOptions{
		Secrets:  configuredSecretStore(cfg, db),
		MainName: pairingName(cfg),
		Bus:      bus,
	})
	connections := service.NewWorkerConnections(db, service.WorkerConnectionsOptions{Workers: workers, Bus: bus})
	handoffs := service.NewTaskHandoffService(db, service.TaskHandoffOptions{Workers: workers, Bus: bus})
	// Step 2 of a return the worker did not confirm goes on connect, and
	// returns a worker shows under way are finished.
	connections.SetOwedCalls(handoffs)
	stopReturns := handoffs.StartReturnLoop(context.Background(), returnLoopEvery)
	stopConnections := connections.Start(context.Background())
	m := &mainHandoffs{
		db:          db,
		workers:     workers,
		handoffs:    handoffs,
		connections: connections,
		watchStop:   func() {},
	}
	m.stop = func() {
		m.watchStop()
		stopReturns()
		stopConnections()
	}
	return m
}

// registerAttention shows this main's away tasks and workers in an
// Attention service: done, failed and lost tasks, and workers that no
// longer accept this main or have been unreachable for 15 minutes, with a
// phone notification (ntfy) for all but done. A process may have two
// Attention services (the TUI's and its web server's): the producer, its
// notifications and its periodic check are made once, with the first.
// See docs/ai/attention.md § Handoff.
func (m *mainHandoffs) registerAttention(attention *service.AttentionService, cfg *config.Config, serverURL func() string) {
	if m == nil || attention == nil {
		return
	}
	m.attentionOnce.Do(func() {
		opts := service.HandoffAttentionOptions{Handoffs: m.handoffs, ServerURL: serverURL}
		if cfg.Notifications.HandoffEnabled() && cfg.Notifications.Ntfy.Topic != "" {
			opts.Notifier = service.NewNtfyNotifier(cfg.Notifications.Ntfy.URL, cfg.Notifications.Ntfy.Topic, cfg.Notifications.Ntfy.Token)
		}
		m.attention = service.NewHandoffAttentionAdapter(m.db, opts)
		m.watchStop = m.attention.Watch(context.Background(), attention, handoffAttentionEvery)
	})
	mustRegister(m.attention.Register(attention))
}

// handoffAttentionEvery is how often a main looks again at its workers for
// one unreachable 15 minutes, besides when the board changes.
const handoffAttentionEvery = 30 * time.Second

// returnLoopEvery is how often a main looks again for returns under way and
// step 2 calls owed, besides when the board hears of a worker's row.
const returnLoopEvery = 30 * time.Second
