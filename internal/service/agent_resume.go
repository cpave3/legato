package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrTaskResumeConflict = errors.New("task session cannot be resumed")

// TaskResumeSession is a durable conversation selector, not a native panel binding.
type TaskResumeSession struct {
	ID         string
	CWD        string
	StorageDir string
}

func SupportsTaskResume(agentKind string) bool {
	return agentKind == "chimera" || agentKind == "claude-code" || agentKind == "codex"
}

func (a *agentService) GetTaskResumeSession(ctx context.Context, taskID, agentKind string) (*TaskResumeSession, error) {
	if agentKind == "" {
		agentKind = a.DefaultAdapter()
	}
	if !SupportsTaskResume(agentKind) {
		return nil, nil
	}
	if agentKind == "chimera" {
		task, err := a.store.GetTask(ctx, taskID)
		if err != nil {
			return nil, err
		}
		if task.ChimeraSessionID == nil || *task.ChimeraSessionID == "" {
			return nil, nil
		}
		return &TaskResumeSession{ID: *task.ChimeraSessionID}, nil
	}
	saved, err := a.store.GetTaskNativeSession(ctx, taskID, agentKind)
	if err != nil || saved == nil {
		return nil, err
	}
	return &TaskResumeSession{ID: saved.ProviderSessionID, CWD: saved.CWD, StorageDir: saved.StorageDir}, nil
}

func (a *agentService) prepareTaskResume(ctx context.Context, taskID string, opt *AgentSpawnOptions) error {
	if opt.SessionPolicy == "" || opt.SessionPolicy == "error" {
		return nil
	}
	kind := opt.AgentKind
	if kind == "" {
		kind = a.DefaultAdapter()
	}
	adapter := a.resolveAdapter(kind)
	if adapter == nil || adapter.Name() != kind {
		return fmt.Errorf("%w: adapter %s is not configured", ErrTaskResumeConflict, kind)
	}
	if validator, ok := adapter.(SessionLaunchValidator); ok {
		if err := validator.ValidateSessionLaunch(opt.SessionPolicy, opt.Tier); err != nil {
			return fmt.Errorf("%w: %v", ErrTaskResumeConflict, err)
		}
	}
	if opt.SessionPolicy == "new" {
		return nil
	}
	if opt.SessionPolicy != "resume" || !SupportsTaskResume(kind) {
		return fmt.Errorf("%w: unsupported session policy for %s", ErrTaskResumeConflict, kind)
	}
	// Chimera's caller already supplies its hook-captured selector. Its
	// collision and cleanup lifecycle is independent of native bridge records.
	if kind == "chimera" {
		return nil
	}
	saved, err := a.GetTaskResumeSession(ctx, taskID, kind)
	if err != nil {
		return err
	}
	if saved == nil || (opt.ResumeSessionID != "" && opt.ResumeSessionID != saved.ID) {
		return fmt.Errorf("%w: no matching saved %s conversation", ErrTaskResumeConflict, kind)
	}
	info, err := os.Stat(saved.CWD)
	if err != nil {
		return fmt.Errorf("%w: saved %s directory %q is unavailable: %v", ErrTaskResumeConflict, kind, saved.CWD, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: saved directory %q is not a directory", ErrTaskResumeConflict, saved.CWD)
	}
	if opt.WorkingDir != "" {
		selected, err := filepath.EvalSymlinks(opt.WorkingDir)
		if err != nil {
			return fmt.Errorf("%w: selected directory is unavailable: %v", ErrTaskResumeConflict, err)
		}
		original, err := filepath.EvalSymlinks(saved.CWD)
		if err != nil {
			return fmt.Errorf("%w: saved directory is unavailable: %v", ErrTaskResumeConflict, err)
		}
		selected, err = filepath.Abs(selected)
		if err != nil {
			return err
		}
		original, err = filepath.Abs(original)
		if err != nil {
			return err
		}
		if selected != original {
			return fmt.Errorf("%w: saved %s conversation belongs to %q, not %q; choose that directory or start a new session", ErrTaskResumeConflict, kind, saved.CWD, opt.WorkingDir)
		}
	}
	opt.ResumeSessionID = saved.ID
	opt.ResumeWorkingDir = saved.CWD
	opt.ResumeStorageDir = saved.StorageDir
	opt.WorkingDir = saved.CWD
	return nil
}
