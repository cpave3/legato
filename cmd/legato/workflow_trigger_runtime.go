package main

import (
	"sort"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

func buildWorkflowTriggerCoordinator(db *store.Store, workflows *service.WorkflowService, cfg *config.Config, bus ...*events.Bus) *service.WorkflowTriggerCoordinator {
	global, project := workflowDirs()
	coordinator := service.NewWorkflowTriggerCoordinator(db, workflows, func() workflowengine.DiscoveryResult {
		return injectBuiltInLearningTriggers(workflowengine.DiscoverPaths(global, project), cfg)
	})
	if len(bus) > 0 {
		coordinator.SetEventBus(bus[0])
	}
	return coordinator
}

func injectBuiltInLearningTriggers(result workflowengine.DiscoveryResult, cfg *config.Config) workflowengine.DiscoveryResult {
	if cfg == nil || !cfg.Learning.Enabled {
		return result
	}
	definitions, err := service.BuiltInTriggeredLearningWorkflows(cfg.Learning.TransitionColumns, service.LearningWorkflowOptions{
		Adapter: firstNonEmpty(cfg.Learning.CuratorAdapter, cfg.Swarm.DefaultAgent), Tier: cfg.Learning.CuratorTier,
		WorkingDir: cfg.Learning.CuratorWorkingDir, PacketMaxItems: cfg.Learning.PacketMaxItems,
		PacketMaxBytes: cfg.Learning.PacketMaxBytes,
	})
	if err != nil {
		result.Errors = append(result.Errors, workflowengine.DiscoveryError{Path: "builtin:learning", Error: err.Error()})
		return result
	}
	result.Definitions = append(result.Definitions, definitions...)
	sort.Slice(result.Definitions, func(i, j int) bool { return result.Definitions[i].Name < result.Definitions[j].Name })
	return result
}
