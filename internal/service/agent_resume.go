package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cpave3/legato/internal/engine/store"
)

var ErrTaskResumeConflict = errors.New("task session cannot be resumed")

// TaskResumeSession is a durable conversation selector, not a native panel binding.
type TaskResumeSession struct {
	AgentKind       string
	ID              string
	CWD             string
	StorageDir      string
	AccountIdentity *store.AgentAccountIdentity
}

func SupportsTaskResume(agentKind string) bool {
	return agentKind == "chimera" || agentKind == "claude-code" || agentKind == "codex"
}

// GetTaskResumeSession is the conversation the task's main slot resumes.
func (a *agentService) GetTaskResumeSession(ctx context.Context, taskID, agentKind string) (*TaskResumeSession, error) {
	return a.GetTaskResumeSessionInSlot(ctx, taskID, store.SlotMain, agentKind)
}

// GetTaskResumeSessionInSlot is the conversation one of the task's slots
// resumes: each slot keeps its own, so a reviewer never replaces the
// author's.
func (a *agentService) GetTaskResumeSessionInSlot(ctx context.Context, taskID, slot, agentKind string) (*TaskResumeSession, error) {
	if agentKind != "" && !SupportsTaskResume(agentKind) {
		return nil, nil
	}
	if _, err := a.store.GetTask(ctx, taskID); err != nil {
		return nil, err
	}
	var saved *store.TaskNativeSession
	var err error
	if agentKind == "" {
		saved, err = a.store.NewestTaskNativeSessionInSlot(ctx, taskID, slot)
	} else {
		saved, err = a.store.GetTaskNativeSessionInSlot(ctx, taskID, slot, agentKind)
	}
	if err != nil || saved == nil {
		return nil, err
	}
	identity, err := a.store.ExecutionAccount(ctx, saved.ExecutionID)
	if err != nil {
		return nil, err
	}
	return &TaskResumeSession{AgentKind: saved.AgentKind, ID: saved.ProviderSessionID, CWD: saved.CWD, StorageDir: saved.StorageDir, AccountIdentity: identity}, nil
}

func (a *agentService) prepareTaskResume(ctx context.Context, taskID string, opt *AgentSpawnOptions) error {
	if opt.SessionPolicy == "" || opt.SessionPolicy == "error" {
		return nil
	}
	kind := opt.AgentKind
	if kind == "" && opt.SessionPolicy == "resume" {
		if err := a.inferResumeKind(ctx, taskID, opt); err != nil {
			return err
		}
		kind = opt.AgentKind
	}
	if kind == "" {
		kind = a.baseDefaultAdapter()
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
	saved, err := a.resumeConversation(ctx, taskID, *opt, kind)
	if err != nil {
		return err
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
	if kind == "claude-code" {
		if err := a.bindResumeAccount(ctx, opt, saved, kind); err != nil {
			return err
		}
	}
	// An agent that works in a job clone joins it again (and is recorded
	// in it); the spawn checks the clone is where the conversation is.
	if opt.Repo == "" && opt.Clone == 0 {
		opt.WorkingDir = saved.CWD
	}
	return nil
}

// inferResumeKind names the tool of a resume that names none: the tool of
// the slot's newest saved conversation, set as opt.AgentKind. Chimera's
// resume ID comes from that conversation unless the caller gave one. With
// nothing saved there is nothing to resume.
func (a *agentService) inferResumeKind(ctx context.Context, taskID string, opt *AgentSpawnOptions) error {
	saved, err := a.GetTaskResumeSessionInSlot(ctx, taskID, opt.Slot, "")
	if err != nil {
		return err
	}
	if saved == nil {
		return fmt.Errorf("%w: no saved conversation", ErrTaskResumeConflict)
	}
	opt.AgentKind = saved.AgentKind
	if saved.AgentKind == "chimera" && opt.ResumeSessionID == "" {
		opt.ResumeSessionID = saved.ID
	}
	return nil
}

// resumeStorage is the storage root a resume asks for: the selected
// account's directory, else the caller's storage directory, else nil for
// any root.
func resumeStorage(opt AgentSpawnOptions) *string {
	if opt.AccountIdentity != nil {
		return &opt.AccountIdentity.ConfigDir
	}
	if opt.ResumeStorageDir != "" {
		return &opt.ResumeStorageDir
	}
	return nil
}

// resumeConversation is the conversation of kind the task's slot resumes:
// the saved one, unless nothing is saved, the resume names another ID or
// names an ID without a storage root, or it asks for a storage root other
// than the saved one's. Then it is the session history's conversation of
// that ID (in that root, when one is asked for); an ambiguous entry and no
// match are refusals.
func (a *agentService) resumeConversation(ctx context.Context, taskID string, opt AgentSpawnOptions, kind string) (*TaskResumeSession, error) {
	saved, err := a.GetTaskResumeSessionInSlot(ctx, taskID, opt.Slot, kind)
	if err != nil {
		return nil, err
	}
	storage := resumeStorage(opt)
	lookupHistory := saved == nil || (opt.ResumeSessionID != "" && (opt.ResumeSessionID != saved.ID || storage == nil))
	if saved != nil && storage != nil && saved.StorageDir != *storage {
		lookupHistory = true
	}
	if !lookupHistory {
		return saved, nil
	}
	// Any conversation the session history keeps for this task, slot
	// and tool can be resumed, not only the newest.
	earlier, err := a.historyConversation(ctx, taskID, opt.Slot, kind, opt.ResumeSessionID, storage)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, fmt.Errorf("%w: %w", ErrTaskResumeConflict, err)
		}
		return nil, err
	}
	if earlier == nil {
		return nil, fmt.Errorf("%w: no matching saved %s conversation", ErrTaskResumeConflict, kind)
	}
	return earlier, nil
}

// bindResumeAccount keeps a resumed Claude Code conversation on the
// account it ran with, never on defaults that changed since: the identity
// its execution recorded, else, for legacy storage, the account that now
// resolves to that directory (a refusal when none does). Whatever it binds
// is checked against this instance's accounts again before launch.
func (a *agentService) bindResumeAccount(ctx context.Context, opt *AgentSpawnOptions, saved *TaskResumeSession, kind string) error {
	if saved.AccountIdentity != nil {
		opt.AccountIdentity = saved.AccountIdentity
	} else if saved.StorageDir != "" {
		identity, err := a.resolveClaudeAccount(ctx, *opt, kind)
		if err != nil {
			return err
		}
		root, err := resolvedAccountRoot(saved.StorageDir)
		if err != nil || root != identity.ResolvedDir {
			return fmt.Errorf("%w: legacy Claude storage has no matching local account; restore its original account mapping", ErrTaskResumeConflict)
		}
		opt.AccountIdentity = identity
	}
	_, err := a.resolveClaudeAccount(ctx, *opt, kind)
	return err
}

// historyConversation is a conversation of the session history the task's
// slot may resume, nil when it has none of that tool by that ID.
func (a *agentService) historyConversation(ctx context.Context, taskID, slot, kind, providerSessionID string, storageDir *string) (*TaskResumeSession, error) {
	if providerSessionID == "" {
		return nil, nil
	}
	c, err := a.store.FindTaskConversation(ctx, taskID, slot, kind, providerSessionID, storageDir)
	if err != nil || c == nil || c.CWD == "" {
		return nil, err
	}
	identity, err := a.latestRunAccount(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	return &TaskResumeSession{AgentKind: c.AgentKind, ID: c.ProviderSessionID, CWD: c.CWD, StorageDir: c.StorageDir, AccountIdentity: identity}, nil
}

// latestRunAccount is the account of a conversation's newest run that
// recorded one, or nil when none did: a later run with no account (an
// older Legato's) does not reset the conversation to the default.
func (a *agentService) latestRunAccount(ctx context.Context, conversationID int64) (*store.AgentAccountIdentity, error) {
	runs, err := a.store.ListConversationRuns(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	for i := len(runs) - 1; i >= 0; i-- {
		identity, err := a.store.ExecutionAccount(ctx, runs[i].ExecutionID)
		if err != nil || identity != nil {
			return identity, err
		}
	}
	return nil, nil
}
