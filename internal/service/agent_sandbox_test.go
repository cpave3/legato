package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/sandbox"
	"github.com/cpave3/legato/internal/engine/store"
)

// fakeBackend renders a plan as one word per mount, so tests can read what
// the sandbox was given.
type fakeBackend struct {
	missing bool
	overlay bool
	plans   []sandbox.Plan
}

func (f *fakeBackend) Capabilities() sandbox.Capabilities {
	c := sandbox.Full()
	c.Overlay = f.overlay
	return c
}

func (f *fakeBackend) Name() string { return "fake" }
func (f *fakeBackend) Available() error {
	if f.missing {
		return errors.New("bwrap is not installed")
	}
	return nil
}

// Prefix is bwrap's, with one option.
func (f *fakeBackend) Prefix(plan sandbox.Plan, dir string) (string, error) {
	f.plans = append(f.plans, plan)
	if dir == "" {
		return "SANDBOX --ro-bind '/a path' '/a path' --", nil
	}
	return sandbox.ArgsFileLine("SANDBOX", []string{"--ro-bind", "/a path", "/a path"}, dir)
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
	files := strings.TrimPrefix(strings.Split(lines[0], " -- ")[0], "SANDBOX --args 3 3<")
	file, seccomp, ok := strings.Cut(files, " 4<")
	if !ok {
		t.Fatalf("no seccomp program on fd 4: %s", lines[0])
	}
	args, err := os.ReadFile(strings.Trim(file, "'"))
	if err != nil || string(args) != "--ro-bind\x00/a path\x00/a path\x00--seccomp\x004\x00" {
		t.Fatalf("args file %s = %q, %v", file, args, err)
	}
	// The program that keeps the agent from typing into the pane.
	if program, err := os.ReadFile(strings.Trim(seccomp, "'")); err != nil || len(program) == 0 || len(program)%8 != 0 {
		t.Fatalf("seccomp program %s: %d bytes, %v", seccomp, len(program), err)
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

func TestReloadingAnAgentWithoutTheSandboxTakesItOut(t *testing.T) {
	backend := &fakeBackend{}
	svc, s, mt := sandboxedService(t, backend)
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	running, _ := s.GetAgentSessionByTaskID(context.Background(), "task-1")
	execution, _ := s.GetExecution(context.Background(), running.ExecutionID)
	if _, err := s.RecordTaskNativeSession(context.Background(), "task-1", running.ID, running.ExecutionID, "claude-code", "conv-1", execution.WorkingDir, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	sandboxed, err := svc.(SandboxSwitcher).RestartAgentSwitchingSandbox(context.Background(), "task-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	session, _ := s.GetAgentSessionByTaskID(context.Background(), "task-1")
	if sandboxed || session.Sandboxed {
		t.Fatalf("the agent is still sandboxed (said %v, recorded %v)", sandboxed, session.Sandboxed)
	}
	lines := mt.sentLinesFor("legato-task-1")
	if last := lines[len(lines)-1]; strings.HasPrefix(last, "SANDBOX") {
		t.Fatalf("relaunched as %q", last)
	}
	if note := sentLineEventually(t, mt, "task-1", "[legato sandbox] "); !strings.Contains(note, "outside Legato's sandbox") {
		t.Fatalf("note = %q, want it to say the agent now runs outside the sandbox", note)
	}
}

func TestReloadingAnUnsandboxedAgentSwitchingTheSandboxPutsItBack(t *testing.T) {
	backend := &fakeBackend{}
	svc, s, mt := sandboxedService(t, backend)
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Unsandboxed: true}); err != nil {
		t.Fatal(err)
	}
	running, _ := s.GetAgentSessionByTaskID(context.Background(), "task-1")
	execution, _ := s.GetExecution(context.Background(), running.ExecutionID)
	if _, err := s.RecordTaskNativeSession(context.Background(), "task-1", running.ID, running.ExecutionID, "claude-code", "conv-1", execution.WorkingDir, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	sandboxed, err := svc.(SandboxSwitcher).RestartAgentSwitchingSandbox(context.Background(), "task-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	session, _ := s.GetAgentSessionByTaskID(context.Background(), "task-1")
	if !sandboxed || !session.Sandboxed {
		t.Fatalf("the agent is not back in the sandbox (said %v, recorded %v)", sandboxed, session.Sandboxed)
	}
	lines := mt.sentLinesFor("legato-task-1")
	if last := lines[len(lines)-1]; !strings.HasPrefix(last, "SANDBOX") {
		t.Fatalf("relaunched as %q", last)
	}
	if note := sentLineEventually(t, mt, "task-1", "[legato sandbox] "); !strings.Contains(note, "inside Legato's sandbox") {
		t.Fatalf("note = %q, want it to say the agent now runs inside the sandbox", note)
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

func TestASandboxWithAnAzureCredentialPutsLegatosAzWrapperOverEveryRealAz(t *testing.T) {
	cellar, bin := t.TempDir(), t.TempDir()
	realAz := filepath.Join(cellar, "az")
	_ = os.WriteFile(realAz, []byte("#!/bin/sh\n"), 0o755)
	_ = os.Symlink(realAz, filepath.Join(bin, "az"))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AZURE_CONFIG_DIR", "/var/home/me/.azure")

	backend := &fakeBackend{}
	svc, s, _ := sandboxedService(t, backend, func(settings *SandboxSettings) {
		settings.Base.BinaryPath = "/opt/legato/legato"
		settings.Base.Azure = testAzure
	})
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	plan := backend.plans[0]
	realMount := plan.Setenv["LEGATO_AZ_REAL"]
	if realMount == "" {
		t.Fatal("LEGATO_AZ_REAL is not set")
	}
	var wrapper string
	var kept bool
	for _, m := range plan.Mounts {
		if m.Dest == realAz {
			wrapper = m.Path
		}
		kept = kept || (m.Dest == realMount && m.Path == realAz)
	}
	if wrapper == "" || !kept {
		t.Fatalf("wrapper=%q kept=%v mounts=%+v", wrapper, kept, plan.Mounts)
	}
	script, err := os.ReadFile(wrapper)
	if err != nil || !strings.Contains(string(script), "'/opt/legato/legato' credential exec 'azure-reader'") || !strings.Contains(string(script), testAzure.TenantID) {
		t.Fatalf("the wrapper over az: %v\n%s", err, script)
	}
	// az keeps its configuration in the sandbox's own home, not a folder of
	// the person's the session inherited the name of.
	if !slices.Contains(plan.Unsetenv, "AZURE_CONFIG_DIR") {
		t.Errorf("AZURE_CONFIG_DIR is kept: %v", plan.Unsetenv)
	}
}

// Without an azure credential, or outside the sandbox, az is untouched.
func TestWithoutAnAzureCredentialOrASandboxAzIsTheRealOne(t *testing.T) {
	bin := t.TempDir()
	_ = os.WriteFile(filepath.Join(bin, "az"), []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	backend := &fakeBackend{}
	svc, s, mt := sandboxedService(t, backend)
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	if real := backend.plans[0].Setenv["LEGATO_AZ_REAL"]; real != "" {
		t.Fatalf("no azure credential: LEGATO_AZ_REAL = %q", real)
	}

	backend = &fakeBackend{}
	svc, s, mt = sandboxedService(t, backend, func(settings *SandboxSettings) { settings.Base.Azure = testAzure })
	createTask(t, s, "task-2")
	if err := svc.SpawnAgent(context.Background(), "task-2", 0, 0, AgentSpawnOptions{Unsandboxed: true}); err != nil {
		t.Fatal(err)
	}
	if len(backend.plans) != 0 {
		t.Fatalf("an unsandboxed agent got a plan: %+v", backend.plans)
	}
	if env := mt.envVarsFor("legato-task-2"); env["LEGATO_AZ_REAL"] != "" || strings.Contains(env["PATH"], "az") {
		t.Fatalf("outside the sandbox: %v", env)
	}
}

// Outside the sandbox the session is still signed out of the person's gh
// login, so its gh goes to Legato's wrapper by default.
func TestAnAgentOutsideTheSandboxIsSignedOutOfThePersonsGHLogin(t *testing.T) {
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
	if outside["GH_TOKEN"] != GHSignedOut || outside["GH_CONFIG_DIR"] != isolation["GH_CONFIG_DIR"] {
		t.Errorf("outside the sandbox: GH_TOKEN = %q, GH_CONFIG_DIR = %q", outside["GH_TOKEN"], outside["GH_CONFIG_DIR"])
	}
	inside := mt.envVarsFor("legato-task-2")
	if inside["GH_TOKEN"] != GHSignedOut || inside["GH_CONFIG_DIR"] == "" {
		t.Errorf("inside the sandbox: %v", inside)
	}
}

// A sandboxed agent works in the job clone made for its task, which is in
// Legato's data folder, also when it is started again in it; another
// task's agent cannot be given it.
func TestASandboxedAgentWorksInItsTasksJobCloneAndNoOther(t *testing.T) {
	data := t.TempDir()
	backend := &fakeBackend{}
	svc, s, _ := sandboxedService(t, backend, func(settings *SandboxSettings) { settings.Base.DataDir = data })
	createTask(t, s, "task-1")
	createTask(t, s, "task-2")
	clone := filepath.Join(data, "jobs", "ex-old", "widget")
	if err := os.MkdirAll(clone, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := s.CreateJobClone(ctx, store.JobClone{ExecutionID: "ex-old", TaskID: "task-1", Repo: "acme/widget", Path: clone, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	if err := svc.SpawnAgent(ctx, "task-1", 0, 0, AgentSpawnOptions{WorkingDir: clone}); err != nil {
		t.Fatalf("the task's own job clone: %v", err)
	}
	if got := backend.plans[len(backend.plans)-1].Chdir; got != clone {
		t.Fatalf("the agent works in %s, want %s", got, clone)
	}
	if err := svc.SpawnAgent(ctx, "task-2", 0, 0, AgentSpawnOptions{WorkingDir: clone}); err == nil {
		t.Fatal("another task's agent was given the job clone")
	}
}

// A task that joined a job clone, for the next branch of a stack, works in
// it in the sandbox too.
func TestASandboxedAgentWorksInAJobCloneItsTaskJoined(t *testing.T) {
	data := t.TempDir()
	backend := &fakeBackend{}
	svc, s, _ := sandboxedService(t, backend, func(settings *SandboxSettings) { settings.Base.DataDir = data })
	createTask(t, s, "task-1")
	createTask(t, s, "task-2")
	clone := filepath.Join(data, "jobs", "ex-old", "widget")
	if err := os.MkdirAll(clone, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id, err := s.CreateJobClone(ctx, store.JobClone{ExecutionID: "ex-old", TaskID: "task-1", Repo: "acme/widget", Path: clone, CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AttachJobCloneTask(ctx, id, "task-2", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := svc.SpawnAgent(ctx, "task-2", 0, 0, AgentSpawnOptions{WorkingDir: clone}); err != nil {
		t.Fatalf("a job clone the task joined: %v", err)
	}
}

// linkedHome is a folder whose "home" is a link to "var/home", as on a host
// whose /home is var/home (Fedora Atomic), with "me" in it. It returns home
// as reached through the link and as it really is.
func linkedHome(t *testing.T) (linked, real string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real = filepath.Join(root, "var", "home", "me")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("var", "home"), filepath.Join(root, "home")); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "home", "me"), real
}

// A job clone is the task's however its path is spelled: Legato's data
// folder and the clone's record may name it through the link while the
// agent's working folder, read back from its pane, is where it really is,
// or the other way round. Another task's agent is still refused it.
func TestASandboxedAgentFindsItsJobCloneThroughALinkedHome(t *testing.T) {
	for _, tc := range []struct {
		name               string
		dataLinked, linked bool // data folder and clone record; working folder
	}{
		{"recorded through the link, resumed where it really is", true, false},
		{"recorded where it really is, resumed through the link", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			linked, real := linkedHome(t)
			homeFor := func(throughLink bool) string {
				if throughLink {
					return linked
				}
				return real
			}
			data := filepath.Join(homeFor(tc.dataLinked), ".local", "share", "legato")
			clone := filepath.Join(data, "jobs", "ex-old", "widget")
			if err := os.MkdirAll(clone, 0o700); err != nil {
				t.Fatal(err)
			}
			workDir := filepath.Join(homeFor(tc.linked), ".local", "share", "legato", "jobs", "ex-old", "widget")

			backend := &fakeBackend{}
			svc, s, _ := sandboxedService(t, backend, func(settings *SandboxSettings) { settings.Base.DataDir = data })
			createTask(t, s, "task-1")
			createTask(t, s, "task-2")
			ctx := context.Background()
			if _, err := s.CreateJobClone(ctx, store.JobClone{ExecutionID: "ex-old", TaskID: "task-1", Repo: "acme/widget", Path: clone, CreatedAt: time.Now()}); err != nil {
				t.Fatal(err)
			}

			if err := svc.SpawnAgent(ctx, "task-1", 0, 0, AgentSpawnOptions{WorkingDir: workDir}); err != nil {
				t.Fatalf("the task's own job clone: %v", err)
			}
			want := filepath.Join(real, ".local", "share", "legato", "jobs", "ex-old", "widget")
			if got := backend.plans[len(backend.plans)-1].Chdir; got != want {
				t.Fatalf("the agent works in %s, want %s", got, want)
			}
			if err := svc.SpawnAgent(ctx, "task-2", 0, 0, AgentSpawnOptions{WorkingDir: workDir}); err == nil {
				t.Fatal("another task's agent was given the job clone")
			}
		})
	}
}

// promptsOffAdapter is fakeAdapter whose tool can turn its prompts off.
type promptsOffAdapter struct{ *fakeAdapter }

func (promptsOffAdapter) PromptsOffArgs() []string { return []string{"--no-prompts", "a b"} }

func TestAnUnattendedAgentRunsInTheSandboxWithItsPromptsOff(t *testing.T) {
	backend := &fakeBackend{}
	svc, s, mt := sandboxedService(t, backend)
	createTask(t, s, "task-1")
	// A tool that cannot turn its prompts off is refused before anything starts.
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Unattended: true}); err == nil || !strings.Contains(err.Error(), "permission prompts") {
		t.Fatalf("err %v", err)
	}
	if lines := mt.sentLinesFor("legato-task-1"); len(lines) != 0 {
		t.Fatalf("launched %v", lines)
	}

	svc = NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{
		Adapter: promptsOffAdapter{&fakeAdapter{name: "claude-code", launchFn: func(map[string]string, string, string, string) string { return "fake-tool" }}},
		Sandbox: &SandboxSettings{Backend: backend, Base: SandboxPlanInput{Home: t.TempDir(), DefaultMounts: true}},
	})
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Unattended: true, Unsandboxed: true}); err != nil {
		t.Fatal(err)
	}
	if lines := mt.sentLinesFor("legato-task-1"); len(lines) == 0 || !strings.HasPrefix(lines[0], "SANDBOX") || !strings.HasSuffix(lines[0], " -- fake-tool --no-prompts 'a b'") {
		t.Fatalf("launch = %v", lines)
	}
}

func TestAnUnattendedAgentIsRefusedWithoutTheSandbox(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	mt := newMockTmux()
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{
		Adapter: promptsOffAdapter{&fakeAdapter{name: "claude-code", launchFn: func(map[string]string, string, string, string) string { return "fake-tool" }}},
	})
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Unattended: true}); err == nil || !strings.Contains(err.Error(), "sandbox") {
		t.Fatalf("err %v", err)
	}
	if lines := mt.sentLinesFor("legato-task-1"); len(lines) != 0 {
		t.Fatalf("launched %v", lines)
	}
}

func TestAThrowawayCheckoutFallsBackToReadOnlyWithoutOverlays(t *testing.T) {
	for _, overlay := range []bool{true, false} {
		backend := &fakeBackend{overlay: overlay}
		svc, s, _ := sandboxedService(t, backend)
		createTask(t, s, "task-1")
		dir := t.TempDir()
		if got := svc.(CheckoutOverlayProber).CheckoutOverlayAvailable(); got != overlay {
			t.Fatalf("overlay available = %v, want %v", got, overlay)
		}
		if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{WorkingDir: dir, ReadOnlyCheckout: true, ThrowawayCheckout: true}); err != nil {
			t.Fatal(err)
		}
		want := sandbox.ReadOnly
		if overlay {
			want = sandbox.Overlay
		}
		var kind sandbox.Kind = -1
		for _, m := range backend.plans[0].Mounts {
			if m.Path == dir {
				kind = m.Kind
			}
		}
		if kind != want {
			t.Fatalf("overlay %v: the checkout's mount kind = %v, want %v", overlay, kind, want)
		}
	}
}

// The person may paste an image long after the agent started. Its folder is
// made at the start, so it is in the sandbox from the first moment, read
// only and the task's own: a folder made later would never show inside.
func TestASandboxedAgentSeesItsTasksPastedImagesFolderFromTheStart(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	backend := &fakeBackend{}
	svc, s, _ := sandboxedService(t, backend)
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	dir := PastedImageDir("task-1")
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("pasted images folder: %v", err)
	}
	// A pasted image may be private: the folders on the way are the user's only.
	for _, d := range []string{dir, filepath.Dir(dir), filepath.Dir(filepath.Dir(dir))} {
		info, err := os.Stat(d)
		if err != nil {
			t.Errorf("%s: %v", d, err)
		} else if info.Mode().Perm() != 0o700 {
			t.Errorf("%s: mode=%v", d, info.Mode().Perm())
		}
	}
	if m, ok := mountOf(backend.plans[0], dir); !ok || m.Kind != sandbox.ReadOnly {
		t.Fatalf("mount of %s = %+v, %v", dir, m, ok)
	}
	for _, m := range backend.plans[0].Mounts {
		if m.Path == filepath.Dir(dir) || m.Path == filepath.Dir(filepath.Dir(dir)) {
			t.Errorf("every task's pasted images are mounted: %+v", m)
		}
	}
}

// The tool is told which sandbox it runs in (Claude Code and Codex turn
// their own off inside Seatbelt), and gets the login environment the
// runtime gives a tool that cannot reach its own login there.
func TestASandboxedToolKnowsItsBackendAndGetsItsLogin(t *testing.T) {
	backend := &fakeBackend{}
	var asked []string
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	mt := newMockTmux()
	var launchEnv map[string]string
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{
		Adapter: &fakeAdapter{name: "claude-code", launchFn: func(env map[string]string, _, _, _ string) string {
			launchEnv = env
			return "fake-tool"
		}},
		Sandbox: &SandboxSettings{Backend: backend, Base: SandboxPlanInput{Home: t.TempDir(), RuntimeDir: filepath.Join(t.TempDir(), "legato")},
			ToolLogin: func(_ context.Context, agentKind string) (map[string]string, error) {
				asked = append(asked, agentKind)
				return map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "sk-ant-oat-test"}, nil
			}},
	})
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	if launchEnv["LEGATO_SANDBOX_BACKEND"] != "fake" {
		t.Errorf("the adapter was not told the backend: %q", launchEnv["LEGATO_SANDBOX_BACKEND"])
	}
	if len(asked) != 1 || asked[0] != "claude-code" || len(backend.plans) != 1 || backend.plans[0].Setenv["CLAUDE_CODE_OAUTH_TOKEN"] != "sk-ant-oat-test" {
		t.Fatalf("asked %q, plans %+v", asked, backend.plans)
	}
	if mt.envVarsFor("legato-task-1")["CLAUDE_CODE_OAUTH_TOKEN"] != "" {
		t.Error("the login is in the pane's own environment, outside the sandbox")
	}
}

// A tool that cannot log in inside the sandbox is not started there: the
// spawn says why, before anything runs.
func TestASandboxedToolWithoutALoginIsNotStarted(t *testing.T) {
	backend := &fakeBackend{}
	svc, s, mt := sandboxedService(t, backend, func(settings *SandboxSettings) {
		settings.ToolLogin = func(context.Context, string) (map[string]string, error) {
			return nil, errors.New("no login for Claude Code in the sandbox")
		}
	})
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0); err == nil || !strings.Contains(err.Error(), "no login") {
		t.Fatalf("err %v", err)
	}
	if lines := mt.sentLinesFor("legato-task-1"); len(lines) != 0 {
		t.Fatalf("launched %v", lines)
	}
}
