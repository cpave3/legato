package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/cpave3/legato/internal/engine/store"
)

type taskAgentDispatcher interface {
	SpawnAgent(context.Context, string, int, int, ...AgentSpawnOptions) error
	SendMessage(context.Context, string, string) error
}

// AgentDispatchExecutor delivers an assignment to the subject task's durable
// agent, creating that agent only when the task has no active session.
type AgentDispatchExecutor struct {
	agents taskAgentDispatcher
	store  *store.Store
	macros workflowMacros
}

// SetMacros lets steps name a macro (with.macro) in place of a prompt: the
// task's agent gets it as a macro started on the task would be sent.
func (e *AgentDispatchExecutor) SetMacros(macros workflowMacros) { e.macros = macros }

func NewAgentDispatchExecutor(agents taskAgentDispatcher, s *store.Store) *AgentDispatchExecutor {
	return &AgentDispatchExecutor{agents: agents, store: s}
}

func (e *AgentDispatchExecutor) Type() string                                            { return "agent.dispatch" }
func (e *AgentDispatchExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }
func (e *AgentDispatchExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	taskID, err := workflowTaskID(step.With["task"])
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	macro, fromMacro, err := stepMacro(ctx, e.macros, step.With)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	var prompt string
	if fromMacro {
		if prompt, err = e.macros.Message(ctx, macro.Keys, taskID, stepPR(step.With)); err != nil {
			return WorkflowExecutionResult{}, err
		}
	} else if prompt, err = workflowString(step.With, "prompt", true); err != nil {
		return WorkflowExecutionResult{}, err
	}
	// An empty working directory starts a new agent where agents start by
	// default; a running one is messaged wherever it is.
	workingDir, err := workflowString(step.With, "working_dir", false)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	kind, tier := stepAgent(step.With, macro)

	session, err := e.store.GetAgentSessionByTaskID(ctx, taskID)
	created := false
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return WorkflowExecutionResult{}, fmt.Errorf("find task agent: %w", err)
		}
		if err := e.agents.SpawnAgent(ctx, taskID, 0, 0, AgentSpawnOptions{
			AgentKind: kind, Tier: tier, WorkingDir: workingDir, Assignment: prompt,
		}); err != nil {
			return WorkflowExecutionResult{}, fmt.Errorf("spawn task agent: %w", err)
		}
		session, err = e.store.GetAgentSessionByTaskID(ctx, taskID)
		if err != nil {
			return WorkflowExecutionResult{}, fmt.Errorf("load spawned task agent: %w", err)
		}
		created = true
	} else if err := e.agents.SendMessage(ctx, taskID, prompt); err != nil {
		return WorkflowExecutionResult{}, fmt.Errorf("prompt task agent: %w", err)
	}

	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{
		"agent_session_id": int64(session.ID), "created": created, "working_dir": workingDir,
	}}, nil
}
