package main

import (
	"context"
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
	workers     *service.WorkerService
	handoffs    *service.TaskHandoffService
	connections *service.WorkerConnections
	stop        func()
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
	return &mainHandoffs{
		workers:     workers,
		handoffs:    handoffs,
		connections: connections,
		stop: func() {
			stopReturns()
			stopConnections()
		},
	}
}

// returnLoopEvery is how often a main looks again for returns under way and
// step 2 calls owed, besides when the board hears of a worker's row.
const returnLoopEvery = 30 * time.Second
