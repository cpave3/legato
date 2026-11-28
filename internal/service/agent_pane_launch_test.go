package service

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// LEG-689: an agent's launch is the pane's own process, never a line typed
// into the shell the session started with.
func TestSpawnAgentLaunchesAsThePanesProcess(t *testing.T) {
	t.Setenv("SHELL", "/usr/bin/zsh")
	s := newLaunchTestStore(t)
	mt := newMockTmux()
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{Adapter: &fakeAdapter{
		name:     "fake",
		launchFn: func(map[string]string, string, string, string) string { return "fake-tool --go" },
	}})
	createTask(t, s, "pane-1")
	if err := svc.SpawnAgent(context.Background(), "pane-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	launches := mt.paneLaunchesFor("legato-pane-1")
	want := []string{"/usr/bin/zsh", "-i", "-c", "fake-tool --go; exec /usr/bin/zsh -l"}
	if len(launches) != 1 || !reflect.DeepEqual(launches[0], want) {
		t.Fatalf("pane launches = %q, want one %q", launches, want)
	}
	if raw := mt.sentRawFor("legato-pane-1"); len(raw) != 0 {
		t.Fatalf("the launch was typed: %q", raw)
	}
}

func TestPaneLaunchUsesAPOSIXShellForOtherShells(t *testing.T) {
	for _, tc := range []struct {
		shell string
		want  []string
	}{
		{"/bin/bash", []string{"/bin/bash", "-i", "-c", "run; exec /bin/bash -l"}},
		{"/usr/bin/fish", []string{"/bin/sh", "-c", "run; exec /usr/bin/fish -l"}},
		{"", []string{"/bin/sh", "-c", "run; exec /bin/sh -l"}},
	} {
		if got := paneLaunch(tc.shell, "run"); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("paneLaunch(%q) = %q, want %q", tc.shell, got, tc.want)
		}
	}
}

// A launch tmux refuses is an error, not a silent shell: the session goes,
// the execution fails and the spawn says why, so a workflow step that
// started it fails and can be retried.
func TestSpawnAgentFailsWhenItsLaunchFails(t *testing.T) {
	s := newLaunchTestStore(t)
	mt := newMockTmux()
	mt.runInPaneErr = errors.New("tmux respawn-pane: no server")
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{Adapter: &fakeAdapter{
		name:     "fake",
		launchFn: func(map[string]string, string, string, string) string { return "fake-tool" },
	}})
	createTask(t, s, "pane-2")
	err := svc.SpawnAgent(context.Background(), "pane-2", 0, 0)
	if err == nil || !strings.Contains(err.Error(), "no server") {
		t.Fatalf("SpawnAgent = %v, want the launch's error", err)
	}
	if mt.sessionAlive("legato-pane-2") {
		t.Fatal("the session of a failed launch was left running")
	}
	agents, err := svc.ListAgents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range agents {
		if a.TaskID == "pane-2" && a.Status == "running" {
			t.Fatalf("a failed launch is listed as running: %+v", a)
		}
	}
}

// A second launch for a task kills the first session before making its own.
func TestSpawnAgentKillsTheOldSessionBeforeANewLaunch(t *testing.T) {
	s := newLaunchTestStore(t)
	mt := newMockTmux()
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{Adapter: &fakeAdapter{
		name:     "fake",
		launchFn: func(map[string]string, string, string, string) string { return "fake-tool" },
	}})
	createTask(t, s, "pane-3")
	ctx := context.Background()
	if err := svc.SpawnAgent(ctx, "pane-3", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := svc.KillAgent(ctx, "pane-3"); err != nil {
		t.Fatal(err)
	}
	mt.mu.Lock()
	mt.sessions["legato-pane-3"] = true // a shell left behind
	mt.mu.Unlock()
	var killedFirst bool
	mt.onSpawn = func(string) { killedFirst = !mt.sessionAlive("legato-pane-3") }
	if err := svc.SpawnAgent(ctx, "pane-3", 0, 0); err != nil {
		t.Fatal(err)
	}
	if !killedFirst {
		t.Fatal("the new launch was made beside the old session")
	}
	if n := len(mt.paneLaunchesFor("legato-pane-3")); n != 2 {
		t.Fatalf("pane launches = %d, want 2 (one per launch)", n)
	}
}

func newLaunchTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
