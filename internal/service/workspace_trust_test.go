package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// trustingAdapter records the folders it is asked to trust, and how many
// lines had been typed into the session by then.
type trustingAdapter struct {
	fakeAdapter
	tmux    *mockTmux
	session string
	err     error
	trusted []trustCall
}

type trustCall struct {
	dir         string
	env         map[string]string
	linesBefore int
}

func (f *trustingAdapter) TrustWorkDir(env map[string]string, dir string) error {
	f.trusted = append(f.trusted, trustCall{dir: dir, env: env, linesBefore: len(f.tmux.sentLinesFor(f.session))})
	return f.err
}

func spawnWithTrustingAdapter(t *testing.T, trustErr error) (*trustingAdapter, *mockTmux, string) {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	workDir := t.TempDir()
	mt := newMockTmux()
	adapter := &trustingAdapter{
		fakeAdapter: fakeAdapter{name: "fake", launchFn: func(map[string]string, string, string, string) string { return "fake-tool" }},
		tmux:        mt,
		session:     "legato-task-1",
		err:         trustErr,
	}
	svc := NewAgentService(s, mt, workDir, AgentServiceOptions{Adapter: adapter})
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	return adapter, mt, workDir
}

func TestSpawnAgent_TrustsTheWorkDirBeforeLaunching(t *testing.T) {
	adapter, mt, workDir := spawnWithTrustingAdapter(t, nil)

	if len(adapter.trusted) != 1 {
		t.Fatalf("trusted %d times, want once: %+v", len(adapter.trusted), adapter.trusted)
	}
	call := adapter.trusted[0]
	if call.dir != workDir {
		t.Errorf("trusted %q, want %q", call.dir, workDir)
	}
	if call.env["LEGATO_TASK_ID"] != "task-1" {
		t.Errorf("trust not given the session's environment: %v", call.env)
	}
	if call.linesBefore != 0 {
		t.Errorf("trusted after %d lines were typed; want before the launch command", call.linesBefore)
	}
	if lines := mt.sentLinesFor("legato-task-1"); len(lines) == 0 || lines[0] != "fake-tool" {
		t.Errorf("launch lines = %v", lines)
	}
}

func TestSpawnAgent_LaunchesWhenTrustFails(t *testing.T) {
	// The tool then asks the person, as it did before.
	_, mt, _ := spawnWithTrustingAdapter(t, errors.New("config locked"))

	if lines := mt.sentLinesFor("legato-task-1"); len(lines) == 0 || lines[0] != "fake-tool" {
		t.Errorf("launch lines = %v", lines)
	}
}
