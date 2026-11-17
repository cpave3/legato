package main

import (
	"context"
	"os/exec"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// agentCommands are the commands each agent tool runs, which the advert
// looks for on PATH.
var agentCommands = map[string]string{"claude-code": "claude", "chimera": "chimera", "codex": "codex"}

// handoffAdvertSources are where a worker's advert to its mains comes from:
// the agent tools on PATH and their tiers, the Legato tools setting, the MCP
// connections that are on, the skills that are on, and the repositories the
// GitHub credentials list. See docs/ai/integrations.md § What a worker
// advertises.
func handoffAdvertSources(cfg *config.Config, db *store.Store) service.HandoffAdvertSources {
	skills := skillService(cfg, db)
	return service.HandoffAdvertSources{
		Installed: func(kind string) bool {
			command, ok := agentCommands[kind]
			if !ok {
				command = kind
			}
			_, err := exec.LookPath(command)
			return err == nil
		},
		Tiers: func(kind string) []string {
			var tiers []string
			for tier := range cfg.Adapters[kind].Tiers {
				tiers = append(tiers, tier)
			}
			return tiers
		},
		MCPTools: func() bool { return cfg.Agents.MCP.Enabled },
		MCPConnections: func(ctx context.Context) ([]string, error) {
			conns, err := db.ListMCPConnections(ctx)
			if err != nil {
				return nil, err
			}
			var names []string
			for _, c := range conns {
				if c.Enabled {
					names = append(names, c.Name)
				}
			}
			return names, nil
		},
		Skills: skills.Usable,
		Repositories: func() []string {
			var repos []string
			for _, c := range cfg.Credentials {
				if c.Profile == "github" {
					repos = append(repos, c.Settings.Repositories...)
				}
			}
			return repos
		},
	}
}
