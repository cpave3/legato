package main

import (
	"context"
	"log"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

// workflowDiscoveryProvider reads every workflow folder, each
// repository's included (the configuration's and, with db, those added in
// the web UI), at each call.
func workflowDiscoveryProvider(cfg *config.Config, db *store.Store) func() workflowengine.DiscoveryResult {
	repositories := workflowRepositoriesFor(cfg, db)
	return func() workflowengine.DiscoveryResult {
		presets := repositories()
		result := workflowengine.WithBuiltIns(workflowengine.DiscoverFolders(workflowFolders(presets)), service.BuiltInWorkflows()...)
		result.Repositories = presets
		return result
	}
}

// workflowRepositoriesFor lists the repository presets: the
// configuration's, and with db those added in the web UI.
func workflowRepositoriesFor(cfg *config.Config, db *store.Store) func() []workflowengine.RepositoryPreset {
	if db == nil {
		return func() []workflowengine.RepositoryPreset { return workflowRepositoryPresets(cfg) }
	}
	presets := newRepositoryPresetService(cfg, db, nil)
	return func() []workflowengine.RepositoryPreset {
		listed, err := presets.WorkflowPresets(context.Background())
		if err != nil {
			log.Printf("workflows: listing repositories: %v", err)
			return workflowRepositoryPresets(cfg)
		}
		return listed
	}
}

// workflowFolders are the folders workflows are read from, in increasing
// precedence: legacy, global, each repository's, the project's.
func workflowFolders(repositories []workflowengine.RepositoryPreset) []workflowengine.Folder {
	global, project := workflowDirs()
	return workflowengine.Folders(legacyWorkflowDir(), global, repositories, project)
}

func buildWorkflowTriggerCoordinator(db *store.Store, workflows *service.WorkflowService, cfg *config.Config, bus ...*events.Bus) *service.WorkflowTriggerCoordinator {
	coordinator := service.NewWorkflowTriggerCoordinator(db, workflows, workflowDiscoveryProvider(cfg, db))
	if len(bus) > 0 {
		coordinator.SetEventBus(bus[0])
	}
	return coordinator
}
