package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/logfile"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/version"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// newReeveService is the Reeve of this instance, its sessions started with
// the agent service and the reeve.* settings, read when each starts.
func newReeveService(db *store.Store, agents service.AgentService, srv *server.Server, approvals *service.ApprovalService) *service.ReeveService {
	if agents == nil {
		return nil
	}
	opts := service.ReeveServiceOptions{
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
	}
	if approvals != nil {
		opts.Approvals = approvals
	}
	return service.NewReeveService(opts)
}

func reeveDefaults(cfg *config.Config) service.ReeveDefaults {
	return service.ReeveDefaults{Agent: cfg.Reeve.Agent, Model: cfg.Reeve.Model, LaunchArgs: cfg.Reeve.LaunchArgs}
}

// wireReeve serves the Reeve on a web server, and suspends its idle
// sessions.
func wireReeve(srv *server.Server, db *store.Store, agents service.AgentService, bus *events.Bus, approvals *service.ApprovalService) *service.ReeveService {
	reeve := newReeveService(db, agents, srv, approvals)
	if reeve != nil {
		srv.SetReeveService(reeve)
		go runReeveIdle(context.Background(), reeve, db, bus)
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
		src.Reeve = &cfg.Reeve
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
	if processChangeSets != nil {
		tools.ChangeSets = processChangeSets
	}
	if processSecrets != nil {
		tools.Secrets = processSecrets
	}
	tools.Legato = legatoCommands{}
	tools.Peers = newReevePeers(cfg, db, nil, nil, nil)
	tools.HostOperations = func(ctx context.Context) bool {
		effective, err := settingsFor(db).Effective(ctx)
		return err == nil && effective.Reeve.HostOperationsOn()
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

// runReeveIdle suspends the Reeve's session once it has been idle for
// reeve.idle_timeout, checking every minute, and counts an agent's activity
// as the session's.
func runReeveIdle(ctx context.Context, reeve *service.ReeveService, db *store.Store, bus *events.Bus) {
	if reeve == nil {
		return
	}
	activity := bus.Subscribe(events.EventAgentActivityChanged)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-activity:
			if p, ok := ev.Payload.(events.AgentActivityChangedPayload); ok && service.IsReeveTask(ctx, db, p.TaskID) {
				reeve.Touch(ctx, p.TaskID)
			}
		case now := <-ticker.C:
			cfg, err := settingsFor(db).Effective(ctx)
			if err != nil {
				continue
			}
			if _, err := reeve.SuspendIdle(ctx, now.UTC(), cfg.Reeve.ReeveIdleTimeout()); err != nil {
				log.Printf("reeve: suspending an idle session: %v", err)
			}
		}
	}
}

// reeveRules puts changes to the Reeve's own rules to signed approvals.
func reeveRules(db *store.Store, approvals *service.ApprovalService) *service.ReeveRules {
	rules := &service.ReeveRules{Settings: settingsFor(db), Approvals: approvals, Store: db}
	rules.Register()
	return rules
}
