package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/cpave3/legato/internal/engine/store"
)

// ErrAgentNotRestartable means the running agent cannot be started again by
// itself: a swarm or a workflow owns it, or there is no conversation to
// resume. The agent is left running.
var ErrAgentNotRestartable = errors.New("agent cannot be restarted")

// AgentRestarter starts a running agent again.
type AgentRestarter interface {
	// RestartAgent stops the running agent of a task and starts it again as
	// it was: the same agent tool, directory and resources, resuming its
	// conversation, so it reads its configuration (MCP servers, plugins,
	// hooks) afresh. Everything that can stop the restart is checked before
	// the agent is stopped.
	RestartAgent(ctx context.Context, taskID string, resources []Resource) error
}

// SandboxSwitcher starts a running agent again on the other side of the
// sandbox.
type SandboxSwitcher interface {
	// RestartAgentSwitchingSandbox restarts the agent as RestartAgent does,
	// but outside the sandbox when it ran in it and in it when it ran
	// outside, for when the sandbox blocks something the agent needs. It
	// says whether the agent now runs in the sandbox.
	RestartAgentSwitchingSandbox(ctx context.Context, taskID string, resources []Resource) (sandboxed bool, err error)
}

// The web UI's restart route and the TUI's U both find the switch this way.
var _ SandboxSwitcher = (*agentService)(nil)

func (a *agentService) RestartAgent(ctx context.Context, taskID string, resources []Resource) error {
	_, err := a.restartAgent(ctx, taskID, resources, false)
	return err
}

func (a *agentService) RestartAgentSwitchingSandbox(ctx context.Context, taskID string, resources []Resource) (bool, error) {
	return a.restartAgent(ctx, taskID, resources, true)
}

func (a *agentService) restartAgent(ctx context.Context, taskID string, resources []Resource, switchSandbox bool) (bool, error) {
	if _, slot := SplitAgentID(taskID); IsNamedSlot(slot) {
		// Legato starts a named slot's agent with what its work needs (a
		// reviewer's read-only, unattended sandbox); a restart would not.
		return false, fmt.Errorf("%w: Legato runs the %s of this task", ErrAgentNotRestartable, slot)
	}
	session, err := a.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return false, err
	}
	if owner := restartOwner(session); owner != "" {
		return false, fmt.Errorf("%w: %s", ErrAgentNotRestartable, owner)
	}
	task, err := a.store.GetTask(ctx, taskID)
	if err != nil {
		return false, err
	}
	// It comes back as it was, in the sandbox or out of it, unless it is
	// asked to switch.
	sandboxed := session.Sandboxed
	if switchSandbox && IsReeveTask(ctx, a.store, taskID) {
		return false, ErrReeveSandboxed
	}
	if switchSandbox {
		sandboxed = !sandboxed
		if sandboxed && a.sandbox == nil {
			return false, errors.New("Legato's sandbox is off, so there is no sandbox to put the agent in")
		}
	}
	opt := AgentSpawnOptions{AgentKind: session.AgentKind, Resources: resources, Ephemeral: task.Ephemeral, Unsandboxed: !sandboxed}
	if session.ExecutionID != "" {
		if execution, err := a.store.GetExecution(ctx, session.ExecutionID); err == nil {
			opt.WorkingDir = execution.WorkingDir
		}
		// It comes back as the engram it ran as: in the same engram
		// session when its conversation is resumed, booted again with a
		// new one when not.
		if name, engramSession, err := a.store.ExecutionEngram(ctx, session.ExecutionID); err == nil && name != "" {
			opt.Engram, opt.EngramSession = name, engramSession
		}
	}
	if SupportsTaskResume(session.AgentKind) {
		resume, err := a.restartConversation(ctx, task, session)
		if err != nil {
			return false, err
		}
		opt.SessionPolicy = "resume"
		opt.ResumeSessionID = resume.ID
		opt.ResumeWorkingDir = resume.CWD
		check := opt
		if err := a.prepareTaskResume(ctx, taskID, &check); err != nil {
			return false, err
		}
	}
	stopCtx := ctx
	if opt.SessionPolicy == "resume" {
		// The engram session goes on in the agent that resumes it.
		stopCtx = withEngramSessionKept(ctx)
	}
	if err := a.stopAgent(stopCtx, taskID, false); err != nil {
		return false, err
	}
	return sandboxed, a.SpawnAgent(ctx, taskID, 0, 0, opt)
}

// restartOwner names what owns an agent that is started by it, not by the
// person, or is empty for an agent of its own.
func restartOwner(session *store.AgentSession) string {
	switch {
	case session.WorkflowRunID != "" || session.ExecutionKind == store.ExecutionWorkflowAgent:
		return "a workflow runs this agent"
	case session.Role != "" || (session.ParentTaskID != nil && *session.ParentTaskID != "") ||
		(session.SubtaskID != nil && *session.SubtaskID != ""):
		return "a swarm runs this agent"
	}
	return ""
}

// restartConversation is the conversation the running agent reported, which
// its restart resumes.
func (a *agentService) restartConversation(ctx context.Context, task *store.Task, session *store.AgentSession) (*TaskResumeSession, error) {
	kind := session.AgentKind
	// A Chimera agent's native binding is its Chimera session.
	if kind == "chimera" && session.ChimeraSessionID != nil && *session.ChimeraSessionID != "" {
		return &TaskResumeSession{ID: *session.ChimeraSessionID}, nil
	}
	saved, err := a.GetTaskResumeSession(ctx, task.ID, kind)
	if err != nil {
		return nil, err
	}
	if saved == nil {
		return nil, fmt.Errorf("%w: the %s session has not reported its conversation yet, so a restart would lose it", ErrAgentNotRestartable, kind)
	}
	return saved, nil
}
