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

type fakeWorktreeRunner struct {
	path        string
	createErr   error
	removeErr   error
	removedFrom string
	removed     string
}

func (f *fakeWorktreeRunner) Create(context.Context, string, string, string, string) (string, error) {
	return f.path, f.createErr
}
func (f *fakeWorktreeRunner) Remove(_ context.Context, primaryDir, branch string) error {
	f.removedFrom, f.removed = primaryDir, branch
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

func TestWorktreeWorkflowCreateRemovesWorktreeWhenPersistenceFails(t *testing.T) {
	s := newServiceTestStore(t)
	runner := &fakeWorktreeRunner{path: "/projects/legato.feature"}

	_, err := NewWorktreeWorkflow(s, runner).Create(context.Background(), "missing", "/projects/legato", "feature", "main")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if runner.removedFrom != "/projects/legato" || runner.removed != "feature" {
		t.Fatalf("cleanup = (%q, %q)", runner.removedFrom, runner.removed)
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

	if err := NewWorktreeWorkflow(s, runner).Remove(context.Background(), "task-1"); err != nil {
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

	err := NewWorktreeWorkflow(s, runner).Remove(context.Background(), "task-1")
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
