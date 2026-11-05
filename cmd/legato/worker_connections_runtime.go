package main

import (
	"context"

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
	return &mainHandoffs{
		workers:     workers,
		handoffs:    service.NewTaskHandoffService(db, service.TaskHandoffOptions{Workers: workers, Bus: bus}),
		connections: connections,
		stop:        connections.Start(context.Background()),
	}
}
