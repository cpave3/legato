package hooks_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
)

// Legato's Claude Code state hooks ship as a plugin Legato writes each time
// it starts and loads into every session it launches, so they are never
// older than the binary that reads them.
func TestClaudeStateModReportsEveryHookTheStateNeeds(t *testing.T) {
	pluginDir, err := hooks.InstallClaudeStateMod(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(pluginDir) != "legato-state" {
		t.Fatalf("plugin dir = %q", pluginDir)
	}
	var manifest struct {
		Name string `json:"name"`
	}
	readJSON(t, filepath.Join(pluginDir, ".claude-plugin", "plugin.json"), &manifest)
	if manifest.Name != "legato-state" {
		t.Fatalf("manifest name = %q", manifest.Name)
	}
	var config struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	readJSON(t, filepath.Join(pluginDir, "hooks", "hooks.json"), &config)
	// The events the session's bridge answers as well (LEG-684): Claude
	// Code's security mod passes these over the Legato mod, so a command
	// hook answers them for it.
	bridged := map[string]bool{"PermissionRequest": true, "StopFailure": true}
	var events []string
	commands := map[string]bool{}
	bridgeCommands := map[string]bool{}
	for event, groups := range config.Hooks {
		events = append(events, event)
		want := 1
		if bridged[event] {
			want = 2
		}
		if len(groups) != 1 || groups[0].Matcher != "" || len(groups[0].Hooks) != want {
			t.Fatalf("%s: %#v, want %d commands for every call", event, groups, want)
		}
		for _, hook := range groups[0].Hooks {
			if hook.Type != "command" {
				t.Fatalf("%s: %#v, want command hooks only", event, hook)
			}
		}
		commands[groups[0].Hooks[0].Command] = true
		if bridged[event] {
			bridge := groups[0].Hooks[1]
			// Its call lasts as long as the person takes to decide.
			if bridge.Timeout < 3600 {
				t.Fatalf("%s: the bridge hook times out after %d seconds", event, bridge.Timeout)
			}
			bridgeCommands[bridge.Command] = true
		}
	}
	slices.Sort(events)
	want := []string{"PermissionRequest", "PostCompact", "PostToolUse", "PreCompact", "SessionEnd", "SessionStart", "Stop", "StopFailure", "SubagentStart", "SubagentStop", "UserPromptSubmit"}
	if !slices.Equal(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	if len(commands) != 1 || len(bridgeCommands) != 1 {
		t.Fatalf("commands = %v, bridge commands = %v, want the same one for every event", commands, bridgeCommands)
	}
	var command, bridgeCommand string
	for c := range commands {
		command = c
	}
	for c := range bridgeCommands {
		bridgeCommand = c
	}
	t.Run("bridge", func(t *testing.T) { testClaudeBridgeHookCommand(t, bridgeCommand) })

	// A stand-in for the legato binary records how it was run.
	dir := t.TempDir()
	record := filepath.Join(dir, "calls")
	binary := filepath.Join(dir, "legato")
	script := "#!/bin/sh\n{ echo \"args: $*\"; cat; echo; } >> " + record + "\necho 'some output'\nexit 3\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(env ...string) string {
		t.Helper()
		cmd := exec.Command("sh", "-c", command)
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader(`{"hook_event_name":"Stop"}`)
		cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, env...)
		out, err := cmd.CombinedOutput()
		// A failing hook is shown in the session; a UserPromptSubmit hook's
		// output becomes part of the prompt.
		if err != nil || len(out) != 0 {
			t.Fatalf("the hook command failed or printed: %v: %q", err, out)
		}
		return string(out)
	}

	run("LEGATO_BIN="+binary, "LEGATO_TASK_ID=task1", "LEGATO_EXECUTION_ID=ex-1")
	recorded, _ := os.ReadFile(record)
	wantArgs := "args: agent hook claude-code task1 --execution-id ex-1 --working-dir " + dir
	if !strings.Contains(string(recorded), wantArgs) || !strings.Contains(string(recorded), `{"hook_event_name":"Stop"}`) {
		t.Fatalf("Legato was not given the payload: %q, want %q", recorded, wantArgs)
	}

	// Outside a Legato session the hook does nothing.
	if err := os.Remove(record); err != nil {
		t.Fatal(err)
	}
	run("LEGATO_BIN=" + binary)
	run("LEGATO_TASK_ID=task1")
	run("LEGATO_BIN="+binary, "LEGATO_TASK_ID=task1") // no execution: not a session Legato started
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatal("Legato was run outside a Legato session")
	}
}

// testClaudeBridgeHookCommand runs the bridge hook's command as Claude Code
// would: its output is the hook's answer, so it must come through unchanged.
func testClaudeBridgeHookCommand(t *testing.T, command string) {
	dir := t.TempDir()
	record := filepath.Join(dir, "calls")
	binary := filepath.Join(dir, "legato")
	script := "#!/bin/sh\n{ echo \"args: $*\"; cat; echo; } >> " + record + "\necho '{\"hookSpecificOutput\":{}}'\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := `{"hook_event_name":"PermissionRequest"}`
	run := func(env ...string) string {
		t.Helper()
		cmd := exec.Command("sh", "-c", command)
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader(payload)
		cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, env...)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("the hook command failed: %v: %q", err, out)
		}
		return string(out)
	}

	// Without a bridge (native_bridge off, or not ready yet) nothing runs.
	if out := run("LEGATO_BIN=" + binary); out != "" {
		t.Fatalf("printed %q without a bridge", out)
	}
	if out := run("LEGATO_CLAUDE_BRIDGE_URL=http://127.0.0.1:1/secret"); out != "" {
		t.Fatalf("printed %q without a binary", out)
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatal("Legato was run without a bridge")
	}

	out := run("LEGATO_BIN="+binary, "LEGATO_CLAUDE_BRIDGE_URL=http://127.0.0.1:1/secret")
	if out != "{\"hookSpecificOutput\":{}}\n" {
		t.Fatalf("the answer did not come through: %q", out)
	}
	recorded, _ := os.ReadFile(record)
	if !strings.Contains(string(recorded), "args: agent bridge-hook claude-code\n"+payload) {
		t.Fatalf("Legato was not given the payload: %q", recorded)
	}
}

func TestClaudeCodeAdapterLoadsTheStateModBesideTheOthers(t *testing.T) {
	t.Setenv("CLAUDE_CODE_PLUGIN_DIRS", "")
	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	adapter.SetStateModDir("/data/claude-mod/legato-state")
	adapter.SetToolsModDir("/data/claude-mod/legato-tools")

	vars := adapter.EnvVars("task123", "")

	want := strings.Join([]string{"/data/claude-mod/legato-tools", "/data/claude-mod/legato-state"}, string(os.PathListSeparator))
	if vars["CLAUDE_CODE_PLUGIN_DIRS"] != want {
		t.Fatalf("CLAUDE_CODE_PLUGIN_DIRS = %q, want %q", vars["CLAUDE_CODE_PLUGIN_DIRS"], want)
	}
	if vars["LEGATO_BIN"] != "/usr/bin/legato" {
		t.Fatalf("LEGATO_BIN = %q", vars["LEGATO_BIN"])
	}
}

func TestClaudeCodeAdapterLoadsTheStateModAlone(t *testing.T) {
	t.Setenv("CLAUDE_CODE_PLUGIN_DIRS", "")
	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	adapter.SetStateModDir("/data/claude-mod/legato-state")

	vars := adapter.EnvVars("task123", "")

	if vars["CLAUDE_CODE_PLUGIN_DIRS"] != "/data/claude-mod/legato-state" || vars["LEGATO_BIN"] != "/usr/bin/legato" {
		t.Fatalf("vars = %v", vars)
	}
}
