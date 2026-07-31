package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	engineworktree "github.com/cpave3/legato/internal/engine/worktree"
)

type fakeCheckoutInspector struct {
	context *store.CodeContext
	err     error
	dir     string
}

func (f *fakeCheckoutInspector) InspectCodeContext(directory string) (*store.CodeContext, error) {
	f.dir = directory
	return f.context, f.err
}

type fakeWorktreeRunner struct {
	path        string
	createErr   error
	removeErr   error
	removedFrom string
	removed     string
	force       bool
	createCalls int
}

func (f *fakeWorktreeRunner) Create(context.Context, string, string, string, string) (string, error) {
	f.createCalls++
	return f.path, f.createErr
}
func (f *fakeWorktreeRunner) Remove(_ context.Context, primaryDir, branch string, force bool) error {
	f.removedFrom, f.removed, f.force = primaryDir, branch, force
	return f.removeErr
}

func TestWorktreeWorkflowCreatePersistsMetadata(t *testing.T) {
	s := newServiceTestStore(t)
	createServiceTestTask(t, s, "task-1")
	runner := &fakeWorktreeRunner{path: "/projects/legato.feature"}
	workflow := NewWorktreeWorkflow(s, runner)

	got, err := workflow.Create(context.Background(), "task-1", "/projects/legato", "feature", "main")
	if err != nil {
		t.Fatal(err)
	}
	want := store.TaskWorktree{PrimaryDir: "/projects/legato", Path: runner.path, Branch: "feature", BaseBranch: "main"}
	if *got != want {
		t.Fatalf("worktree = %#v, want %#v", got, want)
	}
	task, err := s.GetTask(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.WorktreePath == nil || *task.WorktreePath != runner.path || task.WorktreeBranch == nil || *task.WorktreeBranch != "feature" {
		t.Fatalf("task metadata = %#v", task)
	}
}

func TestWorktreeWorkflowCreateRejectsExistingWorktreeBeforeRunner(t *testing.T) {
	s := newServiceTestStore(t)
	createServiceTestTask(t, s, "task-1")
	if err := s.SetTaskWorktree(context.Background(), "task-1", &store.TaskWorktree{PrimaryDir: "/repo", Path: "/existing", Branch: "old", BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	runner := &fakeWorktreeRunner{path: "/new"}
	_, err := NewWorktreeWorkflow(s, runner).Create(context.Background(), "task-1", "/repo", "new", "main")
	if err == nil || runner.createCalls != 0 {
		t.Fatalf("error=%v create calls=%d", err, runner.createCalls)
	}
}

func TestAgentCaptureUsesExactWorkingDirectoryAndPreservesManualContext(t *testing.T) {
	s := newServiceTestStore(t)
	createServiceTestTask(t, s, "task-1")
	inspector := &fakeCheckoutInspector{context: &store.CodeContext{Repo: "cpave3/legato", Branch: "feature/task-1", HeadSHA: "abc123"}}
	agent := &agentService{store: s, checkoutInspector: inspector}
	agent.captureAgentCodeContext("task-1", "/projects/legato.feature")
	task, _ := s.GetTask(context.Background(), "task-1")
	captured, _ := store.ParseCodeContext(task.CodeContext)
	if inspector.dir != "/projects/legato.feature" || captured == nil || captured.RepoSource != "agent" || captured.WorkingDir != inspector.dir {
		t.Fatalf("directory=%q context=%#v", inspector.dir, captured)
	}

	manual := &store.CodeContext{Repo: "other/repo", Branch: "manual", RepoSource: "manual", BranchSource: "manual"}
	if err := s.SetCodeContext(context.Background(), "task-1", manual); err != nil {
		t.Fatal(err)
	}
	agent.captureAgentCodeContext("task-1", "/projects/legato.feature")
	task, _ = s.GetTask(context.Background(), "task-1")
	preserved, _ := store.ParseCodeContext(task.CodeContext)
	if preserved == nil || preserved.Repo != "other/repo" || preserved.Branch != "manual" {
		t.Fatalf("manual context overwritten: %#v", preserved)
	}
}

func TestWorktreeWorkflowCreateCapturesCheckoutCodeContext(t *testing.T) {
	s := newServiceTestStore(t)
	createServiceTestTask(t, s, "task-1")
	runner := &fakeWorktreeRunner{path: "/projects/legato.feature"}
	inspector := &fakeCheckoutInspector{context: &store.CodeContext{Repo: "cpave3/legato", Branch: "feature", HeadSHA: "abc123"}}
	workflow := NewWorktreeWorkflow(s, runner, inspector)

	if _, err := workflow.Create(context.Background(), "task-1", "/projects/legato", "feature", "main"); err != nil {
		t.Fatal(err)
	}
	task, _ := s.GetTask(context.Background(), "task-1")
	codeContext, _ := store.ParseCodeContext(task.CodeContext)
	if inspector.dir != runner.path || codeContext == nil || codeContext.Repo != "cpave3/legato" || codeContext.WorkingDir != runner.path || codeContext.BaseBranch != "main" || codeContext.RepoSource != "worktree" {
		t.Fatalf("directory=%q context=%#v", inspector.dir, codeContext)
	}
}

func TestWorktreeWorkflowCreateMissingTaskDoesNotInvokeRunner(t *testing.T) {
	s := newServiceTestStore(t)
	runner := &fakeWorktreeRunner{path: "/projects/legato.feature"}

	_, err := NewWorktreeWorkflow(s, runner).Create(context.Background(), "missing", "/projects/legato", "feature", "main")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if runner.createCalls != 0 {
		t.Fatalf("create calls = %d, want 0", runner.createCalls)
	}
}

func TestWorktreeWorkflowRemoveClearsMetadata(t *testing.T) {
	s := newServiceTestStore(t)
	createServiceTestTask(t, s, "task-1")
	meta := store.TaskWorktree{PrimaryDir: "/projects/legato", Path: "/projects/legato.feature", Branch: "feature", BaseBranch: "main"}
	if err := s.SetTaskWorktree(context.Background(), "task-1", &meta); err != nil {
		t.Fatal(err)
	}
	runner := &fakeWorktreeRunner{}

	if err := NewWorktreeWorkflow(s, runner).Remove(context.Background(), "task-1", false); err != nil {
		t.Fatal(err)
	}
	task, _ := s.GetTask(context.Background(), "task-1")
	if task.WorktreePath != nil || task.WorktreeBranch != nil {
		t.Fatalf("metadata not cleared: %#v", task)
	}
	if runner.removedFrom != meta.PrimaryDir || runner.removed != meta.Branch {
		t.Fatalf("remove = (%q, %q)", runner.removedFrom, runner.removed)
	}
}

func TestWorktreeWorkflowRemovePreservesMetadataOnRunnerFailure(t *testing.T) {
	s := newServiceTestStore(t)
	createServiceTestTask(t, s, "task-1")
	meta := store.TaskWorktree{PrimaryDir: "/projects/legato", Path: "/projects/legato.feature", Branch: "feature", BaseBranch: "main"}
	if err := s.SetTaskWorktree(context.Background(), "task-1", &meta); err != nil {
		t.Fatal(err)
	}
	runner := &fakeWorktreeRunner{removeErr: errors.New("dirty worktree")}

	err := NewWorktreeWorkflow(s, runner).Remove(context.Background(), "task-1", false)
	if err == nil {
		t.Fatal("Remove() error = nil")
	}
	task, _ := s.GetTask(context.Background(), "task-1")
	if task.WorktreePath == nil {
		t.Fatal("metadata cleared after failed removal")
	}
}

func newServiceTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func createServiceTestTask(t *testing.T, s *store.Store, id string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := s.CreateTask(context.Background(), store.Task{ID: id, Title: id, Status: "Backlog", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
}

// Compile-time check keeps this test double aligned with the engine entry point.
var _ worktreeRunner = (*engineworktree.Runner)(nil)
