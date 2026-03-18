package tmux

import (
	"os/exec"
	"strings"
	"testing"
)

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

func TestSpawnAndKillIntegration(t *testing.T) {
	skipWithoutTmux(t)

	m, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}

	name := "legato-test-spawn"
	t.Cleanup(func() { m.Kill(name) })

	if err := m.Spawn(name, t.TempDir()); err != nil {
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

	if err := m.Spawn(name, t.TempDir()); err != nil {
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

	if err := m.Spawn(name1, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := m.Spawn(name2, t.TempDir()); err != nil {
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
