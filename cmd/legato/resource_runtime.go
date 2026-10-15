package main

import (
	"os"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// configuredResourceService builds the resource service from the resources of
// the configuration and the ones saved in the database.
func configuredResourceService(cfg *config.Config, db *store.Store) *service.ResourceService {
	home, _ := os.UserHomeDir()
	var configured []service.Resource
	for _, resource := range cfg.Resources {
		item := service.Resource{Name: resource.Name}
		for _, path := range resource.Paths {
			item.Paths = append(item.Paths, service.ResourcePath{Path: path.Path, Access: path.Access})
		}
		configured = append(configured, item)
	}
	var saved service.ResourceStore
	if db != nil {
		saved = db
	}
	return service.NewResourceService(saved, configured, home)
}

// wiredResourceService is the resource service of a runtime with agents: it
// can also give resources to the agents that are running.
func wiredResourceService(cfg *config.Config, db *store.Store, agents service.AgentService) *service.ResourceService {
	resources := configuredResourceService(cfg, db)
	if attacher, ok := agents.(service.ResourceAttacher); ok {
		resources.SetAttacher(attacher)
	}
	return resources
}
