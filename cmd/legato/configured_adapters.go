package main

import (
	"log"
	"path/filepath"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/service"
)

// chimeraMCPConfigProbe, when set, replaces how the Chimera adapter learns
// whether the installed Chimera accepts --mcp-config. Tests set it so they
// do not depend on a Chimera being installed.
var chimeraMCPConfigProbe func() bool

func configuredAdapters(cfg *config.Config, legatoBin string) (service.AIToolAdapter, map[string]service.AIToolAdapter) {
	claude := hooks.NewClaudeCodeAdapter(legatoBin)
	chimera := hooks.NewChimeraAdapter(legatoBin)
	codex := hooks.NewCodexAdapter(legatoBin)
	claude.SetReviews(cfg.Agents.Reviews)
	chimera.SetReviews(cfg.Agents.Reviews)
	codex.SetReviews(cfg.Agents.Reviews)

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
	// Every Claude Code session reports its state through the state plugin,
	// written afresh at each start so its hooks match this binary. A failed
	// install leaves the state to hook scripts installed by hand, if any.
	if pluginDir, err := hooks.InstallClaudeStateMod(filepath.Join(config.ResolveDataDir(cfg), "claude-mod")); err != nil {
		log.Printf("claude-code state plugin unavailable: %v", err)
	} else {
		claude.SetStateModDir(pluginDir)
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
	if nativeBridgeEnabled(cfg, chimera.Name()) {
		root := filepath.Join(config.ResolveDataDir(cfg), "chimera-mod")
		var modDir string
		var err error
		if cfg.Agents.MCP.Enabled {
			modDir, err = hooks.InstallChimeraModWithTools(root, legatoBin, cli.MCPToolNames())
		} else {
			modDir, err = hooks.InstallChimeraMod(root)
		}
		if err != nil {
			log.Printf("chimera native mod unavailable: %v", err)
		} else {
			chimera.SetModDir(modDir)
		}
	}
	if configured, ok := cfg.Adapters[codex.Name()]; ok {
		codex.SetLaunchArgs(configured.LaunchArgs)
		codex.SetTiers(adapterTierLaunchArgs(configured))
	}
	codex.SetNativeBridge(nativeBridgeEnabled(cfg, codex.Name()))
	if cfg.Agents.MCP.Enabled {
		// Every session gets Legato's own tools through `legato mcp serve`,
		// added for that session only: nothing is written to the agent
		// tool's own configuration. Each tool is run without asking.
		//
		// A failed install leaves Claude Code sessions without the tools;
		// nothing else about them changes.
		if pluginDir, err := hooks.InstallClaudeToolsMod(filepath.Join(config.ResolveDataDir(cfg), "claude-mod")); err != nil {
			log.Printf("claude-code Legato tools unavailable: %v", err)
		} else {
			claude.SetToolsModDir(pluginDir)
		}
		codex.SetMCPTools(true)
		chimera.SetMCPTools(true, cli.MCPToolNames())
		// Mod-enabled Chimera receives the server's instructions itself.
		// Keep credential/branch guidance separate so it still reaches the
		// launch prompt without repeating the MCP tools instructions.
		toolsPrompt := "## Legato tools\n\n" + cli.MCPGuidance + credentialPromptGuidance(cfg, hostStackSupport(), service.SessionBranches{})
		codex.SetToolsPrompt(toolsPrompt)
		chimera.SetToolsPrompt("## Legato tools\n\n" + cli.MCPGuidance)
		chimera.SetCredentialPrompt(credentialPromptGuidance(cfg, hostStackSupport(), service.SessionBranches{}), func(base, given string, granted bool) string {
			return credentialPromptGuidance(cfg, hostStackSupport(), service.SessionBranches{Base: base, Given: given, Granted: granted})
		})
		// A handed-off task's session names its branches after the main's
		// task (see docs/ai/credentials.md § Handed-off sessions).
		toolsPromptFor := func(base, given string, granted bool) string {
			return "## Legato tools\n\n" + cli.MCPGuidance + credentialPromptGuidance(cfg, hostStackSupport(), service.SessionBranches{Base: base, Given: given, Granted: granted})
		}
		codex.SetToolsPromptFor(toolsPromptFor)
		if chimeraMCPConfigProbe != nil {
			chimera.SetMCPConfigProbe(chimeraMCPConfigProbe)
		}
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

// credentialPromptGuidance names the configured credentials and vaults and
// how agents use them, for the launch prompt. Credentials the broker refuses
// are left out; a host Legato logs why.
//
// With stacks (the host's st can move a stack between clones), it also says
// how to build and push a stack with Staccato. branches is what a
// handed-off task's session is told of its branches; zero for any other.
func credentialPromptGuidance(cfg *config.Config, stacks bool, branches service.SessionBranches) string {
	if len(cfg.Credentials) == 0 && len(cfg.CredentialSources) == 0 {
		return cli.GuidanceFor(nil, nil, stacks, branches)
	}
	summaries, sources, err := service.DescribeCredentials(cfg.Credentials, cfg.CredentialSources, credentialProfileDirs())
	if err != nil {
		return cli.GuidanceFor(nil, nil, stacks, branches)
	}
	return cli.GuidanceFor(summaries, sources, stacks, branches)
}
