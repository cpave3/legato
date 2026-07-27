package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

type workflowAgentRunner interface {
	SpawnWorkflowAgent(context.Context, string, AgentSpawnOptions) (*store.AgentSession, error)
	KillAgent(context.Context, string) error
}

// WorkflowAgentRunner adapts the presentation-facing agent service to the
// workflow executor's spawn-and-resolve contract.
type WorkflowAgentRunner struct {
	agents AgentService
	store  *store.Store
}

func NewWorkflowAgentRunner(agents AgentService, s *store.Store) *WorkflowAgentRunner {
	return &WorkflowAgentRunner{agents: agents, store: s}
}
func (r *WorkflowAgentRunner) SpawnWorkflowAgent(ctx context.Context, taskID string, opts AgentSpawnOptions) (*store.AgentSession, error) {
	if err := r.agents.SpawnAgent(ctx, taskID, 0, 0, opts); err != nil {
		return nil, err
	}
	return r.store.GetAgentSessionByTaskID(ctx, taskID)
}
func (r *WorkflowAgentRunner) KillAgent(ctx context.Context, taskID string) error {
	return r.agents.KillAgent(ctx, taskID)
}

type AgentRunExecutor struct{ agents workflowAgentRunner }

func NewAgentRunExecutor(agents workflowAgentRunner) *AgentRunExecutor {
	return &AgentRunExecutor{agents: agents}
}
func (e *AgentRunExecutor) Type() string { return "agent.run" }

func (e *AgentRunExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	prompt, err := workflowString(step.With, "prompt", true)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	kind, _ := workflowString(step.With, "agent", false)
	tier, _ := workflowString(step.With, "tier", false)
	workingDir, _ := workflowString(step.With, "working_dir", false)
	assignment := buildWorkflowAssignment(prompt, step)
	opts := AgentSpawnOptions{AgentKind: kind, Tier: tier, WorkingDir: workingDir, Assignment: assignment, ExtraEnv: map[string]string{
		"LEGATO_WORKFLOW_RUN_ID": step.RunID, "LEGATO_WORKFLOW_STEP_ID": step.StepID, "LEGATO_WORKFLOW_ATTEMPT_ID": step.AttemptID,
	}}
	session, err := e.agents.SpawnWorkflowAgent(ctx, step.TaskID, opts)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	return WorkflowExecutionResult{Status: "waiting_agent", AgentSession: session, WorkingDir: workingDir}, nil
}

func (e *AgentRunExecutor) Cancel(ctx context.Context, attempt store.WorkflowStepAttempt) error {
	return e.agents.KillAgent(ctx, attempt.TaskID)
}

func buildWorkflowAssignment(prompt string, step ResolvedWorkflowStep) string {
	var outputs []string
	for key, output := range step.Outputs {
		outputs = append(outputs, fmt.Sprintf("- %s (%s)", key, output.Type))
	}
	encoded, _ := json.Marshal(outputs)
	return prompt + "\n\n## Workflow completion contract\n" +
		"Run ID: " + step.RunID + "\nStep ID: " + step.StepID + "\nAttempt ID: " + step.AttemptID + "\n" +
		"Declared outputs: " + string(encoded) + "\n" +
		"When all outputs are ready, run `legato workflow step complete` with the output/artifact flags. On unrecoverable failure run `legato workflow step fail \"reason\"`."
}

func workflowString(values map[string]any, key string, required bool) (string, error) {
	value, ok := values[key]
	if !ok {
		if required {
			return "", fmt.Errorf("%s is required", key)
		}
		return "", nil
	}
	text, ok := value.(string)
	if !ok || (required && strings.TrimSpace(text) == "") {
		return "", fmt.Errorf("%s must be a string", key)
	}
	return text, nil
}
