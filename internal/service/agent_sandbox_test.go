package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/sandbox"
	"github.com/cpave3/legato/internal/engine/store"
)

// fakeBackend renders a plan as one word per mount, so tests can read what
// the sandbox was given.
type fakeBackend struct {
	missing bool
	plans   []sandbox.Plan
}

func (f *fakeBackend) Name() string { return "fake" }
func (f *fakeBackend) Available() error {
	if f.missing {
		return errors.New("bwrap is not installed")
	}
	return nil
}
func (f *fakeBackend) Wrap(plan sandbox.Plan, command []string) ([]string, error) {
	f.plans = append(f.plans, plan)
	return append([]string{"SANDBOX", "--"}, command...), nil
}

func sandboxedService(t *testing.T, backend *fakeBackend) (AgentService, *store.Store, *mockTmux) {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	mt := newMockTmux()
	home := t.TempDir()
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{
		Adapter: &fakeAdapter{name: "claude-code", launchFn: func(env map[string]string, _, _, _ string) string {
			if env["LEGATO_SANDBOXED"] == "1" {
				return "fake-tool --in-sandbox"
			}
			return "fake-tool"
		}},
		Sandbox: &SandboxSettings{Backend: backend, Base: SandboxPlanInput{Home: home, RuntimeDir: filepath.Join(t.TempDir(), "legato"), DefaultMounts: true}},
	})
	return svc, s, mt
}

func TestASandboxedSpawnWrapsTheLaunchCommand(t *testing.T) {
	backend := &fakeBackend{}
	svc, s, mt := sandboxedService(t, backend)
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	lines := mt.sentLinesFor("legato-task-1")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "SANDBOX -- fake-tool --in-sandbox") {
		t.Fatalf("launch = %v", lines)
	}
	// The pane's own shell is the person's: it is not told it is sandboxed.
	if mt.envVarsFor("legato-task-1")["LEGATO_SANDBOXED"] != "" {
		t.Fatal("the host shell is marked sandboxed")
	}
	if len(backend.plans) != 1 || backend.plans[0].Setenv["LEGATO_SANDBOXED"] != "1" {
		t.Fatalf("plans = %+v", backend.plans)
	}
	session, _ := s.GetAgentSessionByTaskID(context.Background(), "task-1")
	if !session.Sandboxed {
		t.Fatal("the session is not recorded as sandboxed")
	}
}

func TestAnAgentCanBeSpawnedWithoutTheSandbox(t *testing.T) {
	backend := &fakeBackend{}
	svc, s, mt := sandboxedService(t, backend)
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Unsandboxed: true}); err != nil {
		t.Fatal(err)
	}
	if lines := mt.sentLinesFor("legato-task-1"); len(lines) == 0 || lines[0] != "fake-tool" {
		t.Fatalf("launch = %v", lines)
	}
	session, _ := s.GetAgentSessionByTaskID(context.Background(), "task-1")
	if session.Sandboxed {
		t.Fatal("an unsandboxed session is recorded as sandboxed")
	}
}

func TestASwarmWorkerCannotLeaveTheSandbox(t *testing.T) {
	backend := &fakeBackend{}
	svc, s, mt := sandboxedService(t, backend)
	createTask(t, s, "parent")
	if err := svc.SpawnAgent(context.Background(), "st-1", 0, 0, AgentSpawnOptions{Unsandboxed: true, ParentTaskID: "parent", SubtaskID: "st-1", Role: "worker"}); err != nil {
		t.Fatal(err)
	}
	if lines := mt.sentLinesFor("legato-st-1"); len(lines) == 0 || !strings.HasPrefix(lines[0], "SANDBOX") {
		t.Fatalf("launch = %v", lines)
	}
}

func TestASandboxedSpawnWithoutBwrapStopsBeforeTmux(t *testing.T) {
	svc, s, mt := sandboxedService(t, &fakeBackend{missing: true})
	createTask(t, s, "task-1")
	err := svc.SpawnAgent(context.Background(), "task-1", 0, 0)
	if err == nil || !strings.Contains(err.Error(), "bwrap") {
		t.Fatalf("err = %v", err)
	}
	if mt.sessions["legato-task-1"] {
		t.Fatal("a tmux session was made")
	}
}
