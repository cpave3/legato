package main

import (
	"sync"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// processWorkStages holds this process's agent stages for each store, so
// every agent service and web server of the process applies one order of
// trust under one lock.
var processWorkStages sync.Map

// workStagesFor is this process's agent stages over db, publishing on bus
// (the bus of the first call).
func workStagesFor(db *store.Store, bus *events.Bus) *service.WorkStageService {
	if s, ok := processWorkStages.Load(db); ok {
		return s.(*service.WorkStageService)
	}
	actual, _ := processWorkStages.LoadOrStore(db, service.NewWorkStageService(db, bus))
	return actual.(*service.WorkStageService)
}

// giveAgentsWorkStages lets an agent service reset a place's stage when it
// starts a new conversation there.
func giveAgentsWorkStages(agents service.AgentService, db *store.Store, bus *events.Bus) {
	if agents == nil {
		return
	}
	if setter, ok := agents.(interface {
		SetWorkStages(*service.WorkStageService)
	}); ok {
		setter.SetWorkStages(workStagesFor(db, bus))
	}
}

// wireWorkStages gives a web server the agent stage routes and has it tell
// web clients of each change (agent_stage_changed).
func wireWorkStages(srv *server.Server, db *store.Store, bus *events.Bus) {
	srv.SetWorkStageService(workStagesFor(db, bus))
	srv.StartAgentStageEvents()
}
