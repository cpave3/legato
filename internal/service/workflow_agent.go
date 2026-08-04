package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

type workflowAgentRunner interface {
	SpawnWorkflowAgent(context.Context, string, string, AgentSpawnOptions) (*store.AgentSession, error)
	KillAgent(context.Context, string) error
	FinishWorkflowAgent(context.Context, store.WorkflowStepAttempt) error
	PromptAgent(context.Context, string, string) error
}

// WorkflowAgentRunner adapts the presentation-facing agent service to the
// workflow executor's spawn-and-resolve contract.
type WorkflowAgentRunner struct {
	agents     AgentService
	store      *store.Store
	binaryPath string
}

func NewWorkflowAgentRunner(agents AgentService, s *store.Store, binaryPath ...string) *WorkflowAgentRunner {
	runner := &WorkflowAgentRunner{agents: agents, store: s}
	if len(binaryPath) > 0 {
		runner.binaryPath = binaryPath[0]
	}
	return runner
}
func (r *WorkflowAgentRunner) SpawnWorkflowAgent(ctx context.Context, taskID, name string, opts AgentSpawnOptions) (*store.AgentSession, error) {
	if r.binaryPath != "" {
		opts.ExtraEnv["LEGATO_WORKFLOW_BINARY"] = r.binaryPath
	}
	if name != "" {
		task, err := r.store.GetTask(ctx, taskID)
		if err != nil {
			return nil, err
		}
		task.Title = name
		if err := r.store.UpdateTask(ctx, *task); err != nil {
			return nil, err
		}
	}
	if err := r.agents.SpawnAgent(ctx, taskID, 0, 0, opts); err != nil {
		return nil, err
	}
	return r.store.GetAgentSessionByTaskID(ctx, taskID)
}
func (r *WorkflowAgentRunner) KillAgent(ctx context.Context, taskID string) error {
	return r.agents.KillAgent(ctx, taskID)
}
func (r *WorkflowAgentRunner) FinishWorkflowAgent(ctx context.Context, attempt store.WorkflowStepAttempt) error {
	if attempt.AgentSessionID == nil {
		return nil
	}
	session, err := r.store.GetAgentSessionByID(ctx, *attempt.AgentSessionID)
	if err != nil && err != store.ErrNotFound {
		return err
	}
	if session != nil && session.ExecutionID != "" {
		if err := r.store.TerminalizeExecution(ctx, session.ExecutionID, store.ExecutionSucceeded); err != nil {
			return err
		}
	}
	if err := r.store.FinishAgentSession(ctx, *attempt.AgentSessionID); err != nil && err != store.ErrNotFound {
		return err
	}
	if err := r.store.DeleteAgentSession(ctx, *attempt.AgentSessionID); err != nil {
		return err
	}
	if tmuxer, ok := r.agents.(interface{ Tmux() TmuxManager }); ok && attempt.TmuxSession != "" {
		tmuxer.Tmux().Kill(attempt.TmuxSession)
	}
	return nil
}
func (r *WorkflowAgentRunner) PromptAgent(ctx context.Context, taskID, prompt string) error {
	// Use AgentService.SendMessage for Chimera agents (HTTP API) and
	// tmux send-keys for non-Chimera agents. This ensures workflow
	// follow-ups reach the agent through the same path as web follow-ups.
	if r.agents.HasChimeraEndpoint(ctx, taskID) {
		return r.agents.SendMessage(ctx, taskID, prompt)
	}
	session, err := r.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return err
	}
	tmuxer, ok := r.agents.(interface{ Tmux() TmuxManager })
	if !ok {
		return fmt.Errorf("agent service does not support workflow prompts")
	}
	return tmuxer.Tmux().SendKeysLine(session.TmuxSession, prompt)
}

type AgentStopExecutor struct{ agents workflowAgentRunner }

func NewAgentStopExecutor(agents workflowAgentRunner) *AgentStopExecutor {
	return &AgentStopExecutor{agents: agents}
}
func (e *AgentStopExecutor) Type() string { return "agent.stop" }
func (e *AgentStopExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	taskID, err := interviewTaskID(step.With["task"])
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if err := e.agents.KillAgent(ctx, taskID); err != nil {
		return WorkflowExecutionResult{}, err
	}
	return WorkflowExecutionResult{Status: "succeeded"}, nil
}
func (e *AgentStopExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

type AgentRunExecutor struct {
	agents     workflowAgentRunner
	binaryPath string
}

func NewAgentRunExecutor(agents workflowAgentRunner, binaryPath ...string) *AgentRunExecutor {
	executor := &AgentRunExecutor{agents: agents}
	if len(binaryPath) > 0 {
		executor.binaryPath = binaryPath[0]
	}
	return executor
}
func (e *AgentRunExecutor) Type() string { return "agent.run" }

func (e *AgentRunExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	prompt, err := workflowString(step.With, "prompt", true)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	name, _ := workflowString(step.With, "name", false)
	kind, _ := workflowString(step.With, "agent", false)
	tier, _ := workflowString(step.With, "tier", false)
	workingDir, _ := workflowString(step.With, "working_dir", false)
	assignment := buildWorkflowAssignment(prompt, step, e.binaryPath)
	opts := AgentSpawnOptions{
		AgentKind: kind, Tier: tier, WorkingDir: workingDir, Assignment: assignment,
		ExecutionKind: store.ExecutionWorkflowAgent, AttributionTaskID: step.RelatedTaskID,
		WorkflowRunID: step.RunID, WorkflowAttemptID: step.AttemptID,
		ExtraEnv: map[string]string{
			"LEGATO_TASK_ID": step.TaskID, "LEGATO_WORKFLOW_RUN_ID": step.RunID, "LEGATO_WORKFLOW_STEP_ID": step.StepID, "LEGATO_WORKFLOW_ATTEMPT_ID": step.AttemptID,
		},
	}
	session, err := e.agents.SpawnWorkflowAgent(ctx, step.TaskID, name, opts)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	return WorkflowExecutionResult{Status: "waiting_agent", AgentSession: session, WorkingDir: workingDir}, nil
}

func (e *AgentRunExecutor) Cancel(ctx context.Context, attempt store.WorkflowStepAttempt) error {
	return e.agents.KillAgent(ctx, attempt.TaskID)
}
func (e *AgentRunExecutor) Finish(ctx context.Context, attempt store.WorkflowStepAttempt) error {
	return e.agents.FinishWorkflowAgent(ctx, attempt)
}
func (e *AgentRunExecutor) Prompt(ctx context.Context, attempt store.WorkflowStepAttempt, prompt string) error {
	return e.agents.PromptAgent(ctx, attempt.TaskID, prompt)
}

func buildWorkflowAssignment(prompt string, step ResolvedWorkflowStep, binaryPath string) string {
	var outputs []string
	for key, output := range step.Outputs {
		outputs = append(outputs, fmt.Sprintf("- %s (%s)", key, output.Type))
	}
	encoded, _ := json.Marshal(outputs)
	command := "legato"
	if binaryPath != "" {
		command = shellWorkflowCommand(binaryPath)
	}
	return prompt + "\n\n## Workflow completion contract\n" +
		"Workflow task ID: " + step.TaskID + "\nRun ID: " + step.RunID + "\nStep ID: " + step.StepID + "\nAttempt ID: " + step.AttemptID + "\n" +
		"Declared outputs: " + string(encoded) + "\n" +
		"When all outputs are ready, run `" + command + " workflow step complete` with the output/artifact flags. On unrecoverable failure run `" + command + " workflow step fail \"reason\"`."
}

func shellWorkflowCommand(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
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
