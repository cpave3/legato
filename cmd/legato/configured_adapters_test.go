package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/service"
)

func TestConfiguredAdaptersUsesWorkerDefaultAndConfiguredLaunchSettings(t *testing.T) {
	cfg := &config.Config{
		Swarm: config.SwarmConfig{Worker: config.SwarmParticipantConfig{Agent: "chimera"}},
		Adapters: map[string]config.AdapterConfig{
			"chimera": {
				LaunchArgs: []string{"--sandbox"},
				Tiers: map[string]config.TierConfig{
					"large": {LaunchArgs: []string{"--model", "tier-model"}},
				},
			},
		},
	}
	defaultAdapter, adapters := configuredAdapters(cfg, "legato")
	if defaultAdapter.Name() != "chimera" || len(adapters) != 3 {
		t.Fatalf("default = %q, adapters = %d", defaultAdapter.Name(), len(adapters))
	}
	launcher := adapters["chimera"].(service.LaunchCommandAdapter)
	cmd := launcher.LaunchCommand(map[string]string{}, "", "direct-model", "large")
	for _, want := range []string{"--sandbox", "--model direct-model", "--model tier-model"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("command %q missing %q", cmd, want)
		}
	}
}

func TestBuildValidateOptionsUsesRegisteredDefaultAdapter(t *testing.T) {
	cfg := &config.Config{Adapters: map[string]config.AdapterConfig{
		"claude-code": {Tiers: map[string]config.TierConfig{"small": {}}},
	}}
	opts := buildValidateOptions(cfg)
	if opts.DefaultAgent != "claude-code" {
		t.Fatalf("default agent = %q, want claude-code", opts.DefaultAgent)
	}
}

func TestConfiguredAdaptersGiveClaudeSessionsTheNativeBridge(t *testing.T) {
	t.Setenv("CLAUDE_CODE_PLUGIN_DIRS", "")
	dataDir := t.TempDir()
	cfg := &config.Config{DataDir: dataDir}

	_, adapters := configuredAdapters(cfg, "/opt/legato")

	vars := adapters["claude-code"].EnvVars("task-1", "")
	pluginDir := filepath.Join(dataDir, "claude-mod", "legato")
	if want := pluginDirs(dataDir, pluginDir); vars["CLAUDE_CODE_PLUGIN_DIRS"] != want {
		t.Fatalf("CLAUDE_CODE_PLUGIN_DIRS = %q, want %q", vars["CLAUDE_CODE_PLUGIN_DIRS"], want)
	}
	if _, err := os.Stat(filepath.Join(pluginDir, ".claude-plugin", "plugin.json")); err != nil {
		t.Fatalf("the mod the session is pointed at is not installed: %v", err)
	}
}

// pluginDirs is CLAUDE_CODE_PLUGIN_DIRS for a session given dirs: the state
// plugin comes last, for every session.
func pluginDirs(dataDir string, dirs ...string) string {
	return strings.Join(append(dirs, filepath.Join(dataDir, "claude-mod", "legato-state")), string(os.PathListSeparator))
}

func TestConfiguredAdaptersGiveEveryClaudeSessionTheStatePlugin(t *testing.T) {
	t.Setenv("CLAUDE_CODE_PLUGIN_DIRS", "")
	dataDir := t.TempDir()
	disabled := false
	// Neither the bridge nor the tools: the state plugin does not need them.
	cfg := &config.Config{DataDir: dataDir, Adapters: map[string]config.AdapterConfig{"claude-code": {NativeBridge: &disabled}}}

	_, adapters := configuredAdapters(cfg, "/opt/legato")

	vars := adapters["claude-code"].EnvVars("task-1", "")
	if vars["CLAUDE_CODE_PLUGIN_DIRS"] != pluginDirs(dataDir) || vars["LEGATO_BIN"] != "/opt/legato" {
		t.Fatalf("claude env = %v, want the state plugin", vars)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "claude-mod", "legato-state", "hooks", "hooks.json")); err != nil {
		t.Fatalf("the state plugin was not installed: %v", err)
	}
}

func TestConfiguredAdaptersHonourTheNativeBridgeOptOut(t *testing.T) {
	t.Setenv("CLAUDE_CODE_PLUGIN_DIRS", "")
	disabled := false
	dataDir := t.TempDir()
	cfg := &config.Config{DataDir: dataDir, Adapters: map[string]config.AdapterConfig{
		"claude-code": {NativeBridge: &disabled},
	}}

	_, adapters := configuredAdapters(cfg, "/opt/legato")

	if dirs := adapters["claude-code"].EnvVars("task-1", "")["CLAUDE_CODE_PLUGIN_DIRS"]; dirs != pluginDirs(dataDir) {
		t.Fatalf("CLAUDE_CODE_PLUGIN_DIRS = %q, want only the state plugin when the bridge is disabled", dirs)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "claude-mod", "legato")); !os.IsNotExist(err) {
		t.Fatalf("the mod was installed although the bridge is disabled: %v", err)
	}
}

func TestConfiguredAdaptersLaunchCodexWithItsNativeBridge(t *testing.T) {
	cfg := &config.Config{DataDir: t.TempDir()}

	_, adapters := configuredAdapters(cfg, "/opt/legato")

	launcher, ok := adapters["codex"].(service.LaunchCommandAdapter)
	if !ok {
		t.Fatal("the codex adapter cannot launch")
	}
	if got := launcher.LaunchCommand(nil, "", "", ""); got != "/opt/legato agent codex --" {
		t.Fatalf("launch command = %q", got)
	}
	if binder, ok := adapters["codex"].(service.NativeBindingAdapter); !ok || !binder.BindsNativeEndpoint() {
		t.Fatal("launch messages for Codex should wait for its bridge")
	}
}

func TestConfiguredAdaptersHonourTheCodexNativeBridgeOptOut(t *testing.T) {
	disabled := false
	cfg := &config.Config{DataDir: t.TempDir(), Adapters: map[string]config.AdapterConfig{
		"codex": {NativeBridge: &disabled, LaunchArgs: []string{"--search"}},
	}}

	_, adapters := configuredAdapters(cfg, "/opt/legato")

	if got := adapters["codex"].(service.LaunchCommandAdapter).LaunchCommand(nil, "", "", ""); got != "codex --search" {
		t.Fatalf("launch command = %q, want Codex started directly", got)
	}
}

func TestConfiguredAdaptersReportClaudeUsageToTheConfiguredManticore(t *testing.T) {
	t.Setenv("CLAUDE_CODE_PLUGIN_DIRS", "")
	dataDir := t.TempDir()
	// Reporting does not depend on the bridge: this session stays tmux-driven.
	disabled := false
	cfg := &config.Config{
		DataDir:   dataDir,
		Adapters:  map[string]config.AdapterConfig{"claude-code": {NativeBridge: &disabled}},
		Manticore: config.ManticoreConfig{URL: "http://manticore.local:3456", APIKey: "mc_secret"},
	}

	_, adapters := configuredAdapters(cfg, "/opt/legato")

	vars := adapters["claude-code"].EnvVars("task-1", "")
	pluginDir := filepath.Join(dataDir, "claude-mod", "legato-manticore")
	if want := pluginDirs(dataDir, pluginDir); vars["CLAUDE_CODE_PLUGIN_DIRS"] != want {
		t.Fatalf("CLAUDE_CODE_PLUGIN_DIRS = %q, want %q", vars["CLAUDE_CODE_PLUGIN_DIRS"], want)
	}
	if vars["LEGATO_MANTICORE_URL"] != "http://manticore.local:3456" || vars["LEGATO_MANTICORE_API_KEY"] != "mc_secret" {
		t.Fatalf("Manticore variables = %q, %q", vars["LEGATO_MANTICORE_URL"], vars["LEGATO_MANTICORE_API_KEY"])
	}
	if _, err := os.Stat(filepath.Join(pluginDir, ".claude-plugin", "plugin.json")); err != nil {
		t.Fatalf("the usage mod the session is pointed at is not installed: %v", err)
	}
	// Only Claude Code sessions report: the others are proxied or have no mod.
	for _, name := range []string{"chimera", "codex"} {
		if _, ok := adapters[name].EnvVars("task-1", "")["LEGATO_MANTICORE_API_KEY"]; ok {
			t.Fatalf("%s sessions were handed the Manticore key", name)
		}
	}
}

func TestConfiguredAdaptersInstallNoUsageModWithoutAManticore(t *testing.T) {
	t.Setenv("CLAUDE_CODE_PLUGIN_DIRS", "")
	dataDir := t.TempDir()
	cfg := &config.Config{DataDir: dataDir}

	_, adapters := configuredAdapters(cfg, "/opt/legato")

	if _, ok := adapters["claude-code"].EnvVars("task-1", "")["LEGATO_MANTICORE_URL"]; ok {
		t.Fatal("LEGATO_MANTICORE_URL set although no Manticore instance is configured")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "claude-mod", "legato-manticore")); !os.IsNotExist(err) {
		t.Fatalf("the usage mod was installed although no Manticore instance is configured: %v", err)
	}
}

func TestConfiguredAdaptersGiveEverySessionLegatosMCPServer(t *testing.T) {
	t.Setenv("CLAUDE_CODE_PLUGIN_DIRS", "")
	dataDir := t.TempDir()
	disabled := false
	cfg := &config.Config{
		DataDir: dataDir,
		Agents:  config.AgentsConfig{MCP: config.AgentMCPConfig{Enabled: true}},
		// The tools must not depend on the native bridge.
		Adapters: map[string]config.AdapterConfig{"claude-code": {NativeBridge: &disabled}, "codex": {NativeBridge: &disabled}},
	}
	oldProbe := chimeraMCPConfigProbe
	chimeraMCPConfigProbe = func() bool { return true }
	t.Cleanup(func() { chimeraMCPConfigProbe = oldProbe })

	_, adapters := configuredAdapters(cfg, "/opt/legato")

	claude := adapters["claude-code"].EnvVars("task-1", "")
	toolsDir := filepath.Join(dataDir, "claude-mod", "legato-tools")
	if claude["CLAUDE_CODE_PLUGIN_DIRS"] != pluginDirs(dataDir, toolsDir) || claude["LEGATO_BIN"] != "/opt/legato" {
		t.Fatalf("claude env = %v, want the tools plugin at %s", claude, toolsDir)
	}
	if _, err := os.Stat(filepath.Join(toolsDir, ".mcp.json")); err != nil {
		t.Fatalf("the tools plugin was not installed: %v", err)
	}

	codex := adapters["codex"].(service.LaunchCommandAdapter).LaunchCommand(map[string]string{}, "", "", "")
	if !strings.Contains(codex, `mcp_servers.legato.command="/opt/legato"`) {
		t.Fatalf("codex command %q defines no legato server", codex)
	}

	chimera := adapters["chimera"].(service.LaunchCommandAdapter).LaunchCommand(map[string]string{}, "", "", "")
	if !strings.Contains(chimera, "--mcp-config") || !strings.Contains(chimera, `"command":"/opt/legato"`) {
		t.Fatalf("chimera command %q adds no legato server", chimera)
	}
	// Codex and Chimera do not pass the server's own instructions to their
	// model, so their launch prompt carries the same guidance. Claude Code
	// reads the instructions and needs no second copy.
	for _, name := range []string{"codex", "chimera"} {
		prompt := adapters[name].(service.ToolsPromptAdapter).ToolsPrompt()
		if !strings.HasPrefix(prompt, "## Legato tools\n") || !strings.Contains(prompt, "without being asked") || !strings.Contains(prompt, "show_artifact") {
			t.Fatalf("%s tools guidance = %q", name, prompt)
		}
	}
	if _, repeats := adapters["claude-code"].(service.ToolsPromptAdapter); repeats {
		t.Fatal("claude-code repeats guidance its sessions get from the server")
	}
	// Every tool is trusted, so Chimera asks before none of them.
	for _, tool := range []string{"show_artifact", "show_card", "show_components", "task_context", "add_task_note", "list_peers"} {
		if !strings.Contains(chimera, `"`+tool+`"`) {
			t.Fatalf("chimera command %q does not trust %s", chimera, tool)
		}
	}
}

func TestConfiguredAdaptersAddNoMCPServerWhenTheSettingIsOff(t *testing.T) {
	t.Setenv("CLAUDE_CODE_PLUGIN_DIRS", "")
	dataDir := t.TempDir()
	disabled := false
	cfg := &config.Config{DataDir: dataDir, Adapters: map[string]config.AdapterConfig{"claude-code": {NativeBridge: &disabled}}}
	oldProbe := chimeraMCPConfigProbe
	chimeraMCPConfigProbe = func() bool { return true }
	t.Cleanup(func() { chimeraMCPConfigProbe = oldProbe })

	_, adapters := configuredAdapters(cfg, "/opt/legato")

	if dirs := adapters["claude-code"].EnvVars("task-1", "")["CLAUDE_CODE_PLUGIN_DIRS"]; dirs != pluginDirs(dataDir) {
		t.Fatalf("CLAUDE_CODE_PLUGIN_DIRS = %q, want only the state plugin", dirs)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "claude-mod", "legato-tools")); !os.IsNotExist(err) {
		t.Fatalf("the tools plugin was installed with the setting off: %v", err)
	}
	for _, name := range []string{"codex", "chimera"} {
		command := adapters[name].(service.LaunchCommandAdapter).LaunchCommand(map[string]string{}, "", "", "")
		if strings.Contains(command, "mcp") {
			t.Fatalf("%s command %q mentions an MCP server", name, command)
		}
	}
	for _, name := range []string{"codex", "chimera"} {
		if prompt := adapters[name].(service.ToolsPromptAdapter).ToolsPrompt(); prompt != "" {
			t.Fatalf("%s is told about tools it does not have: %q", name, prompt)
		}
	}
}

func TestTheLaunchPromptNamesTheCredentialsAndHowToUseThem(t *testing.T) {
	t.Setenv("CLAUDE_CODE_PLUGIN_DIRS", "")
	cfg := &config.Config{
		DataDir: t.TempDir(),
		Agents:  config.AgentsConfig{MCP: config.AgentMCPConfig{Enabled: true}},
		Credentials: []config.CredentialConfig{
			{Name: "legato_default", Secret: "op://Legato/legato_default/private key", Profile: "ssh"},
			{Name: "eleven", Secret: "legato://eleven-key", Profile: "elevenlabs"},
		},
	}
	oldProbe := chimeraMCPConfigProbe
	chimeraMCPConfigProbe = func() bool { return true }
	t.Cleanup(func() { chimeraMCPConfigProbe = oldProbe })

	_, adapters := configuredAdapters(cfg, "/opt/legato")
	for _, name := range []string{"codex", "chimera"} {
		prompt := adapters[name].(service.ToolsPromptAdapter).ToolsPrompt()
		for _, want := range []string{"SSH keys (legato_default)", "eleven (an API key", "$ELEVENLABS_BASE_URL"} {
			if !strings.Contains(prompt, want) {
				t.Errorf("%s launch prompt does not say %q", name, want)
			}
		}
	}
}

func TestTheLaunchPromptNamesAVaultOfSSHKeys(t *testing.T) {
	t.Setenv("CLAUDE_CODE_PLUGIN_DIRS", "")
	cfg := &config.Config{
		DataDir:           t.TempDir(),
		Agents:            config.AgentsConfig{MCP: config.AgentMCPConfig{Enabled: true}},
		CredentialSources: []config.CredentialSourceConfig{{Vault: "op://Legato", Profile: "ssh"}},
	}
	oldProbe := chimeraMCPConfigProbe
	chimeraMCPConfigProbe = func() bool { return true }
	t.Cleanup(func() { chimeraMCPConfigProbe = oldProbe })
	_, adapters := configuredAdapters(cfg, "/opt/legato")
	if prompt := adapters["codex"].(service.ToolsPromptAdapter).ToolsPrompt(); !strings.Contains(prompt, "every SSH key in the 1Password vault Legato") {
		t.Fatalf("codex launch prompt names no vault")
	}
}

func TestConfiguredAdaptersTrustTheFolderAClaudeSessionStartsIn(t *testing.T) {
	cfg := &config.Config{DataDir: t.TempDir()}

	_, adapters := configuredAdapters(cfg, "/opt/legato")

	if _, ok := adapters["claude-code"].(service.WorkspaceTrustAdapter); !ok {
		t.Fatal("Claude Code sessions would stop at the trust dialog")
	}
}

func TestConfiguredAdaptersInstallChimeraModUnlessOptedOut(t *testing.T) {
	for _, optOut := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "opt out"}[optOut], func(t *testing.T) {
			t.Setenv("CHIMERA_MOD_DIRS", "")
			cfg := &config.Config{DataDir: t.TempDir()}
			if optOut {
				enabled := false
				cfg.Adapters = map[string]config.AdapterConfig{"chimera": {NativeBridge: &enabled}}
			}
			_, adapters := configuredAdapters(cfg, "/opt/legato")
			a := adapters["chimera"]
			binder, ok := a.(service.NativeBindingAdapter)
			if !ok || binder.BindsNativeEndpoint() == optOut {
				t.Fatalf("native binding does not match opt out %v", optOut)
			}
			dir := filepath.Join(cfg.DataDir, "chimera-mod", "legato")
			if optOut {
				if _, err := os.Stat(dir); !os.IsNotExist(err) {
					t.Fatalf("opted-out mod installed: %v", err)
				}
				return
			}
			vars := a.EnvVars("task-1", "")
			if vars["CHIMERA_MOD_DIRS"] != dir || vars["LEGATO_BIN"] != "/opt/legato" {
				t.Fatalf("environment = %v", vars)
			}
			if _, err := os.Stat(dir); err != nil {
				t.Fatalf("session mod missing: %v", err)
			}
		})
	}
}
