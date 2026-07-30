package main

import (
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

func buildWorkflowTriggerCoordinator(db *store.Store, workflows *service.WorkflowService, bus ...*events.Bus) *service.WorkflowTriggerCoordinator {
	global, project := workflowDirs()
	coordinator := service.NewWorkflowTriggerCoordinator(db, workflows, func() workflowengine.DiscoveryResult {
		return workflowengine.DiscoverPaths(global, project)
	})
	if len(bus) > 0 {
		coordinator.SetEventBus(bus[0])
	}
	return coordinator
}
