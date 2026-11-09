package main

import (
	"context"
	"os"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// newReeveService is the Reeve of this instance, its sessions started with
// the agent service and the reeve.* settings, read when each starts.
func newReeveService(db *store.Store, agents service.AgentService, srv *server.Server) *service.ReeveService {
	if agents == nil {
		return nil
	}
	return service.NewReeveService(service.ReeveServiceOptions{
		Store: db, Agents: agents, Folder: service.ReeveFolder(os.Getenv),
		Defaults: func() service.ReeveDefaults {
			cfg, err := settingsFor(db).Effective(context.Background())
			if err != nil || cfg == nil {
				return service.ReeveDefaults{}
			}
			return reeveDefaults(cfg)
		},
		Changed: func(string) {
			if srv != nil {
				srv.NotifyAgentsChanged()
			}
		},
	})
}

func reeveDefaults(cfg *config.Config) service.ReeveDefaults {
	return service.ReeveDefaults{Agent: cfg.Reeve.Agent, Model: cfg.Reeve.Model, LaunchArgs: cfg.Reeve.LaunchArgs}
}

// wireReeve serves the Reeve on a web server.
func wireReeve(srv *server.Server, db *store.Store, agents service.AgentService) *service.ReeveService {
	reeve := newReeveService(db, agents, srv)
	if reeve != nil {
		srv.SetReeveService(reeve)
	}
	return reeve
}
