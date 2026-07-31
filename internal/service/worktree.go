package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/cpave3/legato/internal/engine/store"
)

type worktreeRunner interface {
	Create(ctx context.Context, taskID, primaryDir, branch, baseBranch string) (string, error)
	Remove(ctx context.Context, primaryDir, branch string, force bool) error
}

// WorktreeWorkflow coordinates external worktree lifecycle with task metadata.
type WorktreeWorkflow struct {
	store     *store.Store
	runner    worktreeRunner
	inspector CheckoutInspector
}

func NewWorktreeWorkflow(s *store.Store, runner worktreeRunner, inspectors ...CheckoutInspector) *WorktreeWorkflow {
	var inspector CheckoutInspector
	if len(inspectors) > 0 {
		inspector = inspectors[0]
	}
	return &WorktreeWorkflow{store: s, runner: runner, inspector: inspector}
}

// Get returns the durable worktree metadata for a task.
func (w *WorktreeWorkflow) Get(ctx context.Context, taskID string) (*store.TaskWorktree, error) {
	task, err := w.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.WorktreePrimaryDir == nil || task.WorktreePath == nil || task.WorktreeBranch == nil || task.WorktreeBaseBranch == nil {
		return nil, fmt.Errorf("task %s has no worktree", taskID)
	}
	return &store.TaskWorktree{PrimaryDir: *task.WorktreePrimaryDir, Path: *task.WorktreePath, Branch: *task.WorktreeBranch, BaseBranch: *task.WorktreeBaseBranch}, nil
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
	lockName := fmt.Sprintf("legato-worktree-%x.lock", sha256.Sum256([]byte(taskID)))
	lock, err := os.OpenFile(filepath.Join(os.TempDir(), lockName), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("opening worktree lock: %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return nil, fmt.Errorf("locking worktree creation: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck

	task, err := w.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.WorktreePath != nil {
		return nil, fmt.Errorf("task %s already has a worktree", taskID)
	}

	path, err := w.runner.Create(ctx, taskID, primaryDir, branch, baseBranch)
	if err != nil {
		return nil, err
	}
	if strings.Contains(path, "\n") {
		return nil, fmt.Errorf("yggdrasil returned an invalid path (contains newlines); last successful worktree may be at %q", path)
	}
	meta := &store.TaskWorktree{PrimaryDir: primaryDir, Path: path, Branch: branch, BaseBranch: baseBranch}
	if err := w.store.SetTaskWorktree(ctx, taskID, meta); err != nil {
		if cleanupErr := w.runner.Remove(ctx, primaryDir, branch, false); cleanupErr != nil {
			return nil, fmt.Errorf("persisting worktree metadata: %w (cleanup failed: %v)", err, cleanupErr)
		}
		return nil, fmt.Errorf("persisting worktree metadata: %w", err)
	}
	w.captureContext(ctx, taskID, meta)
	return meta, nil
}

// Record associates externally-created worktree metadata and captures its checkout context.
func (w *WorktreeWorkflow) Record(ctx context.Context, taskID string, meta *store.TaskWorktree) error {
	if err := w.store.SetTaskWorktree(ctx, taskID, meta); err != nil {
		return err
	}
	w.captureContext(ctx, taskID, meta)
	return nil
}

func (w *WorktreeWorkflow) captureContext(ctx context.Context, taskID string, meta *store.TaskWorktree) {
	if w.inspector == nil || meta == nil || meta.Path == "" {
		return
	}
	captured, err := w.inspector.InspectCodeContext(meta.Path)
	if err != nil || captured == nil {
		return
	}
	existingTask, err := w.store.GetTask(ctx, taskID)
	if err != nil {
		return
	}
	existing, _ := store.ParseCodeContext(existingTask.CodeContext)
	if existing != nil && (existing.RepoSource == "manual" || existing.BranchSource == "manual") {
		return
	}
	captured.WorkingDir = meta.Path
	captured.BaseBranch = meta.BaseBranch
	captured.RepoSource = "worktree"
	captured.BranchSource = "worktree"
	_ = w.store.SetCodeContext(ctx, taskID, captured)
}

func (w *WorktreeWorkflow) Remove(ctx context.Context, taskID string, force bool) error {
	task, err := w.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if task.WorktreePrimaryDir == nil || task.WorktreeBranch == nil {
		return fmt.Errorf("task %s has no worktree", taskID)
	}
	if err := w.runner.Remove(ctx, *task.WorktreePrimaryDir, *task.WorktreeBranch, force); err != nil {
		return err
	}
	if err := w.store.SetTaskWorktree(ctx, taskID, nil); err != nil {
		return fmt.Errorf("clearing worktree metadata: %w", err)
	}
	return nil
}
