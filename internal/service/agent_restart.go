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

func (a *agentService) RestartAgent(ctx context.Context, taskID string, resources []Resource) error {
	session, err := a.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return err
	}
	if owner := restartOwner(session); owner != "" {
		return fmt.Errorf("%w: %s", ErrAgentNotRestartable, owner)
	}
	task, err := a.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	// It comes back as it was, in the sandbox or out of it.
	opt := AgentSpawnOptions{AgentKind: session.AgentKind, Resources: resources, Ephemeral: task.Ephemeral, Unsandboxed: !session.Sandboxed}
	if session.ExecutionID != "" {
		if execution, err := a.store.GetExecution(ctx, session.ExecutionID); err == nil {
			opt.WorkingDir = execution.WorkingDir
		}
	}
	if SupportsTaskResume(session.AgentKind) {
		resume, err := a.restartConversation(ctx, task, session)
		if err != nil {
			return err
		}
		opt.SessionPolicy = "resume"
		opt.ResumeSessionID = resume.ID
		opt.ResumeWorkingDir = resume.CWD
		check := opt
		if err := a.prepareTaskResume(ctx, taskID, &check); err != nil {
			return err
		}
	}
	if err := a.stopAgent(ctx, taskID, false); err != nil {
		return err
	}
	return a.SpawnAgent(ctx, taskID, 0, 0, opt)
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
