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
	if vars["CLAUDE_CODE_PLUGIN_DIRS"] != pluginDir {
		t.Fatalf("CLAUDE_CODE_PLUGIN_DIRS = %q, want %q", vars["CLAUDE_CODE_PLUGIN_DIRS"], pluginDir)
	}
	if _, err := os.Stat(filepath.Join(pluginDir, ".claude-plugin", "plugin.json")); err != nil {
		t.Fatalf("the mod the session is pointed at is not installed: %v", err)
	}
}

func TestConfiguredAdaptersHonourTheNativeBridgeOptOut(t *testing.T) {
	disabled := false
	dataDir := t.TempDir()
	cfg := &config.Config{DataDir: dataDir, Adapters: map[string]config.AdapterConfig{
		"claude-code": {NativeBridge: &disabled},
	}}

	_, adapters := configuredAdapters(cfg, "/opt/legato")

	if dirs, ok := adapters["claude-code"].EnvVars("task-1", "")["CLAUDE_CODE_PLUGIN_DIRS"]; ok {
		t.Fatalf("CLAUDE_CODE_PLUGIN_DIRS = %q, want it unset when the bridge is disabled", dirs)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "claude-mod")); !os.IsNotExist(err) {
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
	if vars["CLAUDE_CODE_PLUGIN_DIRS"] != pluginDir {
		t.Fatalf("CLAUDE_CODE_PLUGIN_DIRS = %q, want %q", vars["CLAUDE_CODE_PLUGIN_DIRS"], pluginDir)
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
