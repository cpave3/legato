package service

import (
	"context"

	"github.com/cpave3/legato/internal/engine/store"
)

// AgentDispatchExecutor runs an agent.dispatch step, which is read as
// agent.run with on: <task> and reuse: true that does not wait (LEG-607):
// it delivers the step's work to the task's own agent, starting one when
// the task has none, and succeeds at once.
type AgentDispatchExecutor struct{ run *AgentRunExecutor }

func NewAgentDispatchExecutor(run *AgentRunExecutor) *AgentDispatchExecutor {
	return &AgentDispatchExecutor{run: run}
}

func (e *AgentDispatchExecutor) Type() string                                            { return "agent.dispatch" }
func (e *AgentDispatchExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }
func (e *AgentDispatchExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	taskID, err := workflowTaskID(step.With["task"])
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	return e.run.startOnTask(ctx, step, taskID, true, false)
}
