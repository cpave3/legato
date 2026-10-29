package service

import (
	"context"
	"errors"
	"os"
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
	return append([]string{"SANDBOX", "--ro-bind", "/a path", "/a path", "--"}, command...), nil
}

func sandboxedService(t *testing.T, backend *fakeBackend, adjust ...func(*SandboxSettings)) (AgentService, *store.Store, *mockTmux) {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	mt := newMockTmux()
	home := t.TempDir()
	settings := &SandboxSettings{Backend: backend, Base: SandboxPlanInput{Home: home, RuntimeDir: filepath.Join(t.TempDir(), "legato"), DefaultMounts: true}}
	for _, f := range adjust {
		f(settings)
	}
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{
		Adapter: &fakeAdapter{name: "claude-code", launchFn: func(env map[string]string, _, _, _ string) string {
			if env["LEGATO_SANDBOXED"] == "1" {
				return "fake-tool --in-sandbox"
			}
			return "fake-tool"
		}},
		Sandbox: settings,
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
	// The sandbox's arguments are read from a file, so the typed line stays
	// short however many mounts there are.
	lines := mt.sentLinesFor("legato-task-1")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "SANDBOX --args 3 3<") || !strings.Contains(lines[0], "bwrap.args") || !strings.Contains(lines[0], " -- fake-tool --in-sandbox") {
		t.Fatalf("launch = %v", lines)
	}
	file := strings.TrimPrefix(strings.Split(lines[0], " -- ")[0], "SANDBOX --args 3 3<")
	args, err := os.ReadFile(strings.Trim(file, "'"))
	if err != nil || string(args) != "--ro-bind\x00/a path\x00/a path\x00" {
		t.Fatalf("args file %s = %q, %v", file, args, err)
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

func TestReloadingAnAgentKeepsItsSandboxChoice(t *testing.T) {
	backend := &fakeBackend{}
	svc, s, mt := sandboxedService(t, backend)
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Unsandboxed: true}); err != nil {
		t.Fatal(err)
	}
	// A conversation to resume, as a Claude Code session reports.
	running, _ := s.GetAgentSessionByTaskID(context.Background(), "task-1")
	execution, _ := s.GetExecution(context.Background(), running.ExecutionID)
	if _, err := s.RecordTaskNativeSession(context.Background(), "task-1", running.ID, running.ExecutionID, "claude-code", "conv-1", execution.WorkingDir, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := svc.(AgentRestarter).RestartAgent(context.Background(), "task-1", nil); err != nil {
		t.Fatal(err)
	}
	session, _ := s.GetAgentSessionByTaskID(context.Background(), "task-1")
	if session.Sandboxed {
		t.Fatal("reloading moved an unsandboxed agent into the sandbox")
	}
	lines := mt.sentLinesFor("legato-task-1")
	if strings.HasPrefix(lines[len(lines)-1], "SANDBOX") {
		t.Fatalf("relaunched as %q", lines[len(lines)-1])
	}
}

func mountOf(plan sandbox.Plan, path string) (sandbox.Mount, bool) {
	for _, m := range plan.Mounts {
		if m.Path == path && m.Dest == "" {
			return m, true
		}
	}
	return sandbox.Mount{}, false
}

func TestAToolsStateFolderSetInTheEnvironmentIsMounted(t *testing.T) {
	state := filepath.Join(t.TempDir(), "claude-elsewhere")
	t.Setenv("CLAUDE_CONFIG_DIR", state)
	backend := &fakeBackend{}
	svc, s, _ := sandboxedService(t, backend)
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	// The folder is made when it is missing, so what the tool writes there
	// lands on the host, not in the sandbox's empty home.
	if info, err := os.Stat(state); err != nil || !info.IsDir() {
		t.Fatalf("state folder: %v", err)
	}
	if m, ok := mountOf(backend.plans[0], state); !ok || m.Kind != sandbox.ReadWrite {
		t.Fatalf("mount of %s = %+v, %v", state, m, ok)
	}
}

func TestAResumedSessionsStorageFolderIsMounted(t *testing.T) {
	backend := &fakeBackend{}
	svc, s, _ := sandboxedService(t, backend)
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	running, _ := s.GetAgentSessionByTaskID(context.Background(), "task-1")
	execution, _ := s.GetExecution(context.Background(), running.ExecutionID)
	storage := t.TempDir()
	if _, err := s.RecordTaskNativeSession(context.Background(), "task-1", running.ID, running.ExecutionID, "claude-code", "conv-1", execution.WorkingDir, storage); err != nil {
		t.Fatal(err)
	}
	if err := svc.(AgentRestarter).RestartAgent(context.Background(), "task-1", nil); err != nil {
		t.Fatal(err)
	}
	if m, ok := mountOf(backend.plans[len(backend.plans)-1], storage); !ok || m.Kind != sandbox.ReadWrite {
		t.Fatalf("mount of %s = %+v, %v", storage, m, ok)
	}
}

type fixedSessionEnv map[string]string

func (f fixedSessionEnv) SessionEnv(context.Context, string, string) (map[string]string, error) {
	return f, nil
}

func TestASandboxWithLegatosGHWrapperCoversEveryRealGH(t *testing.T) {
	cellar, bin := t.TempDir(), t.TempDir()
	realGH := filepath.Join(cellar, "gh")
	_ = os.WriteFile(realGH, []byte("#!/bin/sh\n"), 0o755)
	_ = os.Symlink(realGH, filepath.Join(bin, "gh"))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	wrapperDir := filepath.Join(t.TempDir(), "legato-bin")
	backend := &fakeBackend{}
	svc, s, _ := sandboxedService(t, backend, func(settings *SandboxSettings) { settings.Base.CredentialWrapperDir = wrapperDir })
	_ = os.MkdirAll(wrapperDir, 0o700)
	_ = os.WriteFile(filepath.Join(wrapperDir, "gh"), []byte("#!/bin/sh\n"), 0o700)
	svc.(interface{ SetSessionEnv(SessionEnvProvider) }).SetSessionEnv(fixedSessionEnv{"PATH": wrapperDir + string(os.PathListSeparator) + os.Getenv("PATH"), "GH_TOKEN": GHSignedOut})
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	plan := backend.plans[0]
	realMount := plan.Setenv["LEGATO_GH_REAL"]
	if realMount == "" {
		t.Fatal("LEGATO_GH_REAL is not set")
	}
	var covered, kept bool
	for _, m := range plan.Mounts {
		covered = covered || (m.Dest == realGH && m.Path == filepath.Join(wrapperDir, "gh"))
		kept = kept || (m.Dest == realMount && m.Path == realGH)
	}
	if !covered || !kept {
		t.Fatalf("covered=%v kept=%v mounts=%+v", covered, kept, plan.Mounts)
	}
}

func TestAnAgentOutsideTheSandboxKeepsThePersonsGHLogin(t *testing.T) {
	backend := &fakeBackend{}
	svc, s, mt := sandboxedService(t, backend)
	isolation := fixedSessionEnv{"GH_TOKEN": GHSignedOut, "GH_CONFIG_DIR": filepath.Join(t.TempDir(), "gh-config")}
	svc.(interface{ SetSessionEnv(SessionEnvProvider) }).SetSessionEnv(isolation)
	createTask(t, s, "task-1")
	createTask(t, s, "task-2")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Unsandboxed: true}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SpawnAgent(context.Background(), "task-2", 0, 0); err != nil {
		t.Fatal(err)
	}
	outside := mt.envVarsFor("legato-task-1")
	if _, ok := outside["GH_TOKEN"]; ok {
		t.Errorf("outside the sandbox GH_TOKEN = %q, want the person's own", outside["GH_TOKEN"])
	}
	if _, ok := outside["GH_CONFIG_DIR"]; ok {
		t.Errorf("outside the sandbox GH_CONFIG_DIR = %q, want the person's own", outside["GH_CONFIG_DIR"])
	}
	inside := mt.envVarsFor("legato-task-2")
	if inside["GH_TOKEN"] != GHSignedOut || inside["GH_CONFIG_DIR"] == "" {
		t.Errorf("inside the sandbox: %v", inside)
	}
}
