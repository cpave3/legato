package hooks_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
)

type hooksFile struct {
	Hooks map[string][]struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Type    string `json:"type"`
			Command string `json:"command"`
			Timeout int    `json:"timeout"`
		} `json:"hooks"`
	} `json:"hooks"`
}

// entries is each Claude Code event's commands, with their time limits.
func (f hooksFile) entries(t *testing.T) map[string]map[string]int {
	t.Helper()
	out := map[string]map[string]int{}
	for event, groups := range f.Hooks {
		if len(groups) != 1 || groups[0].Matcher != "" {
			t.Fatalf("%s: %#v, want one group for every call", event, groups)
		}
		out[event] = map[string]int{}
		for _, hook := range groups[0].Hooks {
			if hook.Type != "command" {
				t.Fatalf("%s: %#v, want command hooks only", event, hook)
			}
			out[event][hook.Command] = hook.Timeout
		}
	}
	return out
}

func readHooksBundle(t *testing.T, bundle string) hooksFile {
	t.Helper()
	var manifest struct {
		Name string `json:"name"`
	}
	readJSON(t, filepath.Join(bundle, ".claude-plugin", "plugin.json"), &manifest)
	if manifest.Name != hooks.HooksBundleName {
		t.Fatalf("plugin name = %q", manifest.Name)
	}
	var file hooksFile
	readJSON(t, filepath.Join(bundle, "hooks", "hooks.json"), &file)
	return file
}

// runnerEvent is the --event a runner entry passes, "" for a command that
// calls no runner.
func runnerEvent(command string) string {
	_, after, ok := strings.Cut(command, "agent managed-hooks run --tool claude-code --event ")
	if !ok {
		return ""
	}
	event, _, _ := strings.Cut(after, " ")
	return event
}

func TestTheClaudeCodeHooksBundleRegistersEachDeliveredEventAndTheTwoInternalEntries(t *testing.T) {
	root := t.TempDir()
	env := map[string]string{"CLAUDE_CODE_PLUGIN_DIRS": "/state"}
	bundle, err := hooks.NewClaudeCodeAdapter("/opt/legato").HooksBundle(root, map[string]int{"stop": 300}, env)
	if err != nil {
		t.Fatal(err)
	}
	if bundle != filepath.Join(root, "legato-hooks") {
		t.Fatalf("bundle = %q", bundle)
	}
	if want := "/state" + string(os.PathListSeparator) + bundle; env["CLAUDE_CODE_PLUGIN_DIRS"] != want {
		t.Fatalf("CLAUDE_CODE_PLUGIN_DIRS = %q, want %q", env["CLAUDE_CODE_PLUGIN_DIRS"], want)
	}
	if env["LEGATO_BIN"] != "/opt/legato" {
		t.Fatalf("LEGATO_BIN = %q, want the binary the entries run", env["LEGATO_BIN"])
	}
	got := map[string]map[string]int{}
	for event, commands := range readHooksBundle(t, bundle).entries(t) {
		got[event] = map[string]int{}
		for command, timeout := range commands {
			if !strings.Contains(command, "--bundle "+bundle) {
				t.Fatalf("%s: %q does not name its bundle", event, command)
			}
			got[event][runnerEvent(command)] = timeout
		}
	}
	want := map[string]map[string]int{
		"SessionStart":     {"canary": 10},
		"UserPromptSubmit": {"turn_reset": 10},
		// The runner's own time limit for the event, with room to report a
		// hook that ran out of time.
		"Stop": {"stop": 300 + hooks.HooksEntryGrace},
	}
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for event, entries := range want {
		if !slices.Equal(sortedKeys(got[event]), sortedKeys(entries)) {
			t.Fatalf("%s = %v, want %v", event, got[event], entries)
		}
		for name, timeout := range entries {
			if got[event][name] != timeout {
				t.Fatalf("%s %s times out after %d s, want %d", event, name, got[event][name], timeout)
			}
		}
	}
}

func TestASessionStartHookRunsBesideTheCanary(t *testing.T) {
	bundle, err := hooks.NewClaudeCodeAdapter("legato").HooksBundle(t.TempDir(), map[string]int{"session_start": 30, "stop": 120}, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	entries := readHooksBundle(t, bundle).entries(t)
	var events []string
	for command := range entries["SessionStart"] {
		events = append(events, runnerEvent(command))
	}
	slices.Sort(events)
	if !slices.Equal(events, []string{"canary", "session_start"}) {
		t.Fatalf("SessionStart runs %v", events)
	}
}

func TestTheClaudeCodeHooksBundleRefusesAnEventItCannotDeliver(t *testing.T) {
	root := t.TempDir()
	env := map[string]string{}
	if _, err := hooks.NewClaudeCodeAdapter("legato").HooksBundle(root, map[string]int{"pre_tool": 10}, env); err == nil {
		t.Fatal("pre_tool was registered; Phase 1 delivers session_start and stop only")
	}
	if _, ok := env["CLAUDE_CODE_PLUGIN_DIRS"]; ok {
		t.Fatal("a bundle that failed was put in CLAUDE_CODE_PLUGIN_DIRS")
	}
}

// The runner does not exist before LEG-698, and today's legato answers an
// unknown agent command with exit 2, which Claude Code takes as a block; a
// Go panic exits 2 as well. An entry passes unless the runner exits 0, and
// passes on only what such a runner printed.
func TestAnEntryPassesUnlessTheRunnerAnswers(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	bundle, err := hooks.NewClaudeCodeAdapter("legato").HooksBundle(t.TempDir(), map[string]int{"stop": 120}, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	var stop string
	for command := range readHooksBundle(t, bundle).entries(t)["Stop"] {
		stop = command
	}
	bin := t.TempDir()
	script := func(name, body string) string {
		path := filepath.Join(bin, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	block := `{"decision":"block","reason":"net debt rose"}`
	cases := []struct {
		name, legatoBin, wantOut string
	}{
		{"today's legato, which has no runner", script("old", "cat >/dev/null\necho 'unknown agent command \"managed-hooks\"' >&2\nexit 2\n"), ""},
		{"a runner that panics", script("panic", "cat >/dev/null\necho '{\"decision\":' \necho 'panic: boom' >&2\nexit 2\n"), ""},
		{"a runner that fails", script("fail", "cat >/dev/null\nexit 1\n"), ""},
		{"no binary there", filepath.Join(bin, "missing"), ""},
		{"no LEGATO_BIN", "", ""},
		{"a runner that blocks", script("block", "read -r payload\n[ \"$payload\" = '{\"hook_event_name\":\"Stop\"}' ] || exit 1\necho '"+block+"'\n"), block},
		{"a runner that passes", script("pass", "cat >/dev/null\nexit 0\n"), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd := exec.Command("sh", "-c", stop)
			cmd.Env = append(os.Environ(), "LEGATO_BIN="+c.legatoBin)
			cmd.Stdin = strings.NewReader(`{"hook_event_name":"Stop"}` + "\n")
			var stdout bytes.Buffer
			cmd.Stdout = &stdout
			if err := cmd.Run(); err != nil {
				t.Fatalf("the entry exited with %v, want 0", err)
			}
			if got := strings.TrimSpace(stdout.String()); got != c.wantOut {
				t.Fatalf("the entry printed %q, want %q", got, c.wantOut)
			}
		})
	}
}

func sortedKeys(m map[string]int) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
