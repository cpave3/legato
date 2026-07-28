package service

import (
	"context"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

type fakeWorkflowWorktreeRemover struct {
	meta    *store.TaskWorktree
	removed string
	force   bool
}

func (f *fakeWorkflowWorktreeRemover) Get(context.Context, string) (*store.TaskWorktree, error) {
	return f.meta, nil
}
func (f *fakeWorkflowWorktreeRemover) Remove(_ context.Context, taskID string, force bool) error {
	f.removed, f.force = taskID, force
	return nil
}

func TestWorktreeRemoveExecutorChecksExpectedPathAndRemoves(t *testing.T) {
	worktrees := &fakeWorkflowWorktreeRemover{meta: &store.TaskWorktree{PrimaryDir: "/repo", Path: "/repo.wt", Branch: "branch", BaseBranch: "main"}}
	executor := NewWorktreeRemoveExecutor(worktrees)
	result, err := executor.Start(context.Background(), ResolvedWorkflowStep{TaskID: "task", With: map[string]any{"expected_path": "/repo.wt"}})
	if err != nil {
		t.Fatal(err)
	}
	if worktrees.removed != "task" || result.Outputs["removed_path"] != "/repo.wt" || result.Outputs["removed"] != true {
		t.Fatalf("result=%+v removed=%q", result, worktrees.removed)
	}
}

func TestWorktreeRemoveExecutorForwardsForceSelection(t *testing.T) {
	worktrees := &fakeWorkflowWorktreeRemover{meta: &store.TaskWorktree{Path: "/repo.wt", Branch: "branch"}}
	_, err := NewWorktreeRemoveExecutor(worktrees).Start(context.Background(), ResolvedWorkflowStep{TaskID: "task", With: map[string]any{"expected_path": "/repo.wt", "force": true}})
	if err != nil {
		t.Fatal(err)
	}
	if !worktrees.force {
		t.Fatal("force selection was not forwarded")
	}
}

func TestWorktreeRemoveExecutorRejectsMismatchedPath(t *testing.T) {
	worktrees := &fakeWorkflowWorktreeRemover{meta: &store.TaskWorktree{Path: "/repo.other", Branch: "branch"}}
	_, err := NewWorktreeRemoveExecutor(worktrees).Start(context.Background(), ResolvedWorkflowStep{TaskID: "task", With: map[string]any{"expected_path": "/repo.wt"}})
	if err == nil || worktrees.removed != "" {
		t.Fatalf("err=%v removed=%q", err, worktrees.removed)
	}
}
