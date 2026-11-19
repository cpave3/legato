package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeMachine is a machine for the doctor: the tools on its PATH, what each
// command answers, its environment and its files.
type fakeMachine struct {
	mu      sync.Mutex
	paths   map[string]string
	answers map[string]fakeAnswer
	env     map[string]string
	files   map[string]string
	ran     []string
}

type fakeAnswer struct {
	out string
	err error
}

var errExit1 = errors.New("exit status 1")

// healthyMachine has every tool installed, runnable and signed in.
func healthyMachine() *fakeMachine {
	m := &fakeMachine{
		paths:   map[string]string{},
		answers: map[string]fakeAnswer{},
		env:     map[string]string{"PATH": "/usr/bin:/home/op/.local/bin", "SHELL": "/bin/bash", "HOME": "/home/op", "ANTHROPIC_API_KEY": "k"},
		files:   map[string]string{"/home/op/.chimera/config.json": `{"providers":{"anthropic":{"baseUrl":"https://api.anthropic.com","apiKey":"env:ANTHROPIC_API_KEY"}},"defaultModel":"anthropic/claude"}`},
	}
	for _, tool := range []string{"tmux", "git", "claude", "codex", "chimera", "gh", "st", "yg", "bwrap"} {
		m.paths[tool] = "/usr/bin/" + tool
	}
	m.answer("tmux -V", "tmux 3.4", nil)
	m.answer("git --version", "git version 2.45.0", nil)
	m.answer("claude --version", "2.1.328 (Claude Code)", nil)
	m.answer("claude auth status", `{"loggedIn":true}`, nil)
	m.answer("codex --version", "codex-cli 0.169.0", nil)
	m.answer("codex login status", "Logged in using ChatGPT", nil)
	m.answer("chimera version --json", `{"version":"v1.4.0","release":true,"modApi":[1]}`, nil)
	m.answer("gh --version", "gh version 2.93.0 (2026-05-27)", nil)
	m.answer("gh auth token", "gho_x", nil)
	m.answer("gh auth status", "Logged in to github.com", nil)
	m.answer("st version --json", `{"version":"v0.1.0","release":true}`, nil)
	m.answer("yg --help", "yg manages worktrees", nil)
	m.answer("bwrap --version", "bubblewrap 0.9.0", nil)
	m.answer("/bin/bash -lc printf %s \"$PATH\"", "/usr/bin:/home/op/.local/bin", nil)
	return m
}

func (m *fakeMachine) answer(command, out string, err error) {
	m.answers[command] = fakeAnswer{out: out, err: err}
}

func (m *fakeMachine) remove(tool string) { delete(m.paths, tool) }

func (m *fakeMachine) options() DoctorOptions {
	return DoctorOptions{
		LookPath: func(name string) (string, error) {
			if p, ok := m.paths[name]; ok {
				return p, nil
			}
			return "", errors.New("executable file not found in $PATH")
		},
		Run: func(ctx context.Context, path string, args ...string) ([]byte, error) {
			name := path
			if i := strings.LastIndex(path, "/"); i >= 0 && strings.HasPrefix(path, "/usr/bin/") {
				name = path[i+1:]
			}
			command := strings.Join(append([]string{name}, args...), " ")
			m.mu.Lock()
			m.ran = append(m.ran, command)
			a, ok := m.answers[command]
			m.mu.Unlock()
			if !ok {
				return nil, errors.New("unexpected command " + command)
			}
			if errors.Is(a.err, context.DeadlineExceeded) {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return []byte(a.out), a.err
		},
		Getenv: func(k string) string { return m.env[k] },
		ReadFile: func(path string) ([]byte, error) {
			if s, ok := m.files[path]; ok {
				return []byte(s), nil
			}
			return nil, os.ErrNotExist
		},
		Now:     func() time.Time { return time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC) },
		Timeout: 50 * time.Millisecond,
	}
}

func (m *fakeMachine) ranCommand(command string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.ran {
		if c == command {
			return true
		}
	}
	return false
}

func runDoctor(t *testing.T, m *fakeMachine, cfg DoctorConfig, online bool) DoctorReport {
	t.Helper()
	return NewDoctor(m.options()).Check(context.Background(), cfg, online)
}

func findCheck(t *testing.T, r DoctorReport, id string) DoctorCheck {
	t.Helper()
	for _, c := range r.Checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no check %q in %+v", id, r.Checks)
	return DoctorCheck{}
}

func TestDoctorOnAHealthyMachineEveryCheckIsOK(t *testing.T) {
	r := runDoctor(t, healthyMachine(), DoctorConfig{DefaultAgent: "claude-code"}, false)
	for _, c := range r.Checks {
		if c.Result != DoctorOK {
			t.Errorf("%s: %s (%s)", c.ID, c.Result, c.Detail)
		}
	}
	if !r.OK() {
		t.Fatal("report not OK")
	}
}

func TestDoctorResultsForEachWayAToolCanFail(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(m *fakeMachine)
		id     string
		result DoctorResult
		detail string
	}{
		{"missing", func(m *fakeMachine) { m.remove("tmux") }, "tmux", DoctorMissing, "not found"},
		{"resolves but fails", func(m *fakeMachine) {
			m.answer("chimera version --json", "", errors.New("exit status 1: Error: Cannot find module bin.js"))
		}, "chimera", DoctorFailed, "Cannot find module"},
		{"timed out", func(m *fakeMachine) { m.answer("git --version", "", context.DeadlineExceeded) }, "git", DoctorFailed, "did not answer"},
		{"signed out", func(m *fakeMachine) { m.answer("claude auth status", `{"loggedIn":false}`, errExit1) }, "claude-code.auth", DoctorNotSignedIn, ""},
		{"codex signed out", func(m *fakeMachine) { m.answer("codex login status", "Not logged in", errExit1) }, "codex.auth", DoctorNotSignedIn, ""},
		{"gh signed out", func(m *fakeMachine) { m.answer("gh auth token", "", errExit1) }, "gh.auth", DoctorNotSignedIn, ""},
		{"chimera too old", func(m *fakeMachine) {
			m.answer("chimera version --json", `{"version":"v0.9.0","release":true}`, nil)
		}, "chimera", DoctorFailed, "mod"},
		{"chimera without a model", func(m *fakeMachine) { m.files["/home/op/.chimera/config.json"] = `{"providers":{"a":{"apiKey":"x"}}}` }, "chimera.auth", DoctorNotSignedIn, "defaultModel"},
		{"chimera key unset", func(m *fakeMachine) { delete(m.env, "ANTHROPIC_API_KEY") }, "chimera.auth", DoctorNotSignedIn, "ANTHROPIC_API_KEY"},
		{"chimera with no config", func(m *fakeMachine) { delete(m.files, "/home/op/.chimera/config.json") }, "chimera.auth", DoctorNotSignedIn, "config.json"},
		{"st without version", func(m *fakeMachine) { m.answer("st version --json", "unknown command", errExit1) }, "st", DoctorFailed, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := healthyMachine()
			tc.break_(m)
			c := findCheck(t, runDoctor(t, m, DoctorConfig{DefaultAgent: "claude-code"}, false), tc.id)
			if c.Result != tc.result {
				t.Fatalf("result = %s, want %s (%s)", c.Result, tc.result, c.Detail)
			}
			if !strings.Contains(c.Detail, tc.detail) {
				t.Errorf("detail %q does not mention %q", c.Detail, tc.detail)
			}
			if c.Fix == "" || strings.Contains(c.Fix, "\n") {
				t.Errorf("fix %q is not one line", c.Fix)
			}
		})
	}
}

func TestDoctorSignInChecksOfAMissingToolAreSkipped(t *testing.T) {
	m := healthyMachine()
	m.remove("codex")
	r := runDoctor(t, m, DoctorConfig{DefaultAgent: "claude-code"}, false)
	if c := findCheck(t, r, "codex.auth"); c.Result != DoctorSkipped {
		t.Fatalf("codex.auth = %s", c.Result)
	}
	if m.ranCommand("codex login status") {
		t.Fatal("ran codex login status without codex")
	}
}

func TestDoctorSignedInSaysCredentialsArePresentNotProven(t *testing.T) {
	r := runDoctor(t, healthyMachine(), DoctorConfig{DefaultAgent: "claude-code"}, false)
	if c := findCheck(t, r, "claude-code.auth"); !strings.Contains(c.Detail, "not proven") {
		t.Fatalf("detail %q", c.Detail)
	}
}

func TestDoctorRequiredChecks(t *testing.T) {
	required := func(r DoctorReport) map[string]bool {
		out := map[string]bool{}
		for _, c := range r.Checks {
			if c.Required {
				out[c.ID] = true
			}
		}
		return out
	}
	t.Run("default", func(t *testing.T) {
		got := required(runDoctor(t, healthyMachine(), DoctorConfig{DefaultAgent: "codex"}, false))
		for _, id := range []string{"tmux", "git", "codex", "codex.auth"} {
			if !got[id] {
				t.Errorf("%s not required", id)
			}
		}
		for _, id := range []string{"claude-code", "claude-code.auth", "chimera", "gh", "gh.auth", "st", "yg", "bwrap", "path"} {
			if got[id] {
				t.Errorf("%s required", id)
			}
		}
	})
	t.Run("no default agent means Claude Code", func(t *testing.T) {
		if got := required(runDoctor(t, healthyMachine(), DoctorConfig{}, false)); !got["claude-code"] {
			t.Error("claude-code not required")
		}
	})
	t.Run("sandbox on", func(t *testing.T) {
		if got := required(runDoctor(t, healthyMachine(), DoctorConfig{SandboxEnabled: true}, false)); !got["bwrap"] {
			t.Error("bwrap not required with the sandbox on")
		}
	})
	t.Run("promoted by a setting", func(t *testing.T) {
		r := runDoctor(t, healthyMachine(), DoctorConfig{RequiredBy: map[string]string{"gh": "PR tracking (github.auth: gh)", "gh.auth": "PR tracking (github.auth: gh)"}}, false)
		c := findCheck(t, r, "gh.auth")
		if !c.Required || c.RequiredBy != "PR tracking (github.auth: gh)" {
			t.Errorf("gh.auth = %+v", c)
		}
	})
}

func TestDoctorFailsOnlyOnRequiredChecks(t *testing.T) {
	m := healthyMachine()
	m.remove("yg")
	if r := runDoctor(t, m, DoctorConfig{}, false); !r.OK() {
		t.Fatal("an optional tool missing failed the report")
	}
	m.remove("tmux")
	r := runDoctor(t, m, DoctorConfig{}, false)
	if r.OK() {
		t.Fatal("missing tmux did not fail the report")
	}
	if failed := r.RequiredFailed(); len(failed) != 1 || failed[0].ID != "tmux" {
		t.Fatalf("required failed = %+v", failed)
	}
}

func TestDoctorReportsALoginShellPATHThatDiffers(t *testing.T) {
	m := healthyMachine()
	m.answer("/bin/bash -lc printf %s \"$PATH\"", "/usr/bin:/home/op/.local/bin:/home/op/.cargo/bin", nil)
	c := findCheck(t, runDoctor(t, m, DoctorConfig{}, false), "path")
	if c.Result != DoctorFailed || c.Required {
		t.Fatalf("path = %+v", c)
	}
	if !strings.Contains(c.Detail, "/home/op/.cargo/bin") {
		t.Errorf("detail %q does not name the difference", c.Detail)
	}
}

func TestDoctorOnlineCheckRunsOnlyWhenAsked(t *testing.T) {
	m := healthyMachine()
	r := runDoctor(t, m, DoctorConfig{}, false)
	if m.ranCommand("gh auth status") {
		t.Fatal("gh auth status ran without --online")
	}
	for _, c := range r.Checks {
		if c.ID == "gh.online" {
			t.Fatal("gh.online in an offline report")
		}
	}
	m.answer("gh auth status", "The token in GH_TOKEN is invalid", errExit1)
	c := findCheck(t, runDoctor(t, m, DoctorConfig{}, true), "gh.online")
	if c.Result != DoctorNotSignedIn {
		t.Fatalf("gh.online = %+v", c)
	}
}

func TestDoctorNeverStartsASessionOrABrowser(t *testing.T) {
	m := healthyMachine()
	runDoctor(t, m, DoctorConfig{}, true)
	for _, c := range m.ran {
		for _, bad := range []string{" login\x00", "auth login", "claude -p", "codex exec", "chimera run", "xdg-open", "update"} {
			if strings.Contains(c+"\x00", bad) {
				t.Errorf("ran %q", c)
			}
		}
	}
}
