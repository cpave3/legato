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
			} `json:"hooks"`
		} `json:"hooks"`
	}
	readJSON(t, filepath.Join(pluginDir, "hooks", "hooks.json"), &config)
	var events []string
	commands := map[string]bool{}
	for event, groups := range config.Hooks {
		events = append(events, event)
		if len(groups) != 1 || groups[0].Matcher != "" || len(groups[0].Hooks) != 1 || groups[0].Hooks[0].Type != "command" {
			t.Fatalf("%s: %#v, want one command for every call", event, groups)
		}
		commands[groups[0].Hooks[0].Command] = true
	}
	slices.Sort(events)
	want := []string{"PermissionRequest", "PostCompact", "PostToolUse", "PreCompact", "SessionEnd", "SessionStart", "Stop", "StopFailure", "UserPromptSubmit"}
	if !slices.Equal(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	if len(commands) != 1 {
		t.Fatalf("commands = %v, want the same one for every event", commands)
	}
	var command string
	for c := range commands {
		command = c
	}

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
