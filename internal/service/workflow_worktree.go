package service

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/cpave3/legato/internal/engine/store"
)

type workflowWorktreeRemover interface {
	Get(context.Context, string) (*store.TaskWorktree, error)
	Remove(context.Context, string) error
}

type WorktreeRemoveExecutor struct{ worktrees workflowWorktreeRemover }

func NewWorktreeRemoveExecutor(worktrees workflowWorktreeRemover) *WorktreeRemoveExecutor {
	return &WorktreeRemoveExecutor{worktrees: worktrees}
}
func (e *WorktreeRemoveExecutor) Type() string { return "worktree.remove" }
func (e *WorktreeRemoveExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	expected, err := workflowString(step.With, "expected_path", true)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	meta, err := e.worktrees.Get(ctx, step.TaskID)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	actual, err := filepath.Abs(meta.Path)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	wanted, err := filepath.Abs(expected)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if actual != wanted {
		return WorkflowExecutionResult{}, fmt.Errorf("worktree path mismatch: task has %q, workflow expected %q", meta.Path, expected)
	}
	if err := e.worktrees.Remove(ctx, step.TaskID); err != nil {
		return WorkflowExecutionResult{}, err
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"removed": true, "removed_path": meta.Path, "removed_branch": meta.Branch}}, nil
}
func (e *WorktreeRemoveExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }
