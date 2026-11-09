package main

import (
	"context"
	"os"
	"strconv"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/logfile"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/version"
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
		Snapshot: func(ctx context.Context) string { return reeveSnapshot(ctx, db) },
		Engram:   service.NoEngram{},
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

// reeveSnapshot is this instance's snapshot, for the Reeve's launch prompt
// and its instance_status tool.
func reeveSnapshot(ctx context.Context, db *store.Store) string {
	cfg, _ := settingsFor(db).Effective(ctx)
	src := service.ReeveSnapshotSource{Store: db, Version: version.String(), Runs: howThisRuns()}
	if cfg != nil {
		src.LogPath = logfile.Path(config.ResolveDataDir(cfg))
		src.UpdateMode = cfg.Updates.Mode
	}
	return service.ReeveSnapshot(ctx, src)
}

// howThisRuns says how this Legato process runs.
func howThisRuns() string {
	how := "the TUI"
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		how = "legato serve"
	}
	switch {
	case os.Getenv(hotReloadChildEnv) != "":
		how += ", under the hot-reload supervisor"
	case os.Getenv("SYSTEMD_EXEC_PID") == strconv.Itoa(os.Getpid()):
		how += ", as a systemd service"
	}
	return how
}

// reeveTools is what the Reeve's own MCP tools need, in the host process.
func reeveTools(cfg *config.Config, db *store.Store) *cli.MCPReeve {
	tools := &cli.MCPReeve{
		Snapshot: func(ctx context.Context) string { return reeveSnapshot(ctx, db) },
		LogPath:  logfile.Path(config.ResolveDataDir(cfg)),
	}
	if cfg.Web.Enabled || (len(os.Args) > 1 && os.Args[1] == "serve") {
		scheme := "http"
		if tlsEnabled(cfg) {
			scheme = "https"
		}
		tools.APIBase = scheme + "://127.0.0.1:" + cfg.Web.Port
	}
	return tools
}
