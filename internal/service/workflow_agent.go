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
	// The name is the agent's, not the run's task's: the run keeps its
	// run_title, and a run's later agents keep their own names.
	opts.Label = name
	if err := r.agents.SpawnAgent(ctx, taskID, 0, 0, opts); err != nil {
		return nil, err
	}
	// The session is the slot's: a named slot's agent runs beside the
	// task's own, which the step must never take for its agent.
	return r.store.GetAgentSessionInSlot(ctx, taskID, opts.Slot)
}

// SlotHasAgent says whether one of taskID's slots has an agent running.
func (r *WorkflowAgentRunner) SlotHasAgent(ctx context.Context, taskID, slot string) bool {
	_, err := r.store.GetAgentSessionInSlot(ctx, taskID, slot)
	return err == nil
}

// KillAgent stops the agent of an agent ID: a task's main slot, or
// <task>_<slot> for a named one.
func (r *WorkflowAgentRunner) KillAgent(ctx context.Context, taskID string) error {
	return r.agents.KillAgent(ctx, taskID)
}

// AttemptAgentID is the agent ID of an attempt's own agent: <task>_<slot>
// when it runs in a named slot. With its agent gone, a named slot's
// attempt has none, so nothing is stopped or prompted in the main slot.
func (r *WorkflowAgentRunner) AttemptAgentID(ctx context.Context, attempt store.WorkflowStepAttempt) (string, bool) {
	session, err := attemptAgentSession(ctx, r.store, attempt)
	switch {
	case err == nil:
		return AgentSlotKey(session.TaskID, session.Slot), true
	case attemptInNamedSlot(ctx, r.store, attempt):
		return "", false
	}
	return attempt.TaskID, true
}

// attemptAgentSession is the running session of an attempt's own agent:
// the session it recorded, which tells a named slot's agent from the main
// slot's. An attempt with no session it can find is read as the main
// slot's, unless its tmux session is a named slot's.
func attemptAgentSession(ctx context.Context, s *store.Store, attempt store.WorkflowStepAttempt) (*store.AgentSession, error) {
	if attempt.AgentSessionID != nil {
		session, err := s.GetAgentSessionByID(ctx, *attempt.AgentSessionID)
		if err == nil {
			if session.Status != "running" {
				return nil, store.ErrNotFound
			}
			return session, nil
		}
		if err != store.ErrNotFound {
			return nil, err
		}
	}
	if attemptInNamedSlot(ctx, s, attempt) {
		return nil, store.ErrNotFound
	}
	return s.GetAgentSessionByTaskID(ctx, attempt.TaskID)
}

// attemptInNamedSlot says whether an attempt's agent ran in a named slot
// of its task: its session says so, or its tmux session is a named slot's
// (legato-<task>_<slot>).
func attemptInNamedSlot(ctx context.Context, s *store.Store, attempt store.WorkflowStepAttempt) bool {
	if attempt.AgentSessionID != nil {
		if session, err := s.GetAgentSessionByID(ctx, *attempt.AgentSessionID); err == nil {
			return IsNamedSlot(session.Slot)
		}
	}
	slot, ok := strings.CutPrefix(attempt.TmuxSession, AgentTmuxSession(attempt.TaskID, store.SlotMain)+"_")
	return ok && slotName.MatchString(slot)
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
		r.bus.Publish(events.Event{Type: events.EventAgentCompleted, At: time.Now(), Payload: events.AgentDiedPayload{TaskID: session.TaskID, Slot: namedSlot(session.Slot)}})
	}
	return nil
}

// PromptAgent messages the agent of an agent ID: a task's main slot, or
// <task>_<slot> for a named one.
func (r *WorkflowAgentRunner) PromptAgent(ctx context.Context, taskID, prompt string) error {
	ctx, taskID = agentAddress(ctx, taskID)
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
	// With task, the agent is that task's own (LEG-540): on its card, in
	// its slot main, reading its context; with slot too, in that named
	// slot of the card, beside its own agent (LEG-618).
	agentTask, err := stepAgentTask(step.With)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	slot, err := stepAgentSlot(step.With, agentTask)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	macro, fromMacro, err := stepMacro(ctx, e.macros, step.With)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	prompt := strings.TrimRight(macro.Keys, "\n")
	switch {
	case fromMacro && agentTask != "":
		// As a macro started on the card is sent: its key and title first.
		if prompt, err = e.macros.Message(ctx, macro.Keys, agentTask, stepPR(step.With)); err != nil {
			return WorkflowExecutionResult{}, err
		}
	case fromMacro:
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
	spawnTask, attribution := step.TaskID, step.RelatedTaskID
	if agentTask != "" {
		if strings.TrimSpace(workingDir) != "" || strings.TrimSpace(name) != "" {
			return WorkflowExecutionResult{}, fmt.Errorf("with task takes no working_dir or name")
		}
		if checker, ok := e.agents.(interface {
			SlotHasAgent(ctx context.Context, taskID, slot string) bool
		}); ok && checker.SlotHasAgent(ctx, agentTask, slot) {
			if IsNamedSlot(slot) {
				return WorkflowExecutionResult{}, fmt.Errorf("task %s already has an agent in its %s slot: it must end first", agentTask, slot)
			}
			return WorkflowExecutionResult{}, fmt.Errorf("task %s already has an agent running: stop it first, or use agent.dispatch to message it", agentTask)
		}
		spawnTask, attribution = agentTask, agentTask
	}
	assignment := buildWorkflowAssignment(prompt, step, e.binaryPath)
	opts := AgentSpawnOptions{
		AgentKind: kind, Tier: tier, WorkingDir: workingDir, Repo: strings.TrimSpace(repo), Ref: strings.TrimSpace(ref), Branch: strings.TrimSpace(branch), Assignment: assignment,
		ExecutionKind: store.ExecutionWorkflowAgent, AttributionTaskID: attribution, Slot: slot,
		WorkflowRunID: step.RunID, WorkflowAttemptID: step.AttemptID,
		Engram: strings.TrimSpace(engram), EngramRequired: strings.TrimSpace(engram) != "",
		ExtraEnv: map[string]string{
			"LEGATO_TASK_ID": spawnTask, "LEGATO_WORKFLOW_RUN_ID": step.RunID, "LEGATO_WORKFLOW_STEP_ID": step.StepID, "LEGATO_WORKFLOW_ATTEMPT_ID": step.AttemptID,
		},
	}
	session, err := e.agents.SpawnWorkflowAgent(ctx, spawnTask, name, opts)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	result := WorkflowExecutionResult{Status: "waiting_agent", AgentSession: session, WorkingDir: workingDir}
	if agentTask != "" {
		result.TaskID = agentTask
	}
	return result, nil
}

// stepAgentTask is the task an agent.run step's agent belongs to (with.task:
// a task input or a task ID), or "" for the run's own task.
func stepAgentTask(with map[string]any) (string, error) {
	value, ok := with["task"]
	if !ok || value == nil {
		return "", nil
	}
	if text, isText := value.(string); isText && strings.TrimSpace(text) == "" {
		return "", nil
	}
	taskID, err := interviewTaskID(value)
	if err != nil {
		return "", fmt.Errorf("with task: %w", err)
	}
	return strings.TrimSpace(taskID), nil
}

// stepAgentSlot is the card's slot an agent.run step's agent runs in
// (with.slot): "" for the main slot. The file was checked when it loaded;
// this checks it again on the resolved values.
func stepAgentSlot(with map[string]any, agentTask string) (string, error) {
	value, ok := with["slot"]
	if !ok || value == nil {
		return "", nil
	}
	slot, isText := value.(string)
	if !isText || slot == "" {
		return "", fmt.Errorf("with slot must be main or a slot name")
	}
	if !IsNamedSlot(slot) {
		return "", nil
	}
	if agentTask == "" {
		return "", fmt.Errorf("with slot requires task")
	}
	if err := ValidateAgentSlot(slot); err != nil {
		return "", fmt.Errorf("with slot: %w", err)
	}
	if slot == SlotReviewer {
		return "", fmt.Errorf("with slot %s is the review loop's", slot)
	}
	return slot, nil
}

// attemptAgent is the agent ID of the attempt's own agent, when its runner
// can tell it apart from the task's main slot.
func (e *AgentRunExecutor) attemptAgent(ctx context.Context, attempt store.WorkflowStepAttempt) (string, bool) {
	if finder, ok := e.agents.(interface {
		AttemptAgentID(context.Context, store.WorkflowStepAttempt) (string, bool)
	}); ok {
		return finder.AttemptAgentID(ctx, attempt)
	}
	return attempt.TaskID, true
}

func (e *AgentRunExecutor) Cancel(ctx context.Context, attempt store.WorkflowStepAttempt) error {
	agentID, ok := e.attemptAgent(ctx, attempt)
	if !ok {
		return nil
	}
	return e.agents.KillAgent(ctx, agentID)
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
	agentID, ok := e.attemptAgent(ctx, attempt)
	if !ok {
		return fmt.Errorf("the step's agent has ended")
	}
	return e.agents.PromptAgent(ctx, agentID, prompt)
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
