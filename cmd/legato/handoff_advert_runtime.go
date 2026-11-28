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
func handoffAdvertSources(cfg *config.Config, db *store.Store, credentials *service.CredentialBroker) service.HandoffAdvertSources {
	skills := skillService(cfg, db)
	sources := service.HandoffAdvertSources{
		Installed: func(kind string) bool {
			command, ok := agentCommands[kind]
			if !ok {
				command = kind
			}
			if kind == "claude-code" {
				command, _ = claudeLaunch(cfg)()
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
	}
	// What the repository cache can clone is what the credential broker's
	// GitHub credentials list, from config.yaml or a vault. Without a
	// broker the worker cannot say, and the advert leaves it null.
	if credentials != nil {
		sources.Repositories = credentials.GitHubRepositories
	}
	return sources
}
