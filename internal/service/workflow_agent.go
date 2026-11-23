package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
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
	bus        *events.Bus
}

// SetBus has a finished workflow agent announced as EventAgentCompleted,
// so Attention reconciles what it said of that agent (an engram that did
// not boot).
func (r *WorkflowAgentRunner) SetBus(bus *events.Bus) { r.bus = bus }

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
	if err == store.ErrNotFound {
		// Finished already: its pane was stopped before its session went.
		// The tmux name may be the next agent's on the same task now (a
		// recovered advance finishing again), so it is not stopped twice.
		return nil
	}
	if err != nil {
		return err
	}
	// The pane is stopped first: until it has, the session is still running,
	// so a sweep meanwhile leaves the sandbox's scratch alone, and a pane
	// that cannot be stopped keeps it and its execution.
	if tmuxer, ok := r.agents.(interface{ Tmux() TmuxManager }); ok && attempt.TmuxSession != "" {
		if err := tmuxer.Tmux().Kill(attempt.TmuxSession); err != nil {
			return fmt.Errorf("stopping the workflow agent's pane: %w", err)
		}
	}
	if session != nil && session.ExecutionID != "" {
		if err := r.store.TerminalizeExecution(ctx, session.ExecutionID, store.ExecutionSucceeded); err != nil {
			return err
		}
		// An agent that ran as an engram ends its engram session with it.
		if ender, ok := r.agents.(interface {
			EndEngramSession(ctx context.Context, executionID string)
		}); ok {
			ender.EndEngramSession(ctx, session.ExecutionID)
		}
	}
	if err := r.store.FinishAgentSession(ctx, *attempt.AgentSessionID); err != nil && err != store.ErrNotFound {
		return err
	}
	if err := r.store.DeleteAgentSession(ctx, *attempt.AgentSessionID); err != nil {
		return err
	}
	// The sandbox's scratch goes once nothing runs in it.
	if remover, ok := r.agents.(interface{ RemoveSandboxScratch(executionID string) }); ok && session != nil {
		remover.RemoveSandboxScratch(session.ExecutionID)
	}
	if r.bus != nil && session != nil {
		r.bus.Publish(events.Event{Type: events.EventAgentCompleted, At: time.Now(), Payload: events.AgentDiedPayload{TaskID: session.TaskID}})
	}
	return nil
}
func (r *WorkflowAgentRunner) PromptAgent(ctx context.Context, taskID, prompt string) error {
	return r.agents.SendMessage(ctx, taskID, prompt)
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
	macros     workflowMacros
	scratch    workflowScratch
}

// SetMacros lets steps name a macro (with.macro) in place of a prompt.
func (e *AgentRunExecutor) SetMacros(macros workflowMacros) { e.macros = macros }

func NewAgentRunExecutor(agents workflowAgentRunner, binaryPath ...string) *AgentRunExecutor {
	executor := &AgentRunExecutor{agents: agents}
	if len(binaryPath) > 0 {
		executor.binaryPath = binaryPath[0]
	}
	return executor
}
func (e *AgentRunExecutor) Type() string { return "agent.run" }

func (e *AgentRunExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	macro, fromMacro, err := stepMacro(ctx, e.macros, step.With)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	prompt := strings.TrimRight(macro.Keys, "\n")
	if fromMacro {
		prompt = macroPRHeader(step.With) + prompt
	}
	if !fromMacro {
		if prompt, err = workflowString(step.With, "prompt", true); err != nil {
			return WorkflowExecutionResult{}, err
		}
	}
	name, _ := workflowString(step.With, "name", false)
	kind, tier := stepAgent(step.With, macro)
	workingDir, _ := workflowString(step.With, "working_dir", false)
	repo, _ := workflowString(step.With, "repo", false)
	ref, _ := workflowString(step.With, "ref", false)
	branch, _ := workflowString(step.With, "branch", false)
	// An engram engram does not list fails the step before anything starts.
	engram, err := workflowString(step.With, "engram", false)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if scratch, _ := step.With["scratch"].(bool); scratch {
		if strings.TrimSpace(workingDir) != "" || strings.TrimSpace(repo) != "" {
			return WorkflowExecutionResult{}, fmt.Errorf("with scratch takes no working_dir or repo")
		}
		if workingDir, err = e.makeScratch(ctx, step.AttemptID); err != nil {
			return WorkflowExecutionResult{}, err
		}
	}
	assignment := buildWorkflowAssignment(prompt, step, e.binaryPath)
	opts := AgentSpawnOptions{
		AgentKind: kind, Tier: tier, WorkingDir: workingDir, Repo: strings.TrimSpace(repo), Ref: strings.TrimSpace(ref), Branch: strings.TrimSpace(branch), Assignment: assignment,
		ExecutionKind: store.ExecutionWorkflowAgent, AttributionTaskID: step.RelatedTaskID,
		WorkflowRunID: step.RunID, WorkflowAttemptID: step.AttemptID,
		Engram: strings.TrimSpace(engram), EngramRequired: strings.TrimSpace(engram) != "",
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
	if err := e.agents.FinishWorkflowAgent(ctx, attempt); err != nil {
		return err
	}
	// The step completed and its agent is gone: so is its scratch folder.
	e.removeScratch(attempt.WorkingDir)
	return nil
}
func (e *AgentRunExecutor) Prompt(ctx context.Context, attempt store.WorkflowStepAttempt, prompt string) error {
	return e.agents.PromptAgent(ctx, attempt.TaskID, prompt)
}

func buildWorkflowAssignment(prompt string, step ResolvedWorkflowStep, binaryPath string) string {
	keys := make([]string, 0, len(step.Outputs))
	for key := range step.Outputs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	outputs := make([]string, 0, len(keys))
	for _, key := range keys {
		outputs = append(outputs, fmt.Sprintf("- %s (%s)", key, step.Outputs[key].Type))
	}
	encoded, _ := json.Marshal(outputs)
	command := "legato"
	if binaryPath != "" {
		command = shellWorkflowCommand(binaryPath)
	}
	// Where the result must go (LEG-532): the conversation goes with the
	// agent, which Legato removes once the step completes.
	keep, _ := step.With["keep_agent"].(bool)
	var kept string
	if scratch, _ := step.With["scratch"].(bool); scratch {
		kept = "Your working folder is a scratch folder made for this step alone. Legato removes it when the step completes, so nothing left in it is kept: anything to keep goes in the step's declared outputs (a file named as an artifact output is copied out first).\n"
	}
	switch {
	case keep:
		kept += "This agent stays running after the step completes, so the operator can go on talking to you; still put the step's result in its declared outputs, which are what later steps and the run keep.\n"
	case len(outputs) == 0:
		kept += "This step declares no outputs, and nothing you write in this conversation is kept: when you complete the step, Legato removes this agent and its conversation. Do not report a result as delivered in chat. If the work has a result someone needs, it must go somewhere lasting that the prompt names; if there is none, say so with `" + command + " workflow step fail \"reason\"`.\n"
	default:
		kept += "What you write in this conversation is not kept: when you complete the step, Legato removes this agent and its conversation. Put the step's whole result in its declared outputs.\n"
	}
	return prompt + "\n\n## Workflow completion contract\n" +
		"Workflow task ID: " + step.TaskID + "\nRun ID: " + step.RunID + "\nStep ID: " + step.StepID + "\nAttempt ID: " + step.AttemptID + "\n" +
		"Declared outputs: " + string(encoded) + "\n" + kept +
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
