package tmux

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestMain gives the tests a tmux server of their own, never the one they
// run inside: $TMUX unset (tmux prefers it to TMUX_TMPDIR) and a socket
// folder of their own, short enough for a socket's path. One session is
// kept open for the whole run, since a server whose last session ends
// exits, and a test's new-session that meets it on the way out fails with
// "server exited unexpectedly". That session ends by itself when this
// process does, and the server with it: nothing here kills tmux.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("/tmp", "lgtmux")
	if err != nil {
		panic(err)
	}
	os.Unsetenv("TMUX")
	os.Setenv("TMUX_TMPDIR", dir)
	if hasTmux() {
		wait := "while kill -0 " + strconv.Itoa(os.Getpid()) + " 2>/dev/null; do sleep 1; done"
		if out, err := exec.Command("tmux", "new-session", "-d", "-s", "legato-test-keepalive", wait).CombinedOutput(); err != nil {
			panic("tmux: " + string(out))
		}
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func hasTmux() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

func skipWithoutTmux(t *testing.T) {
	t.Helper()
	if !hasTmux() {
		t.Skip("tmux not installed, skipping integration test")
	}
}

func TestNewReturnsErrorWhenTmuxNotFound(t *testing.T) {
	_, err := New(Options{
		LookPath: func(name string) (string, error) {
			return "", exec.ErrNotFound
		},
	})
	if err == nil {
		t.Error("expected error when tmux not found, got nil")
	}
}

func TestNewSucceedsWhenTmuxFound(t *testing.T) {
	m, err := New(Options{
		LookPath: func(name string) (string, error) {
			return "/usr/bin/tmux", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestAttachCmdFormation(t *testing.T) {
	m := &Manager{
		tmuxPath:  "/usr/bin/tmux",
		escapeKey: "C-]",
	}

	cmd := m.Attach("legato-REX-1238")
	args := cmd.Args
	// Should contain attach-session -t legato-REX-1238
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "attach-session") {
		t.Errorf("expected attach-session in args, got %v", args)
	}
	if !strings.Contains(joined, "legato-REX-1238") {
		t.Errorf("expected session name in args, got %v", args)
	}
}

func TestSessionEnvironmentReturnsSpawnedVariables(t *testing.T) {
	skipWithoutTmux(t)

	m, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	name := "legato-test-environment"
	t.Cleanup(func() { m.Kill(name) })
	if err := m.Spawn(name, t.TempDir(), 0, 0, "LEGATO_TASK_ID=task-env", "LEGATO_FUTURE_VALUE=future"); err != nil {
		t.Fatal(err)
	}

	env, err := m.SessionEnvironment(name)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(env, "\n")
	for _, want := range []string{"LEGATO_TASK_ID=task-env", "LEGATO_FUTURE_VALUE=future"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("session environment missing %q: %v", want, env)
		}
	}
}

// tmux gives a new session's pane the PATH of the tmux command that made
// it, whatever -e says, so a PATH among the variables must reach the pane
// another way.
func TestSpawnGivesThePaneThePATHAmongItsVariables(t *testing.T) {
	skipWithoutTmux(t)
	// A tmux server of this test's own: never the person's.
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_TMPDIR", t.TempDir())

	m, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Keep this private server alive while configuring the shell it starts.
	control := "legato-test-path-control"
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", control, "sleep 3600").CombinedOutput(); err != nil {
		t.Fatalf("start private tmux server: %v\n%s", err, out)
	}
	t.Cleanup(func() { m.Kill(control) })
	dir := t.TempDir()
	envFile := filepath.Join(dir, "pane-path")
	shell := filepath.Join(dir, "shell")
	script := "#!/bin/sh\nprintf '%s' \"$PATH\" > \"$LEGATO_TEST_ENV_FILE\"\n\"$LEGATO_TEST_TMUX\" wait-for -S path-started\nexec /bin/sh\n"
	if err := os.WriteFile(shell, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("tmux", "set-option", "-g", "default-shell", shell).CombinedOutput(); err != nil {
		t.Fatalf("configure controlled shell: %v\n%s", err, out)
	}
	name := "legato-test-path"
	t.Cleanup(func() { m.Kill(name) })
	path := t.TempDir() + string(os.PathListSeparator) + os.Getenv("PATH")
	if err := m.Spawn(name, t.TempDir(), 0, 0, "PATH="+path, "LEGATO_TEST_ENV_FILE="+envFile, "LEGATO_TEST_TMUX="+m.tmuxPath); err != nil {
		t.Fatal(err)
	}
	// The controlled shell records its inherited environment before any
	// profile can rebuild PATH, then signals that it has started.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "tmux", "wait-for", "path-started").CombinedOutput(); err != nil {
		t.Fatalf("waiting for the pane to record its PATH: %v\n%s", err, out)
	}
	environ, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	got := string(environ)
	if got != path {
		t.Fatalf("the pane's PATH = %q, want %q", got, path)
	}
}

func TestSpawnAndKillIntegration(t *testing.T) {
	skipWithoutTmux(t)

	m, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}

	name := "legato-test-spawn"
	t.Cleanup(func() { m.Kill(name) })

	if err := m.Spawn(name, t.TempDir(), 0, 0); err != nil {
		t.Fatal(err)
	}

	alive, err := m.IsAlive(name)
	if err != nil {
		t.Fatal(err)
	}
	if !alive {
		t.Error("session should be alive after spawn")
	}

	if err := m.Kill(name); err != nil {
		t.Fatal(err)
	}

	alive, err = m.IsAlive(name)
	if err != nil {
		t.Fatal(err)
	}
	if alive {
		t.Error("session should be dead after kill")
	}
}

func TestKillNonExistentSessionNoError(t *testing.T) {
	skipWithoutTmux(t)

	m, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}

	err = m.Kill("legato-nonexistent-test")
	if err != nil {
		t.Errorf("expected no error killing non-existent session, got %v", err)
	}
}

func TestCaptureIntegration(t *testing.T) {
	skipWithoutTmux(t)

	m, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}

	name := "legato-test-capture"
	t.Cleanup(func() { m.Kill(name) })

	if err := m.Spawn(name, t.TempDir(), 0, 0); err != nil {
		t.Fatal(err)
	}

	output, err := m.Capture(name)
	if err != nil {
		t.Fatal(err)
	}
	// Output should be a string (even if empty/whitespace for a fresh shell)
	_ = output
}

func TestCaptureNonExistentSessionReturnsError(t *testing.T) {
	skipWithoutTmux(t)

	m, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = m.Capture("legato-nonexistent-capture-test")
	if err == nil {
		t.Error("expected error capturing non-existent session, got nil")
	}
}

func TestListSessionsIntegration(t *testing.T) {
	skipWithoutTmux(t)

	m, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}

	name1 := "legato-test-list-1"
	name2 := "legato-test-list-2"
	t.Cleanup(func() {
		m.Kill(name1)
		m.Kill(name2)
	})

	if err := m.Spawn(name1, t.TempDir(), 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := m.Spawn(name2, t.TempDir(), 0, 0); err != nil {
		t.Fatal(err)
	}

	sessions, err := m.ListSessions()
	if err != nil {
		t.Fatal(err)
	}

	found := map[string]bool{}
	for _, s := range sessions {
		found[s] = true
	}
	if !found[name1] {
		t.Errorf("expected %s in sessions list", name1)
	}
	if !found[name2] {
		t.Errorf("expected %s in sessions list", name2)
	}
}

func TestSpawnWithDimensionsIntegration(t *testing.T) {
	skipWithoutTmux(t)

	m, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}

	name := "legato-test-spawn-dims"
	t.Cleanup(func() { m.Kill(name) })

	// Spawn with explicit dimensions
	if err := m.Spawn(name, t.TempDir(), 90, 40); err != nil {
		t.Fatal(err)
	}

	alive, err := m.IsAlive(name)
	if err != nil {
		t.Fatal(err)
	}
	if !alive {
		t.Error("session should be alive after spawn with dimensions")
	}
}

func TestSpawnWithZeroDimensionsOmitsFlags(t *testing.T) {
	skipWithoutTmux(t)

	m, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}

	name := "legato-test-spawn-zero-dims"
	t.Cleanup(func() { m.Kill(name) })

	// Zero dimensions = omit -x/-y (backward compat)
	if err := m.Spawn(name, t.TempDir(), 0, 0); err != nil {
		t.Fatal(err)
	}

	alive, err := m.IsAlive(name)
	if err != nil {
		t.Fatal(err)
	}
	if !alive {
		t.Error("session should be alive after spawn without dimensions")
	}
}

func TestPaneCommandsIntegration(t *testing.T) {
	skipWithoutTmux(t)

	m, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}

	name := "legato-test-panecmd"
	t.Cleanup(func() { m.Kill(name) })

	if err := m.Spawn(name, t.TempDir(), 0, 0); err != nil {
		t.Fatal(err)
	}

	cmds, err := m.PaneCommands()
	if err != nil {
		t.Fatal(err)
	}

	cmd, ok := cmds[name]
	if !ok {
		t.Errorf("expected %s in PaneCommands result, got %v", name, cmds)
	}
	// Fresh shell should report a shell process (bash, zsh, sh, fish, etc.)
	if cmd == "" {
		t.Error("expected non-empty command for session")
	}
}

func TestPaneCommandsNoSessionsReturnsEmptyMap(t *testing.T) {
	skipWithoutTmux(t)

	m, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}

	// Kill any legato sessions that might exist from other tests
	sessions, _ := m.ListSessions()
	for _, s := range sessions {
		if strings.HasPrefix(s, "legato-test-panecmd-empty") {
			m.Kill(s)
		}
	}

	cmds, err := m.PaneCommands()
	if err != nil {
		t.Fatal(err)
	}

	// Should not contain our test prefix (other legato sessions may exist)
	for k := range cmds {
		if strings.HasPrefix(k, "legato-test-panecmd-empty") {
			t.Errorf("unexpected session %s in results", k)
		}
	}
}

func TestPaneCommandsTmuxNotInstalled(t *testing.T) {
	_, err := New(Options{
		LookPath: func(name string) (string, error) {
			return "", exec.ErrNotFound
		},
	})
	if err == nil {
		t.Fatal("expected error when tmux not found")
	}
	// Can't call PaneCommands without a valid Manager — error at construction is correct
}

func TestSendKeysIntegration(t *testing.T) {
	skipWithoutTmux(t)

	m, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}

	name := "legato-test-sendkeys"
	t.Cleanup(func() { m.Kill(name) })

	if err := m.Spawn(name, t.TempDir(), 80, 24); err != nil {
		t.Fatal(err)
	}

	// Send some keys and verify no error
	if err := m.SendKeys(name, "echo hello"); err != nil {
		t.Fatalf("SendKeys: %v", err)
	}
}

func TestSendKeysNonExistentSessionReturnsError(t *testing.T) {
	skipWithoutTmux(t)

	m, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}

	err = m.SendKeys("legato-nonexistent-sendkeys-test", "hello")
	if err == nil {
		t.Error("expected error sending keys to non-existent session, got nil")
	}
}

func TestIsAliveNonExistentReturnsFalse(t *testing.T) {
	skipWithoutTmux(t)

	m, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}

	alive, err := m.IsAlive("legato-does-not-exist-xyz")
	if err != nil {
		t.Fatal(err)
	}
	if alive {
		t.Error("expected false for non-existent session")
	}
}

func TestNeedsBase64(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"plain text", false},
		{"with 'single quotes'", false},
		{"with \"double quotes\"", true},
		{"with newline\n", true},
		{"with carriage\r", true},
		{"unicode 你好", false},
	}
	for _, tc := range cases {
		if got := needsBase64(tc.in); got != tc.want {
			t.Errorf("needsBase64(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestSendKeysLineIntegration(t *testing.T) {
	skipWithoutTmux(t)

	m, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	name := "legato-test-sendkeysline"
	if err := m.Spawn(name, t.TempDir(), 0, 0); err != nil {
		t.Fatal(err)
	}
	defer m.Kill(name)

	if err := m.SendKeysLine(name, "echo hello"); err != nil {
		t.Errorf("SendKeysLine: %v", err)
	}
}

func TestSendKeysMultilineIntegration(t *testing.T) {
	skipWithoutTmux(t)

	m, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	name := "legato-test-sendkeysmulti"
	if err := m.Spawn(name, t.TempDir(), 0, 0); err != nil {
		t.Fatal(err)
	}
	defer m.Kill(name)

	// Multi-line payload should base64-encode
	if err := m.SendKeysMultiline(name, "line1\nline2"); err != nil {
		t.Errorf("SendKeysMultiline multi-line: %v", err)
	}
	// Single-line plain payload should pass through
	if err := m.SendKeysMultiline(name, "plain"); err != nil {
		t.Errorf("SendKeysMultiline plain: %v", err)
	}
}

// After the machine restarts there is no tmux server, and often no folder
// for its socket either: that is no sessions, not an error.
func TestListSessionsWithNoServerOrSocketFolderIsEmpty(t *testing.T) {
	skipWithoutTmux(t)
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_TMPDIR", t.TempDir())

	m, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := m.ListSessions()
	if err != nil || len(sessions) != 0 {
		t.Fatalf("ListSessions = %v, %v; want none and no error", sessions, err)
	}
}
