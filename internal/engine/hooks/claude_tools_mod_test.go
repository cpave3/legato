package hooks_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
)

func TestInstallClaudeToolsModWritesAPluginThatStartsLegatosMCPServer(t *testing.T) {
	root := t.TempDir()

	pluginDir, err := hooks.InstallClaudeToolsMod(root)
	if err != nil {
		t.Fatal(err)
	}

	// It is a plugin of its own beside the bridge mod, so it does not depend
	// on the bridge and a change to it reloads no bridge.
	if pluginDir != filepath.Join(root, "legato-tools") {
		t.Fatalf("plugin dir = %q", pluginDir)
	}
	var manifest struct {
		Name string `json:"name"`
	}
	readJSON(t, filepath.Join(pluginDir, ".claude-plugin", "plugin.json"), &manifest)
	if manifest.Name != "legato-tools" {
		t.Fatalf("manifest name = %q", manifest.Name)
	}

	var servers struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	readJSON(t, filepath.Join(pluginDir, ".mcp.json"), &servers)
	server, ok := servers.MCPServers["legato"]
	if !ok || len(servers.MCPServers) != 1 {
		t.Fatalf("mcp servers = %#v, want only legato", servers.MCPServers)
	}
	// The binary is the one that launched the session, named by the
	// session's environment rather than by whatever PATH holds.
	if server.Command != "${LEGATO_BIN}" || strings.Join(server.Args, " ") != "mcp serve" {
		t.Fatalf("legato server = %#v", server)
	}
}

// The person chose not to be asked before an agent uses Legato's own tools.
// The hook that answers for them must cover those tools and no others.
func TestClaudeToolsModAllowsOnlyLegatosOwnToolsWithoutAsking(t *testing.T) {
	pluginDir, err := hooks.InstallClaudeToolsMod(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	readJSON(t, filepath.Join(pluginDir, "hooks", "hooks.json"), &config)
	entries := config.Hooks["PreToolUse"]
	if len(config.Hooks) != 1 || len(entries) != 2 || len(entries[0].Hooks) != 1 {
		t.Fatalf("hooks = %#v, want two PreToolUse hooks: Legato's tools, and reads of pasted images", config.Hooks)
	}
	matcher, err := regexp.Compile("^(?:" + entries[0].Matcher + ")$")
	if err != nil {
		t.Fatalf("matcher %q: %v", entries[0].Matcher, err)
	}
	for _, tool := range []string{"mcp__plugin_legato-tools_legato__show_card", "mcp__plugin_legato-tools_legato__add_task_note"} {
		if !matcher.MatchString(tool) {
			t.Errorf("matcher %q does not cover Legato's tool %s", entries[0].Matcher, tool)
		}
	}
	for _, tool := range []string{"Bash", "Write", "mcp__github__create_issue", "mcp__legato__show_card", "mcp__plugin_legato-tools_other__show_card", "mcp__plugin_evil_legato__show_card"} {
		if matcher.MatchString(tool) {
			t.Errorf("matcher %q also covers %s", entries[0].Matcher, tool)
		}
	}
	hook := entries[0].Hooks[0]
	if hook.Type != "command" || !strings.Contains(hook.Command, `"permissionDecision":"allow"`) || !strings.Contains(hook.Command, `"hookEventName":"PreToolUse"`) {
		t.Fatalf("hook = %#v, want a command that answers allow", hook)
	}
}

func TestClaudeCodeAdapterLoadsTheToolsModWithoutTheBridgeMod(t *testing.T) {
	t.Setenv("CLAUDE_CODE_PLUGIN_DIRS", "")
	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	adapter.SetToolsModDir("/data/claude-mod/legato-tools")

	vars := adapter.EnvVars("task123", "")

	if vars["CLAUDE_CODE_PLUGIN_DIRS"] != "/data/claude-mod/legato-tools" {
		t.Fatalf("CLAUDE_CODE_PLUGIN_DIRS = %q", vars["CLAUDE_CODE_PLUGIN_DIRS"])
	}
	// The plugin starts the server by this path.
	if vars["LEGATO_BIN"] != "/usr/bin/legato" {
		t.Fatalf("LEGATO_BIN = %q", vars["LEGATO_BIN"])
	}
	// Tools alone do not make the session native: nothing binds an endpoint.
	if adapter.BindsNativeEndpoint() {
		t.Fatal("the tools mod was taken for the bridge mod")
	}
}

func TestClaudeCodeAdapterLoadsTheToolsModBesideTheOthers(t *testing.T) {
	t.Setenv("CLAUDE_CODE_PLUGIN_DIRS", "/home/me/mods/tally")
	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	adapter.SetModDir("/data/claude-mod/legato")
	adapter.SetToolsModDir("/data/claude-mod/legato-tools")

	got := adapter.EnvVars("task123", "")["CLAUDE_CODE_PLUGIN_DIRS"]

	want := strings.Join([]string{"/home/me/mods/tally", "/data/claude-mod/legato", "/data/claude-mod/legato-tools"}, string(os.PathListSeparator))
	if got != want {
		t.Fatalf("CLAUDE_CODE_PLUGIN_DIRS = %q, want %q", got, want)
	}
}

// Claude Code asks before it reads a file outside its working directory. The
// plugin's second hook lets it read an image the person pasted for the
// session without asking, by putting the question to `legato agent
// allow-read`. Every other read must cost nothing and change nothing.
func TestClaudeToolsModAsksLegatoOnlyAboutReadsOfPastedImages(t *testing.T) {
	pluginDir, err := hooks.InstallClaudeToolsMod(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	readJSON(t, filepath.Join(pluginDir, "hooks", "hooks.json"), &config)
	entry := config.Hooks["PreToolUse"][1]
	matcher := regexp.MustCompile("^(?:" + entry.Matcher + ")$")
	if !matcher.MatchString("Read") {
		t.Fatalf("matcher %q does not cover Read", entry.Matcher)
	}
	for _, tool := range []string{"Write", "Edit", "Bash", "NotebookRead", "mcp__plugin_legato-tools_legato__show_card"} {
		if matcher.MatchString(tool) {
			t.Errorf("matcher %q also covers %s", entry.Matcher, tool)
		}
	}
	if len(entry.Hooks) != 1 || entry.Hooks[0].Type != "command" {
		t.Fatalf("hook = %#v, want one command", entry.Hooks)
	}

	// A stand-in for the legato binary records what it was run with and
	// answers as the real command does.
	dir := t.TempDir()
	record := filepath.Join(dir, "calls")
	binary := filepath.Join(dir, "legato")
	script := "#!/bin/sh\n{ echo \"args: $*\"; cat; echo; } >> " + record + "\nprintf 'ALLOWED'\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(payload string, env ...string) string {
		t.Helper()
		cmd := exec.Command("sh", "-c", entry.Hooks[0].Command)
		cmd.Stdin = strings.NewReader(payload)
		cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, env...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("the hook command failed, which would get in the way of the read: %v: %s", err, out)
		}
		return string(out)
	}
	pasted := `{"tool_name":"Read","tool_input":{"file_path":"/tmp/legato-1000/pasted-images/task-1/pasted-1.png"}}`
	other := `{"tool_name":"Read","tool_input":{"file_path":"/repo/internal/server/server.go"}}`

	if out := run(pasted, "LEGATO_BIN="+binary); out != "ALLOWED" {
		t.Errorf("a read of a pasted image: the hook printed %q, want Legato's answer", out)
	}
	recorded, _ := os.ReadFile(record)
	if !strings.Contains(string(recorded), "args: agent allow-read") || !strings.Contains(string(recorded), pasted) {
		t.Errorf("Legato was not asked with the hook's payload: %q", recorded)
	}

	// Any other read does not start Legato at all.
	if err := os.Remove(record); err != nil {
		t.Fatal(err)
	}
	if out := run(other, "LEGATO_BIN="+binary); out != "" {
		t.Errorf("another read: the hook printed %q", out)
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Errorf("Legato was started for a read that names no pasted image")
	}

	// Outside a Legato session there is no binary to ask, and no error.
	if out := run(pasted); out != "" {
		t.Errorf("without LEGATO_BIN: the hook printed %q", out)
	}
}
