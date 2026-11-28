package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The repository runs CodeEval as Claude Code hooks (.claude/settings.json,
// LEG-707). These tests run the committed hook commands as Claude Code runs
// them, in a scratch repository, with fake codeeval binaries standing in for
// the real one: which binary a hook finds, and that it stays silent when it
// finds none.

const stopPayload = `{"session_id":"s","hook_event_name":"Stop","stop_hook_active":false}`

// codeEvalHookCommands returns the command of each CodeEval hook the
// repository's .claude/settings.json registers, by event.
func codeEvalHookCommands(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	commands := map[string]string{}
	for event, groups := range settings.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				if strings.Contains(h.Command, "/.claude/hooks/codeeval-") {
					commands[event] = h.Command
				}
			}
		}
	}
	for _, event := range []string{"SessionStart", "Stop"} {
		if commands[event] == "" {
			t.Fatalf("settings.json registers no CodeEval %s hook", event)
		}
	}
	return commands
}

// hookProject is a scratch git repository holding the repository's
// .claude/hooks, with a home folder of its own.
type hookProject struct {
	dir, home, bin string
}

func newHookProject(t *testing.T) hookProject {
	t.Helper()
	p := hookProject{dir: t.TempDir(), home: t.TempDir(), bin: t.TempDir()}
	hooks := filepath.Join(p.dir, ".claude", "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	scripts, err := filepath.Glob(filepath.Join("..", "..", ".claude", "hooks", "codeeval-*.sh"))
	if err != nil || len(scripts) == 0 {
		t.Fatalf("no CodeEval hook scripts in .claude/hooks: %v", err)
	}
	for _, s := range scripts {
		data, err := os.ReadFile(s)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(s)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&0o111 == 0 {
			t.Errorf("%s is not executable", s)
		}
		if err := os.WriteFile(filepath.Join(hooks, filepath.Base(s)), data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "base"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = p.dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return p
}

// fakeCodeEval writes an executable at path that fails every check, naming
// itself, as a real codeeval does when a change adds debt.
func fakeCodeEval(t *testing.T, path, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho 'found " + name + "'\necho 'Failure fingerprint: `" + name + "`'\nexit 3\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func (p hookProject) homeCodeEval() string {
	return filepath.Join(p.home, ".local", "share", "codeeval", "current", "codeeval")
}

// run runs command as Claude Code runs a hook command, with only the
// environment given beside a PATH of the project's bin folder then the
// test's own.
func (p hookProject) run(t *testing.T, command, stdin string, env ...string) (code int, stdout, stderr string) {
	t.Helper()
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = p.dir
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = append([]string{
		"PATH=" + p.bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + p.home,
		"CLAUDE_PROJECT_DIR=" + p.dir,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
	}, env...)
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		code = exit.ExitCode()
	default:
		t.Fatal(err)
	}
	return code, out.String(), errOut.String()
}

func skipWithCodeEvalOnPath(t *testing.T) {
	t.Helper()
	if path, err := exec.LookPath("codeeval"); err == nil {
		t.Skipf("a codeeval is on PATH (%s), so a hook would find it", path)
	}
}

func TestCodeEvalHooksNameNoMachinePath(t *testing.T) {
	for event, command := range codeEvalHookCommands(t) {
		for _, machine := range []string{"/home/", "/var/", "/Users/", "/opt/", "/usr/"} {
			if strings.Contains(command, machine) {
				t.Errorf("the %s hook pins a path on one machine (%q): %s\nRe-running `codeeval hook install` pins one; restore the committed command.", event, machine, command)
			}
		}
	}
}

func TestCodeEvalHooksDoNothingWithoutCodeEval(t *testing.T) {
	skipWithCodeEvalOnPath(t)
	commands := codeEvalHookCommands(t)
	p := newHookProject(t)
	for event, stdin := range map[string]string{
		"SessionStart": `{"session_id":"s","hook_event_name":"SessionStart","source":"startup"}`,
		"Stop":         stopPayload,
	} {
		code, stdout, stderr := p.run(t, commands[event], stdin)
		if code != 0 || stdout != "" || stderr != "" {
			t.Errorf("%s hook without CodeEval: exit %d, stdout %q, stderr %q; want exit 0 and no output", event, code, stdout, stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(p.dir, ".git", "codeeval-broad")); err == nil {
		t.Error("the Stop hook started a background check without CodeEval")
	}
}

func TestCodeEvalStopHookFindsCodeEval(t *testing.T) {
	skipWithCodeEvalOnPath(t)
	stop := codeEvalHookCommands(t)["Stop"]
	cases := []struct {
		name  string
		setup func(t *testing.T, p hookProject) []string // returns extra environment
		found string
	}{
		{"in the shared install under HOME", func(t *testing.T, p hookProject) []string {
			fakeCodeEval(t, p.homeCodeEval(), "home")
			return nil
		}, "home"},
		{"on PATH", func(t *testing.T, p hookProject) []string {
			fakeCodeEval(t, filepath.Join(p.bin, "codeeval"), "path")
			return nil
		}, "path"},
		{"under HOME before PATH", func(t *testing.T, p hookProject) []string {
			fakeCodeEval(t, p.homeCodeEval(), "home")
			fakeCodeEval(t, filepath.Join(p.bin, "codeeval"), "path")
			return nil
		}, "home"},
		{"CODEEVAL_BIN before the rest", func(t *testing.T, p hookProject) []string {
			fakeCodeEval(t, p.homeCodeEval(), "home")
			fakeCodeEval(t, filepath.Join(p.bin, "codeeval"), "path")
			env := filepath.Join(t.TempDir(), "codeeval")
			fakeCodeEval(t, env, "env")
			return []string{"CODEEVAL_BIN=" + env}
		}, "env"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := newHookProject(t)
			env := append(c.setup(t, p), "CODEEVAL_STOP_CHECKS=quick")
			code, _, stderr := p.run(t, stop, stopPayload, env...)
			if code != 2 || !strings.Contains(stderr, "found "+c.found+"\n") {
				t.Errorf("exit %d, stderr:\n%s\nwant exit 2 naming %q", code, stderr, "found "+c.found)
			}
		})
	}
}

func TestCodeEvalStopHookOptsOut(t *testing.T) {
	skipWithCodeEvalOnPath(t)
	stop := codeEvalHookCommands(t)["Stop"]
	for _, env := range []string{"CODEEVAL_HOOK=off", "CODEEVAL_BIN=/bin/false"} {
		t.Run(env, func(t *testing.T) {
			p := newHookProject(t)
			fakeCodeEval(t, p.homeCodeEval(), "home")
			code, stdout, stderr := p.run(t, stop, stopPayload, env, "CODEEVAL_STOP_CHECKS=quick")
			if code != 0 || stdout != "" || stderr != "" {
				t.Errorf("exit %d, stdout %q, stderr %q; want exit 0 and no output", code, stdout, stderr)
			}
		})
	}
}
