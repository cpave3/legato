package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

type worktreeRunner interface {
	Create(ctx context.Context, taskID, primaryDir, branch, baseBranch string) (string, error)
	Remove(ctx context.Context, primaryDir, branch string) error
}

// WorktreeWorkflow coordinates external worktree lifecycle with task metadata.
type WorktreeWorkflow struct {
	store  *store.Store
	runner worktreeRunner
}

func NewWorktreeWorkflow(s *store.Store, runner worktreeRunner) *WorktreeWorkflow {
	return &WorktreeWorkflow{store: s, runner: runner}
}

// Path returns the remembered worktree path for a task, or an empty string.
func (w *WorktreeWorkflow) Path(ctx context.Context, taskID string) (string, error) {
	task, err := w.store.GetTask(ctx, taskID)
	if err != nil {
		return "", err
	}
	if task.WorktreePath == nil {
		return "", nil
	}
	return *task.WorktreePath, nil
}

func (w *WorktreeWorkflow) Create(ctx context.Context, taskID, primaryDir, branch, baseBranch string) (*store.TaskWorktree, error) {
	path, err := w.runner.Create(ctx, taskID, primaryDir, branch, baseBranch)
	if err != nil {
		return nil, err
	}
	if strings.Contains(path, "\n") {
		return nil, fmt.Errorf("yggdrasil returned an invalid path (contains newlines); last successful worktree may be at %q", path)
	}
	meta := &store.TaskWorktree{PrimaryDir: primaryDir, Path: path, Branch: branch, BaseBranch: baseBranch}
	if err := w.store.SetTaskWorktree(ctx, taskID, meta); err != nil {
		if cleanupErr := w.runner.Remove(ctx, primaryDir, branch); cleanupErr != nil {
			return nil, fmt.Errorf("persisting worktree metadata: %w (cleanup failed: %v)", err, cleanupErr)
		}
		return nil, fmt.Errorf("persisting worktree metadata: %w", err)
	}
	return meta, nil
}

func (w *WorktreeWorkflow) Remove(ctx context.Context, taskID string) error {
	task, err := w.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if task.WorktreePrimaryDir == nil || task.WorktreeBranch == nil {
		return fmt.Errorf("task %s has no worktree", taskID)
	}
	if err := w.runner.Remove(ctx, *task.WorktreePrimaryDir, *task.WorktreeBranch); err != nil {
		return err
	}
	if err := w.store.SetTaskWorktree(ctx, taskID, nil); err != nil {
		return fmt.Errorf("clearing worktree metadata: %w", err)
	}
	return nil
}
