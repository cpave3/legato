package main

import (
	"log"
	"path/filepath"

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
	if claudeNativeBridgeEnabled(cfg) {
		// A failed install leaves Claude Code sessions tmux-only, as they
		// were before the mod existed.
		if pluginDir, err := hooks.InstallClaudeMod(filepath.Join(config.ResolveDataDir(cfg), "claude-mod")); err != nil {
			log.Printf("claude-code native bridge unavailable: %v", err)
		} else {
			claude.SetModDir(pluginDir)
		}
	}
	if cfg.Manticore.URL != "" {
		// A failed install leaves Claude Code sessions unreported; nothing
		// else about them changes.
		if pluginDir, err := hooks.InstallClaudeUsageMod(filepath.Join(config.ResolveDataDir(cfg), "claude-mod")); err != nil {
			log.Printf("claude-code usage reporting unavailable: %v", err)
		} else {
			claude.SetUsageReporting(pluginDir, cfg.Manticore.URL, cfg.Manticore.APIKey)
		}
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
	codex.SetNativeBridge(nativeBridgeEnabled(cfg, codex.Name()))

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

// claudeNativeBridgeEnabled reports whether Claude Code sessions load the
// Legato mod. It is on unless adapters.claude-code.native_bridge says false.
func claudeNativeBridgeEnabled(cfg *config.Config) bool {
	return nativeBridgeEnabled(cfg, "claude-code")
}

// nativeBridgeEnabled reports whether an adapter's sessions are connected to
// Legato beyond their terminal. It is on unless adapters.<name>.native_bridge
// says false.
func nativeBridgeEnabled(cfg *config.Config, adapter string) bool {
	configured, ok := cfg.Adapters[adapter]
	return !ok || configured.NativeBridge == nil || *configured.NativeBridge
}
