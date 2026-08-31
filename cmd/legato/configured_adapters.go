package main

import (
	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/service"
)

func configuredAdapters(cfg *config.Config, legatoBin string) (service.AIToolAdapter, map[string]service.AIToolAdapter) {
	claude := hooks.NewClaudeCodeAdapter(legatoBin)
	chimera := hooks.NewChimeraAdapter(legatoBin)
	codex := hooks.NewCodexAdapter(legatoBin)

	if overrides := buildAdapterRoleOverrides(cfg, claude.Name()); overrides != nil {
		claude.SetRoleOverrides(overrides)
	}
	if overrides := buildAdapterRoleOverrides(cfg, chimera.Name()); overrides != nil {
		chimera.SetRoleOverrides(overrides)
	}
	if overrides := buildAdapterRoleOverrides(cfg, codex.Name()); overrides != nil {
		codex.SetRoleOverrides(overrides)
	}
	if configured, ok := cfg.Adapters[claude.Name()]; ok {
		claude.SetLaunchArgs(configured.LaunchArgs)
		claude.SetTiers(adapterTierLaunchArgs(configured))
	}
	if configured, ok := cfg.Adapters[chimera.Name()]; ok {
		chimera.SetLaunchArgs(configured.LaunchArgs)
		if configured.Modes != nil {
			chimera.SetModes(configured.Modes)
		}
		chimera.SetTiers(adapterTierLaunchArgs(configured))
	}
	if configured, ok := cfg.Adapters[codex.Name()]; ok {
		codex.SetLaunchArgs(configured.LaunchArgs)
		codex.SetTiers(adapterTierLaunchArgs(configured))
	}

	adapters := map[string]service.AIToolAdapter{
		claude.Name():  claude,
		chimera.Name(): chimera,
		codex.Name():   codex,
	}
	defaultAdapter := service.AIToolAdapter(claude)
	if configured := cfg.Swarm.Worker.Agent; configured != "" {
		defaultAdapter = adapters[configured]
	}
	return defaultAdapter, adapters
}
