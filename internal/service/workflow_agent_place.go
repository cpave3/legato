package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// Where a workflow's agent lives (spec LEG-482 §5.1, LEG-607). An agent.run
// step with on: <task> works through the task's own agent: a plain agent in
// its main slot, started as an operator's spawn would be (no workflow run of
// its own, so its stage is the card's), and kept once its outputs are in
// unless stop: true. With reuse: true a running one is messaged instead.
// agent.dispatch is read as the same with reuse: true, without waiting.
// agent.message and agent.stop reach the agent an earlier step used
// ({from: <step>}) or a task's own.

// WorkflowMessageTag opens every message Legato composes for a workflow's
// agent steps. A macro is sent untagged: it is the operator's own words.
const WorkflowMessageTag = "[legato workflow] "

// WorkflowStepAgent is the agent an agent: {from: <step>} names: the agent
// that earlier step used, which the service finds before the step starts.
type WorkflowStepAgent struct {
	// Step is the step named.
	Step string
	// AgentID addresses the agent (a task ID, or <task>_<slot>).
	AgentID string
	TaskID  string
	// Session is the agent's running session, or nil once it has ended.
	Session *store.AgentSession
}

// taskAgentRunner is a runner that can find a slot's running agent.
type taskAgentRunner interface {
	AgentSessionInSlot(ctx context.Context, taskID, slot string) (*store.AgentSession, error)
}

// AgentSessionInSlot is the running agent of one of taskID's slots.
func (r *WorkflowAgentRunner) AgentSessionInSlot(ctx context.Context, taskID, slot string) (*store.AgentSession, error) {
	return r.store.GetAgentSessionInSlot(ctx, taskID, slot)
}

// AttemptHasTaskAgent says whether an attempt waits on a plain task agent
// (on: <task>), which the workflow never stops on a cancel. The step's
// recorded with says so, so it holds after the operator has replaced the
// agent the attempt started (a restart, or a stop and a new spawn).
func (r *WorkflowAgentRunner) AttemptHasTaskAgent(ctx context.Context, attempt store.WorkflowStepAttempt) bool {
	if view, err := r.store.GetWorkflowRun(ctx, attempt.RunID); err == nil {
		for _, step := range view.Steps {
			var with map[string]any
			if step.Path == attempt.Path && json.Unmarshal([]byte(step.ResolvedInputsJSON), &with) == nil && with != nil {
				return workflowengine.AgentOnTask(with)
			}
		}
	}
	if attempt.AgentSessionID == nil {
		return false
	}
	session, err := r.store.GetAgentSessionByID(ctx, *attempt.AgentSessionID)
	return err == nil && plainTaskAgent(*session)
}

// plainTaskAgent says whether a session is an agent no workflow owns: one
// an operator started, or an agent.run step on a task.
func plainTaskAgent(session store.AgentSession) bool {
	return session.WorkflowRunID == "" && session.ExecutionKind != store.ExecutionWorkflowAgent
}

// startOnTask starts an agent.run step on a task's own agent. wait is false
// for agent.dispatch, which succeeds once the agent has its work.
func (e *AgentRunExecutor) startOnTask(ctx context.Context, step ResolvedWorkflowStep, taskID string, reuse, wait bool) (WorkflowExecutionResult, error) {
	macro, fromMacro, err := stepMacro(ctx, e.macros, step.With)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	var work string
	if fromMacro {
		// As a macro started on the card is sent: its key and title first.
		if work, err = e.macros.Message(ctx, macro.Keys, taskID, stepPR(step.With)); err != nil {
			return WorkflowExecutionResult{}, err
		}
	} else if work, err = workflowString(step.With, "prompt", true); err != nil {
		return WorkflowExecutionResult{}, err
	}
	workingDir, err := workflowString(step.With, "working_dir", false)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	session, err := e.taskAgent(ctx, taskID)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if session != nil {
		if !reuse {
			return WorkflowExecutionResult{}, fmt.Errorf("task %s already has an agent running: reuse: true messages it instead", taskID)
		}
		if err := e.messageTaskAgent(ctx, step, taskID, work, fromMacro, wait); err != nil {
			return WorkflowExecutionResult{}, err
		}
		return taskAgentResult(session, taskID, workingDir, false, wait), nil
	}
	assignment := work
	if wait {
		assignment = buildWorkflowAssignment(work, step, e.binaryPath)
	}
	kind, tier := stepAgent(step.With, macro)
	// A plain agent of the task, as an operator's spawn: no workflow run or
	// attempt of its own, so its stage is the card's and it outlives the
	// step. The contract names the step it completes.
	opts := AgentSpawnOptions{
		AgentKind: kind, Tier: tier, WorkingDir: workingDir, Assignment: assignment,
		ExtraEnv: map[string]string{},
	}
	// agent.dispatch's repo is only the pull request a macro reads, as it
	// always was: its agent starts in working_dir, never in a clone.
	if wait {
		repo, _ := workflowString(step.With, "repo", false)
		ref, _ := workflowString(step.With, "ref", false)
		branch, _ := workflowString(step.With, "branch", false)
		engram, err := workflowString(step.With, "engram", false)
		if err != nil {
			return WorkflowExecutionResult{}, err
		}
		opts.Repo, opts.Ref, opts.Branch = strings.TrimSpace(repo), strings.TrimSpace(ref), strings.TrimSpace(branch)
		opts.Engram, opts.EngramRequired = strings.TrimSpace(engram), strings.TrimSpace(engram) != ""
		stepCloneOptions(step, taskID, &opts)
	}
	if session, err = e.agents.SpawnWorkflowAgent(ctx, taskID, "", opts); err != nil {
		return WorkflowExecutionResult{}, fmt.Errorf("start the agent of task %s: %w", taskID, err)
	}
	e.grantBranch(ctx, step, taskID, opts, session)
	return taskAgentResult(session, taskID, workingDir, true, wait), nil
}

// taskAgent is the running agent in a task's main slot, or nil.
func (e *AgentRunExecutor) taskAgent(ctx context.Context, taskID string) (*store.AgentSession, error) {
	finder, ok := e.agents.(taskAgentRunner)
	if !ok {
		return nil, nil
	}
	session, err := finder.AgentSessionInSlot(ctx, taskID, store.SlotMain)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find the agent of task %s: %w", taskID, err)
	}
	return session, nil
}

// messageTaskAgent gives a running task agent the step's work: a prompt
// tagged with its contract, or a macro as the operator's words, then the
// contract tagged on its own.
func (e *AgentRunExecutor) messageTaskAgent(ctx context.Context, step ResolvedWorkflowStep, taskID, work string, fromMacro, wait bool) error {
	contract := ""
	if wait {
		contract = workflowContract(step, e.binaryPath)
	}
	if !fromMacro {
		if contract != "" {
			work += "\n\n" + contract
		}
		return e.agents.PromptAgent(ctx, taskID, WorkflowMessageTag+work)
	}
	if err := e.agents.PromptAgent(ctx, taskID, work); err != nil {
		return err
	}
	if contract == "" {
		return nil
	}
	return e.agents.PromptAgent(ctx, taskID, WorkflowMessageTag+contract)
}

func taskAgentResult(session *store.AgentSession, taskID, workingDir string, created, wait bool) WorkflowExecutionResult {
	if wait {
		return WorkflowExecutionResult{Status: "waiting_agent", AgentSession: session, WorkingDir: workingDir, TaskID: taskID}
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{
		"agent_session_id": int64(session.ID), "created": created, "working_dir": workingDir,
	}}
}

// AgentMessageExecutor sends a prompt or a macro to a running agent: the one
// an earlier step used, or a task's own. It does not wait for the agent.
type AgentMessageExecutor struct {
	agents     workflowAgentRunner
	store      *store.Store
	macros     workflowMacros
	binaryPath string
}

func NewAgentMessageExecutor(agents workflowAgentRunner, s *store.Store) *AgentMessageExecutor {
	return &AgentMessageExecutor{agents: agents, store: s}
}

// SetMacros lets steps name a macro (with.macro) in place of a prompt.
func (e *AgentMessageExecutor) SetMacros(macros workflowMacros) { e.macros = macros }

func (e *AgentMessageExecutor) Type() string                                            { return "agent.message" }
func (e *AgentMessageExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

func (e *AgentMessageExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	target, err := e.target(ctx, step)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	macro, fromMacro, err := stepMacro(ctx, e.macros, step.With)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	var message string
	if fromMacro {
		if message, err = e.macros.Message(ctx, macro.Keys, target.TaskID, stepPR(step.With)); err != nil {
			return WorkflowExecutionResult{}, err
		}
	} else {
		prompt, err := workflowString(step.With, "prompt", true)
		if err != nil {
			return WorkflowExecutionResult{}, err
		}
		message = WorkflowMessageTag + prompt
	}
	// The step's contract names what it waits for: tagged with a prompt,
	// tagged on its own after a macro, the operator's words.
	contract := messageContract(step, e.binaryPath)
	if contract != "" && !fromMacro {
		message += "\n\n" + contract
	}
	if err := e.agents.PromptAgent(ctx, target.AgentID, message); err != nil {
		return WorkflowExecutionResult{}, fmt.Errorf("message the agent: %w", err)
	}
	if contract != "" && fromMacro {
		if err := e.agents.PromptAgent(ctx, target.AgentID, WorkflowMessageTag+contract); err != nil {
			return WorkflowExecutionResult{}, fmt.Errorf("message the agent: %w", err)
		}
	}
	// The service ends the step on its signal (none: at once), with the
	// outputs it fills.
	return WorkflowExecutionResult{Status: "waiting_agent", AgentSession: target.Session, TaskID: target.TaskID}, nil
}

// SetBinaryPath names the legato binary the contract's commands run.
func (e *AgentMessageExecutor) SetBinaryPath(path string) { e.binaryPath = path }

// target is the running agent a message goes to.
func (e *AgentMessageExecutor) target(ctx context.Context, step ResolvedWorkflowStep) (*WorkflowStepAgent, error) {
	if step.Agent != nil {
		if step.Agent.Session == nil {
			return nil, fmt.Errorf("the agent of step %s has ended", step.Agent.Step)
		}
		return step.Agent, nil
	}
	taskID, err := interviewTaskID(step.With["task"])
	if err != nil {
		return nil, err
	}
	session, err := e.store.GetAgentSessionInSlot(ctx, taskID, store.SlotMain)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("task %s has no agent running", taskID)
	}
	if err != nil {
		return nil, err
	}
	return &WorkflowStepAgent{AgentID: taskID, TaskID: taskID, Session: session}, nil
}

// stepAgentFrom is the agent an earlier step used, which a step's agent:
// {from: <step>} names. An agent.message, agent.stop or agent.dispatch step
// is followed to its own agent; an agent.run step's is the session its
// latest attempt waited on.
func (s *WorkflowService) stepAgentFrom(ctx context.Context, view *store.WorkflowRunView, definition *workflowengine.Definition, from string) (*WorkflowStepAgent, error) {
	for range len(definition.Steps) {
		var step *workflowengine.Step
		for i := range definition.Steps {
			if definition.Steps[i].ID == from {
				step = &definition.Steps[i]
			}
		}
		if step == nil {
			return nil, fmt.Errorf("step %s is not in the workflow", from)
		}
		if next, ok := workflowengine.AgentFrom(step.With["agent"]); ok && step.Type != "agent.run" {
			from = next
			continue
		}
		path := workflowengine.StepPath("", from)
		if step.Type != "agent.run" {
			return s.taskAgentOfStep(ctx, view, path, step.Type)
		}
		var latest *store.WorkflowStepAttempt
		for i := range view.Attempts {
			if attempt := &view.Attempts[i]; attempt.Path == path && (latest == nil || attempt.AttemptNumber > latest.AttemptNumber) {
				latest = attempt
			}
		}
		if latest == nil || latest.AgentSessionID == nil {
			return nil, fmt.Errorf("step %s has run no agent", from)
		}
		found := &WorkflowStepAgent{Step: from, TaskID: latest.TaskID}
		session, err := s.store.GetAgentSessionByID(ctx, *latest.AgentSessionID)
		switch {
		case errors.Is(err, store.ErrNotFound):
			return found, nil
		case err != nil:
			return nil, err
		case session.Status == "running":
			found.Session, found.TaskID, found.AgentID = session, session.TaskID, AgentSlotKey(session.TaskID, session.Slot)
		}
		return found, nil
	}
	return nil, fmt.Errorf("step %s: agent: {from} goes round in a loop", from)
}

// taskAgentOfStep is the agent of the task an agent.message, agent.stop or
// agent.dispatch step named with task: that task's main slot.
func (s *WorkflowService) taskAgentOfStep(ctx context.Context, view *store.WorkflowRunView, path, stepType string) (*WorkflowStepAgent, error) {
	for _, instance := range view.Steps {
		if instance.Path != path {
			continue
		}
		var with map[string]any
		if err := json.Unmarshal([]byte(instance.ResolvedInputsJSON), &with); err != nil || with == nil {
			break
		}
		taskID, err := interviewTaskID(with["task"])
		if err != nil {
			return nil, fmt.Errorf("step %s: %w", path, err)
		}
		found := &WorkflowStepAgent{Step: path, AgentID: taskID, TaskID: taskID}
		session, err := s.store.GetAgentSessionInSlot(ctx, taskID, store.SlotMain)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		if err == nil {
			found.Session = session
		}
		return found, nil
	}
	return nil, fmt.Errorf("step %s (%s) has not run", path, stepType)
}

// resolveStepAgent fills a step's Agent from its agent: {from: <step>}.
func (s *WorkflowService) resolveStepAgent(ctx context.Context, definition *workflowengine.Definition, runID string, step *ResolvedWorkflowStep) error {
	if step.Type != "agent.message" && step.Type != "agent.stop" {
		return nil
	}
	from, ok := workflowengine.AgentFrom(step.With["agent"])
	if !ok {
		return nil
	}
	view, err := s.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		return err
	}
	agent, err := s.stepAgentFrom(ctx, view, definition, from)
	if err != nil {
		return fmt.Errorf("with agent: %w", err)
	}
	agent.Step = from
	step.Agent = agent
	return nil
}
