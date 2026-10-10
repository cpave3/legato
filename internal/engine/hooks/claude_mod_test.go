package hooks_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/hooks"
)

func TestInstallClaudeModWritesALoadablePlugin(t *testing.T) {
	root := t.TempDir()

	pluginDir, err := hooks.InstallClaudeMod(root)
	if err != nil {
		t.Fatal(err)
	}

	if pluginDir != filepath.Join(root, "legato") {
		t.Fatalf("plugin dir = %q", pluginDir)
	}
	var manifest struct {
		Name string `json:"name"`
	}
	readJSON(t, filepath.Join(pluginDir, ".claude-plugin", "plugin.json"), &manifest)
	if manifest.Name != "legato" {
		t.Fatalf("manifest name = %q", manifest.Name)
	}
	var hookConfig struct {
		Modules []string `json:"modules"`
	}
	readJSON(t, filepath.Join(pluginDir, "hooks", "hooks.json"), &hookConfig)
	if len(hookConfig.Modules) != 1 {
		t.Fatalf("modules = %v", hookConfig.Modules)
	}
	module, err := os.ReadFile(filepath.Join(pluginDir, "hooks", hookConfig.Modules[0]))
	if err != nil {
		t.Fatalf("the hooks module the manifest names is missing: %v", err)
	}
	if !strings.Contains(string(module), "export const register") {
		t.Fatal("hooks module does not export register")
	}
	if _, err := os.Stat(filepath.Join(pluginDir, "tests")); !os.IsNotExist(err) {
		t.Fatalf("the mod's own tests were installed: %v", err)
	}
}

func TestInstallClaudeModLeavesAnUpToDateInstallUntouched(t *testing.T) {
	root := t.TempDir()
	pluginDir, err := hooks.InstallClaudeMod(root)
	if err != nil {
		t.Fatal(err)
	}
	// Claude Code watches the plugin folder and reloads the module on every
	// save, restarting the bridge of each running session.
	module := filepath.Join(pluginDir, "hooks", "register.ts")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(module, old, old); err != nil {
		t.Fatal(err)
	}

	if _, err := hooks.InstallClaudeMod(root); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(module)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(old) {
		t.Fatal("an unchanged module was rewritten")
	}
}

func TestInstallClaudeModReplacesAnOutdatedModule(t *testing.T) {
	root := t.TempDir()
	pluginDir, err := hooks.InstallClaudeMod(root)
	if err != nil {
		t.Fatal(err)
	}
	module := filepath.Join(pluginDir, "hooks", "register.ts")
	if err := os.WriteFile(module, []byte("// from an older Legato\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := hooks.InstallClaudeMod(root); err != nil {
		t.Fatal(err)
	}

	current, err := os.ReadFile(module)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(current), "export const register") {
		t.Fatal("the outdated module was not replaced")
	}
}

func TestClaudeCodeAdapterLoadsTheModInSessionsItLaunches(t *testing.T) {
	t.Setenv("CLAUDE_CODE_PLUGIN_DIRS", "")
	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	adapter.SetModDir("/data/claude-mod/legato")

	vars := adapter.EnvVars("task123", "")

	// The environment reaches every `claude` started in the session, whether
	// Legato typed the command or the person did.
	if vars["CLAUDE_CODE_PLUGIN_DIRS"] != "/data/claude-mod/legato" {
		t.Fatalf("CLAUDE_CODE_PLUGIN_DIRS = %q", vars["CLAUDE_CODE_PLUGIN_DIRS"])
	}
	if vars["LEGATO_BIN"] != "/usr/bin/legato" {
		t.Fatalf("LEGATO_BIN = %q", vars["LEGATO_BIN"])
	}
}

func TestClaudeCodeAdapterKeepsThePersonsOwnPluginDirs(t *testing.T) {
	t.Setenv("CLAUDE_CODE_PLUGIN_DIRS", "/home/me/mods/tally")
	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	adapter.SetModDir("/data/claude-mod/legato")

	got := adapter.EnvVars("task123", "")["CLAUDE_CODE_PLUGIN_DIRS"]

	want := "/home/me/mods/tally" + string(os.PathListSeparator) + "/data/claude-mod/legato"
	if got != want {
		t.Fatalf("CLAUDE_CODE_PLUGIN_DIRS = %q, want %q", got, want)
	}
}

func TestClaudeCodeAdapterWithoutTheModSetsNoPluginDirs(t *testing.T) {
	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")

	vars := adapter.EnvVars("task123", "")

	if _, ok := vars["CLAUDE_CODE_PLUGIN_DIRS"]; ok {
		t.Fatal("CLAUDE_CODE_PLUGIN_DIRS set without a mod directory")
	}
	if _, ok := vars["LEGATO_BIN"]; ok {
		t.Fatal("LEGATO_BIN set without a mod directory")
	}
}

func readJSON(t *testing.T, path string, out any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func TestClaudeCodeAdapterExpectsANativeEndpointOnlyWithTheMod(t *testing.T) {
	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	if adapter.BindsNativeEndpoint() {
		t.Fatal("a session without the mod never starts a bridge")
	}

	adapter.SetModDir("/data/claude-mod/legato")

	// Launch messages wait for the bridge only when one is coming.
	if !adapter.BindsNativeEndpoint() {
		t.Fatal("a session that loads the mod binds its bridge after launch")
	}
}

func TestInstallClaudeUsageModWritesALoadablePlugin(t *testing.T) {
	root := t.TempDir()

	pluginDir, err := hooks.InstallClaudeUsageMod(root)
	if err != nil {
		t.Fatal(err)
	}

	// It sits beside the bridge mod as a plugin of its own.
	if pluginDir != filepath.Join(root, "legato-manticore") {
		t.Fatalf("plugin dir = %q", pluginDir)
	}
	var manifest struct {
		Name string `json:"name"`
	}
	readJSON(t, filepath.Join(pluginDir, ".claude-plugin", "plugin.json"), &manifest)
	if manifest.Name != "legato-manticore" {
		t.Fatalf("manifest name = %q", manifest.Name)
	}
	var hookConfig struct {
		Modules []string `json:"modules"`
	}
	readJSON(t, filepath.Join(pluginDir, "hooks", "hooks.json"), &hookConfig)
	if len(hookConfig.Modules) != 1 {
		t.Fatalf("modules = %v", hookConfig.Modules)
	}
	module, err := os.ReadFile(filepath.Join(pluginDir, "hooks", hookConfig.Modules[0]))
	if err != nil {
		t.Fatalf("the hooks module the manifest names is missing: %v", err)
	}
	if !strings.Contains(string(module), "export const register") {
		t.Fatal("hooks module does not export register")
	}
	if _, err := os.Stat(filepath.Join(pluginDir, "tests")); !os.IsNotExist(err) {
		t.Fatalf("the mod's own tests were installed: %v", err)
	}
}

func TestClaudeCodeAdapterLoadsTheUsageModBesideTheBridgeMod(t *testing.T) {
	t.Setenv("CLAUDE_CODE_PLUGIN_DIRS", "/home/me/mods/tally")
	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	adapter.SetModDir("/data/claude-mod/legato")
	adapter.SetUsageReporting("/data/claude-mod/legato-manticore", "http://manticore.local:3456", "mc_secret")

	vars := adapter.EnvVars("task123", "")

	want := strings.Join([]string{
		"/home/me/mods/tally", "/data/claude-mod/legato", "/data/claude-mod/legato-manticore",
	}, string(os.PathListSeparator))
	if vars["CLAUDE_CODE_PLUGIN_DIRS"] != want {
		t.Fatalf("CLAUDE_CODE_PLUGIN_DIRS = %q, want %q", vars["CLAUDE_CODE_PLUGIN_DIRS"], want)
	}
	// The mod reads where to report, and as whom, from the session.
	if vars["LEGATO_MANTICORE_URL"] != "http://manticore.local:3456" || vars["LEGATO_MANTICORE_API_KEY"] != "mc_secret" {
		t.Fatalf("Manticore variables = %q, %q", vars["LEGATO_MANTICORE_URL"], vars["LEGATO_MANTICORE_API_KEY"])
	}
}

func TestClaudeCodeAdapterReportsUsageWithoutTheBridgeMod(t *testing.T) {
	t.Setenv("CLAUDE_CODE_PLUGIN_DIRS", "")
	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	adapter.SetUsageReporting("/data/claude-mod/legato-manticore", "http://manticore.local:3456", "mc_secret")

	vars := adapter.EnvVars("task123", "")

	if vars["CLAUDE_CODE_PLUGIN_DIRS"] != "/data/claude-mod/legato-manticore" {
		t.Fatalf("CLAUDE_CODE_PLUGIN_DIRS = %q", vars["CLAUDE_CODE_PLUGIN_DIRS"])
	}
	// Reporting usage starts no bridge: the session stays tmux-driven.
	if _, ok := vars["LEGATO_BIN"]; ok {
		t.Fatal("LEGATO_BIN set without the bridge mod")
	}
	if adapter.BindsNativeEndpoint() {
		t.Fatal("the usage mod alone binds no native endpoint")
	}
}

func TestClaudeCodeAdapterWithoutUsageReportingNamesNoManticore(t *testing.T) {
	t.Setenv("CLAUDE_CODE_PLUGIN_DIRS", "")
	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	adapter.SetModDir("/data/claude-mod/legato")

	vars := adapter.EnvVars("task123", "")

	for _, name := range []string{"LEGATO_MANTICORE_URL", "LEGATO_MANTICORE_API_KEY"} {
		if _, ok := vars[name]; ok {
			t.Fatalf("%s set although no Manticore instance is configured", name)
		}
	}
}
