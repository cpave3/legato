package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/sandbox"
	"github.com/cpave3/legato/internal/engine/store"
)

// scratchService is a sandboxed agent service whose scratch is in a folder
// the test can look in.
func scratchService(t *testing.T) (AgentService, *store.Store, *mockTmux, SandboxScratch) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "scratch")
	var scratch SandboxScratch
	svc, s, mt := sandboxedService(t, &fakeBackend{overlay: true}, func(settings *SandboxSettings) {
		settings.Base.ScratchDir = root
		scratch = SandboxScratch{Root: root, RuntimeDir: settings.Base.RuntimeDir}
	})
	return svc, s, mt, scratch
}

func executionOf(t *testing.T, s *store.Store, taskID string) string {
	t.Helper()
	session, err := s.GetAgentSessionByTaskID(context.Background(), taskID)
	if err != nil || session.ExecutionID == "" {
		t.Fatalf("session of %s: %+v, %v", taskID, session, err)
	}
	return session.ExecutionID
}

func assertPrivateDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		t.Fatalf("%s: %v", path, err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("%s has mode %o, want 700", path, perm)
	}
}

func assertGone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s is still there (%v)", path, err)
	}
}

func TestASandboxedSpawnMakesItsScratchFoldersPrivate(t *testing.T) {
	svc, s, _, scratch := scratchService(t)
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{WorkingDir: t.TempDir(), ReadOnlyCheckout: true, ThrowawayCheckout: true}); err != nil {
		t.Fatal(err)
	}
	ex := executionOf(t, s, "task-1")
	for _, dir := range []string{scratch.Dir(ex), scratch.Tmp(ex), scratch.Home(ex)} {
		assertPrivateDir(t, dir)
	}
	upper, work := scratch.overlay(ex, 0)
	assertPrivateDir(t, upper)
	assertPrivateDir(t, work)
}

func TestKillingAnAgentRemovesItsScratch(t *testing.T) {
	svc, s, _, scratch := scratchService(t)
	createTask(t, s, "task-1")
	createTask(t, s, "task-2")
	ctx := context.Background()
	for _, id := range []string{"task-1", "task-2"} {
		if err := svc.SpawnAgent(ctx, id, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	ended, running := executionOf(t, s, "task-1"), executionOf(t, s, "task-2")
	if err := os.WriteFile(filepath.Join(scratch.Tmp(ended), "build.out"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := svc.KillAgent(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}
	assertGone(t, scratch.Dir(ended))
	assertGone(t, scratch.runtime(ended))
	assertPrivateDir(t, scratch.Tmp(running))
	if _, err := os.Stat(scratch.runtime(running)); err != nil {
		t.Fatalf("the running agent's runtime folder: %v", err)
	}
}

func TestAnAgentWhoseSessionDiedHasItsScratchRemovedOnReconcile(t *testing.T) {
	svc, s, mt, scratch := scratchService(t)
	createTask(t, s, "task-1")
	createTask(t, s, "task-2")
	ctx := context.Background()
	for _, id := range []string{"task-1", "task-2"} {
		if err := svc.SpawnAgent(ctx, id, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	died, running := executionOf(t, s, "task-1"), executionOf(t, s, "task-2")
	_ = mt.Kill("legato-task-1")
	if err := svc.ReconcileSessions(ctx); err != nil {
		t.Fatal(err)
	}
	assertGone(t, scratch.Dir(died))
	assertPrivateDir(t, scratch.Home(running))
}

// Leftovers of executions that ended while Legato was not looking (a crash,
// a restart) are swept; a running execution's folder, and one this Legato
// does not know, are left alone.
func TestReconcileSweepsTheScratchOfEndedExecutionsOnly(t *testing.T) {
	svc, s, _, scratch := scratchService(t)
	createTask(t, s, "task-1")
	ctx := context.Background()
	if err := svc.SpawnAgent(ctx, "task-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	running := executionOf(t, s, "task-1")
	if err := s.CreateExecution(ctx, store.Execution{ID: "ex-old", Kind: "interactive", Status: store.ExecutionRunning, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.TerminalizeExecution(ctx, "ex-old", store.ExecutionCancelled); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{scratch.Tmp("ex-old"), scratch.runtime("ex-old") + "/tmp", scratch.Tmp("ex-another-legatos"), scratch.runtime("ex-another-legatos")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.ReconcileSessions(ctx); err != nil {
		t.Fatal(err)
	}
	assertGone(t, scratch.Dir("ex-old"))
	assertGone(t, scratch.runtime("ex-old"))
	assertPrivateDir(t, scratch.Tmp(running))
	for _, kept := range []string{scratch.Dir("ex-another-legatos"), scratch.runtime("ex-another-legatos")} {
		if _, err := os.Stat(kept); err != nil {
			t.Fatalf("%s: %v", kept, err)
		}
	}
}

// An agent can leave folders it cannot write (Go's module cache does); they
// are removed all the same.
func TestScratchWithUnwritableFoldersIsRemoved(t *testing.T) {
	scratch := SandboxScratch{Root: t.TempDir()}
	locked := filepath.Join(scratch.Home("ex-1"), "go", "pkg", "mod", "x@v1")
	if err := os.MkdirAll(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "go.mod"), nil, 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	if err := scratch.Remove("ex-1"); err != nil {
		t.Fatal(err)
	}
	assertGone(t, scratch.Dir("ex-1"))
}

func TestAnExecutionIDThatIsNotOneFolderRemovesNothing(t *testing.T) {
	scratch := SandboxScratch{Root: t.TempDir()}
	for _, id := range []string{"", ".", "..", "../x", "a/b"} {
		if err := scratch.Remove(id); err == nil {
			t.Errorf("Remove(%q) was accepted", id)
		}
	}
	if _, err := os.Stat(scratch.Root); err != nil {
		t.Fatal(err)
	}
}

func TestAFinishedWorkflowAgentHasItsScratchRemoved(t *testing.T) {
	svc, s, mt, scratch := scratchService(t)
	createTask(t, s, "task-1")
	ctx := context.Background()
	if err := svc.SpawnAgent(ctx, "task-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	session, err := s.GetAgentSessionByTaskID(ctx, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	ex := session.ExecutionID
	assertPrivateDir(t, scratch.Tmp(ex))
	id := session.ID
	if err := NewWorkflowAgentRunner(svc, s).FinishWorkflowAgent(ctx, store.WorkflowStepAttempt{AgentSessionID: &id, TmuxSession: session.TmuxSession}); err != nil {
		t.Fatal(err)
	}
	if alive, _ := mt.IsAlive(session.TmuxSession); alive {
		t.Fatal("the agent's pane is still alive")
	}
	assertGone(t, scratch.Dir(ex))
}

// A reconcile that runs while a spawn is in flight (its execution recorded,
// its scratch made, its session not yet linked) neither ends the execution
// nor removes its scratch.
func TestAReconcileDuringASpawnLeavesItsScratch(t *testing.T) {
	svc, s, mt, scratch := scratchService(t)
	createTask(t, s, "task-1")
	ctx := context.Background()
	reconciled := false
	mt.onSpawn = func(string) {
		reconciled = true
		if err := svc.ReconcileSessions(ctx); err != nil {
			t.Errorf("reconcile: %v", err)
		}
	}
	if err := svc.SpawnAgent(ctx, "task-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	if !reconciled {
		t.Fatal("the reconcile did not run during the spawn")
	}
	ex := executionOf(t, s, "task-1")
	execution, err := s.GetExecution(ctx, ex)
	if err != nil || execution.Status != store.ExecutionRunning {
		t.Fatalf("execution = %+v, %v", execution, err)
	}
	assertPrivateDir(t, scratch.Tmp(ex))
	assertPrivateDir(t, scratch.Home(ex))
}

// The sweep trusts a running agent session over its execution's status.
func TestTheSweepKeepsTheScratchOfAnAgentStillRunning(t *testing.T) {
	svc, s, _, scratch := scratchService(t)
	createTask(t, s, "task-1")
	ctx := context.Background()
	if err := svc.SpawnAgent(ctx, "task-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	ex := executionOf(t, s, "task-1")
	if err := s.TerminalizeExecution(ctx, ex, store.ExecutionCancelled); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReconcileSessions(ctx); err != nil {
		t.Fatal(err)
	}
	assertPrivateDir(t, scratch.Tmp(ex))
}

// The cover works on the first spawn too, before anything made the folder.
func TestTheFirstSpawnHidesAScratchFolderInsideAMount(t *testing.T) {
	big := t.TempDir()
	root := filepath.Join(big, "legato-scratch")
	backend := &fakeBackend{}
	svc, s, _ := sandboxedService(t, backend, func(settings *SandboxSettings) {
		settings.Base.ScratchDir = root
		settings.Base.Extra = []SandboxPath{{Path: big, Access: "write"}}
	})
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	m, ok := mountOf(backend.plans[0], root)
	if !ok || m.Kind != sandbox.HideDir {
		t.Fatalf("scratch = %+v (%v)", m, ok)
	}
}
