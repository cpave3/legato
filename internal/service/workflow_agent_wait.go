package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/gitrepo"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// Agent waits (spec LEG-482 §5.3–§5.6, LEG-485). An agent step ends on the
// first of its wait_for signals (workflowengine.EffectiveWaitFor):
//
//   - complete: the agent completes the step from its pane (Complete);
//   - finished: a stage event of the agent's place reached Finished above
//     the step's watermark;
//   - ended: the agent's session ended, not interrupted;
//   - none: the prompt was delivered.
//
// The wait record (signals, place, watermark, session) is written on the
// step's row before its prompt is sent, with the place reset to
// Implementing in the same write, so only a Finished reported afterwards
// counts. Hosts find every wait again from the tables, at start, on stage
// and agent events and on each reconcile (CheckAgentWaits): a bus event
// lost, or a restart, misses nothing. At the signal Legato fills the
// step's outputs (agentOutputs), then records it and advances the run as
// a completion does.

// lastMessageLimit is the most bytes of an agent's last message a step's
// last_message output keeps.
const lastMessageLimit = 4 << 10

// WorkflowErrorTimeout is the error code of a step its timeout ended.
const WorkflowErrorTimeout = "timeout"

// WorkflowErrorOutputsUnavailable is the error code of a step whose
// outputs Legato could not fill at its signal: GitHub did not say which
// pull requests its branches have, and stale ones would mislead a later
// step.
const WorkflowErrorOutputsUnavailable = "outputs_unavailable"

// AgentOutputSources are what fills an agent step's outputs at its signal.
// Each is optional: without one, its outputs are left empty.
type AgentOutputSources struct {
	// Stacks records a clone's Staccato stack (TaskStackService).
	Stacks interface {
		Record(ctx context.Context, clone store.JobClone) error
	}
	// PRs tracks the clone's branches on the task, finds their pull
	// requests and fetches those found before again (a later step marked
	// one ready, or merged it); an error there fails the step.
	PRs interface {
		AddTaskBranches(ctx context.Context, taskID, repo string, branches []string) ([]string, error)
		TrackedBranches(ctx context.Context, taskID string) ([]store.TaskBranch, error)
		DiscoverAndRefreshPRs(ctx context.Context, taskID string) error
	}
	// LastMessage is an agent's newest message, by agent ID; "" when its
	// conversation cannot be read.
	LastMessage func(ctx context.Context, agentID string) string
}

// SetAgentOutputs gives the service what fills agent steps' outputs.
func (s *WorkflowService) SetAgentOutputs(sources AgentOutputSources) { s.agentOutputSources = sources }

// waitRecorded says whether a step began a wait (a step started before
// waits were recorded has none, and keeps the old rules).
func waitRecorded(step store.WorkflowStep) bool {
	return step.WaitSignals != "" && step.WaitSignals != "[]"
}

func waitSignals(step store.WorkflowStep) []string {
	var signals []string
	_ = json.Unmarshal([]byte(step.WaitSignals), &signals)
	return signals
}

// beginsWait says whether a started step waits on an agent: an agent.stop
// waits only for ended.
func beginsWait(step ResolvedWorkflowStep) bool {
	if len(step.WaitFor) == 0 {
		return false
	}
	return step.Type != "agent.stop" || slices.Contains(step.WaitFor, workflowengine.SignalEnded)
}

// beginAgentWait writes a started agent step's wait record before its
// prompt is sent: the place it watches when it is known now (a task's
// agent, the agent a message goes to), its watermark, and the place reset
// to Implementing when an agent already runs there.
func (s *WorkflowService) beginAgentWait(ctx context.Context, step *ResolvedWorkflowStep) error {
	wait := store.WorkflowWait{Signals: step.WaitFor, Reason: "workflow step " + step.StepID}
	running := false
	switch {
	case step.Agent != nil:
		wait.Place = TaskPlace(step.Agent.TaskID, "")
		if step.Agent.Session != nil {
			wait.Place, running = SessionPlace(*step.Agent.Session), true
		}
	case step.Type == "agent.message" || step.Type == "agent.stop":
		if taskID, err := interviewTaskID(step.With["task"]); err == nil {
			wait.Place = TaskPlace(taskID, "")
		}
	case workflowengine.AgentOnTask(step.With):
		if taskID, err := interviewTaskID(step.With["on"]); err == nil {
			wait.Place = TaskPlace(taskID, "")
		}
	default:
		if taskID, _ := stepAgentTask(step.With); taskID != "" {
			slot, _ := stepAgentSlot(step.With, taskID)
			wait.Place = TaskPlace(taskID, slot)
		}
	}
	if wait.Place.TaskID != "" && !running {
		_, err := s.store.GetAgentSessionInSlot(ctx, wait.Place.TaskID, wait.Place.Slot)
		running = err == nil
	}
	wait.Reset = running && slices.Contains(step.WaitFor, workflowengine.SignalFinished)
	if _, err := s.store.BeginWorkflowWait(ctx, step.RunID, step.StepID, wait); err != nil {
		return fmt.Errorf("begin the step's wait: %w", err)
	}
	if wait.Reset && s.bus != nil {
		s.bus.Publish(events.Event{Type: events.EventAgentStageChanged, At: time.Now(), Payload: events.AgentStageChangedPayload{
			TaskID: wait.Place.TaskID, Slot: wait.Place.Slot, ExecutionID: wait.Place.PlaceExecutionID,
			Stage: store.WorkStageImplementing, Reason: wait.Reason, Source: store.WorkStageSourceWorkflow,
		}})
	}
	return nil
}

// CheckAgentWaits looks at every step waiting on an agent for one of its
// signals, from the tables, and ends those that have one.
func (s *WorkflowService) CheckAgentWaits(ctx context.Context) error {
	steps, err := s.store.ListWorkflowAgentWaits(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, step := range steps {
		if err := s.checkAgentWait(ctx, step); err != nil {
			errs = append(errs, fmt.Errorf("run %s step %s: %w", step.RunID, step.Path, err))
		}
	}
	return errors.Join(errs...)
}

// checkAgentWaitAt checks one step's wait, just begun.
func (s *WorkflowService) checkAgentWaitAt(ctx context.Context, runID, path string) error {
	view, err := s.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		return err
	}
	for _, step := range view.Steps {
		if step.Path == path && step.Status == "waiting_agent" && waitRecorded(step) {
			return s.checkAgentWait(ctx, step)
		}
	}
	return nil
}

func (s *WorkflowService) checkAgentWait(ctx context.Context, step store.WorkflowStep) error {
	attempt, err := s.waitingAttempt(ctx, step)
	if err != nil || attempt == nil {
		return err
	}
	signals := waitSignals(step)
	place := store.WorkStagePlace{TaskID: step.WaitTaskID, Slot: step.WaitSlot, PlaceExecutionID: step.WaitExecutionID}
	finished := func(place store.WorkStagePlace) (bool, error) {
		if place.TaskID == "" || !slices.Contains(signals, workflowengine.SignalFinished) {
			return false, nil
		}
		event, err := s.store.WorkStageEventAbove(ctx, place, step.WaitWatermark, store.WorkStageFinished)
		return event != nil, err
	}
	if slices.Contains(signals, workflowengine.SignalNone) {
		return s.resolveAgentWait(ctx, step, *attempt, workflowengine.SignalNone)
	}
	// A resume that continued the attempt (a workflow's own agent) names
	// its new session on the attempt: the wait follows it.
	if attempt.AgentSessionID != nil && (step.WaitSessionID == nil || *attempt.AgentSessionID != *step.WaitSessionID) {
		step.WaitSessionID = attempt.AgentSessionID
	}
	live, ended, err := s.waitedAgent(ctx, step)
	if err != nil {
		return err
	}
	bound := place
	if live != nil && (step.WaitSessionID == nil || *step.WaitSessionID != live.ID || SessionPlace(*live) != place) {
		// The agent came back (a resume), or the place has a new one: the
		// wait is the place's, so it follows.
		bound = SessionPlace(*live)
		if err := s.store.BindWorkflowWait(ctx, step.RunID, step.Path, attempt.ID, bound, live.ID); err != nil {
			return err
		}
		step.WaitTaskID, step.WaitSlot, step.WaitExecutionID, step.WaitSessionID = bound.TaskID, bound.Slot, bound.PlaceExecutionID, &live.ID
		attempt.AgentSessionID = &live.ID
	}
	for _, at := range []store.WorkStagePlace{place, bound} {
		if done, err := finished(at); err != nil || done {
			if err != nil {
				return err
			}
			return s.resolveAgentWait(ctx, step, *attempt, workflowengine.SignalFinished)
		}
	}
	if !ended {
		return nil
	}
	if slices.Contains(signals, workflowengine.SignalEnded) {
		return s.resolveAgentWait(ctx, step, *attempt, workflowengine.SignalEnded)
	}
	if !plainAgentWait(step) {
		// A workflow's own agent that went without completing keeps the
		// old rules: the step needs the operator (Show).
		return nil
	}
	if err := s.failRecorded(ctx, step.RunID, step.Path, attempt.ID, errors.New("the agent ended before the step completed")); err != nil {
		if errors.Is(err, store.ErrWorkflowStateConflict) {
			return nil
		}
		return err
	}
	return nil
}

// plainAgentWait says whether a step waits on an agent no workflow owns:
// an agent on a task, or the agent a message or a stop went to.
func plainAgentWait(step store.WorkflowStep) bool {
	if step.ExecutorType != "agent.run" {
		return true
	}
	var with map[string]any
	_ = json.Unmarshal([]byte(step.ResolvedInputsJSON), &with)
	return workflowengine.AgentOnTask(with)
}

// waitingAttempt is a step's latest attempt while it waits on its agent.
func (s *WorkflowService) waitingAttempt(ctx context.Context, step store.WorkflowStep) (*store.WorkflowStepAttempt, error) {
	view, err := s.store.GetWorkflowRun(ctx, step.RunID)
	if err != nil {
		return nil, err
	}
	for i := range view.Attempts {
		attempt := &view.Attempts[i]
		if attempt.Path == step.Path && attempt.AttemptNumber == step.LatestAttempt && attempt.Status == "waiting_agent" {
			return attempt, nil
		}
	}
	return nil, nil
}

// waitedAgent is the running agent a wait watches: its session, else the
// running agent of its place. ended says the agent has ended for good; an
// interrupted one has not, since a resume brings it back to the place.
func (s *WorkflowService) waitedAgent(ctx context.Context, step store.WorkflowStep) (*store.AgentSession, bool, error) {
	if step.WaitSessionID != nil {
		session, err := s.store.GetAgentSessionByID(ctx, *step.WaitSessionID)
		switch {
		case err == nil && session.Status == "running":
			return session, false, nil
		case err == nil && session.Status == store.AgentSessionInterrupted:
			return nil, false, nil
		case err != nil && !errors.Is(err, store.ErrNotFound):
			return nil, false, err
		}
	}
	if step.WaitTaskID == "" {
		// Not bound to an agent yet.
		return nil, step.WaitSessionID != nil, nil
	}
	running, err := s.store.GetAgentSessionInSlot(ctx, step.WaitTaskID, step.WaitSlot)
	if err == nil {
		return running, false, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, false, err
	}
	latest, err := s.store.LatestAgentSessionInSlot(ctx, step.WaitTaskID, step.WaitSlot)
	if err == nil && latest.Status == store.AgentSessionInterrupted {
		return nil, false, nil
	}
	return nil, true, nil
}

// resolveAgentWait ends a step's wait on signal: it fills the step's
// outputs, records it, and advances the run (in a host). Of the hosts that
// see the same signal, the record's claim on the attempt lets one through.
func (s *WorkflowService) resolveAgentWait(ctx context.Context, step store.WorkflowStep, attempt store.WorkflowStepAttempt, signal string) error {
	outputs, err := s.agentOutputs(ctx, step, attempt, signal)
	if err != nil {
		if failErr := s.failRecorded(ctx, step.RunID, step.Path, attempt.ID, outputsUnavailable(err)); failErr != nil {
			if errors.Is(failErr, store.ErrWorkflowStateConflict) {
				return nil
			}
			return failErr
		}
		return nil
	}
	// Outputs the agent declared and filled win over Legato's.
	var declared map[string]any
	_ = json.Unmarshal([]byte(step.OutputsJSON), &declared)
	for key, value := range declared {
		outputs[key] = value
	}
	encoded, _ := json.Marshal(outputs)
	at, err := s.loadExecution(ctx, step.RunID, step.Path)
	if err != nil {
		return err
	}
	if err := s.store.RecordWorkflowStepCompletion(ctx, step.RunID, step.Path, attempt.ID, string(encoded), at.next); err != nil {
		if errors.Is(err, store.ErrWorkflowStateConflict) {
			return nil
		}
		return err
	}
	s.changed(ctx, step.RunID, step.Path, WorkflowChangeStepCompleted)
	if s.recordOnly {
		return nil
	}
	_, err = s.advanceRecorded(ctx, step.RunID, step.Path)
	return err
}

// agentOutputs are what Legato fills on an agent step at its signal (spec
// LEG-482 §5.6): the signal, the agent's stage and last message, its
// session, task and folder, and for a job clone its branch, its stack
// (recorded now) and the pull requests of its branches (found or fetched
// again now). A
// GitHub error is returned with what was filled, without prs.
func (s *WorkflowService) agentOutputs(ctx context.Context, step store.WorkflowStep, attempt store.WorkflowStepAttempt, signal string) (map[string]any, error) {
	taskID := firstNonEmpty(step.WaitTaskID, attempt.TaskID)
	out := map[string]any{
		"signal": signal, "stage": "", "stage_reason": "", "last_message": "", "session_id": 0, "task": taskID,
		"working_dir": attempt.WorkingDir, "clone_id": 0, "branch": "", "top_branch": "", "stack": []any{}, "prs": []any{},
	}
	sessionID := attempt.AgentSessionID
	if step.WaitSessionID != nil {
		sessionID = step.WaitSessionID
	}
	var session *store.AgentSession
	if sessionID != nil {
		out["session_id"] = *sessionID
		if found, err := s.store.GetAgentSessionByID(ctx, *sessionID); err == nil {
			session = found
			if found.WorkingDir != "" {
				out["working_dir"] = found.WorkingDir
			}
		}
	}
	place := store.WorkStagePlace{TaskID: step.WaitTaskID, Slot: step.WaitSlot, PlaceExecutionID: step.WaitExecutionID}
	if place.TaskID == "" && session != nil {
		place = SessionPlace(*session)
	}
	if place.TaskID != "" {
		if stage, err := s.store.GetWorkStage(ctx, place); err == nil && stage != nil {
			out["stage"], out["stage_reason"] = stage.Stage, stage.Reason
		}
	}
	if session != nil && session.Status == "running" && s.agentOutputSources.LastMessage != nil {
		out["last_message"] = capBytes(s.agentOutputSources.LastMessage(ctx, AgentSlotKey(session.TaskID, session.Slot)), lastMessageLimit)
	}
	workingDir, _ := out["working_dir"].(string)
	if workingDir == "" {
		return out, nil
	}
	branch, _, _ := gitrepo.Head(workingDir)
	out["branch"], out["top_branch"] = branch, branch
	clone, err := s.store.JobCloneByPath(ctx, workingDir)
	if err != nil {
		return out, nil
	}
	out["clone_id"] = clone.ID
	var stack []TaskStackBranch
	if s.agentOutputSources.Stacks != nil {
		// Only logged: recording reads the clone's own files (a stack it
		// cannot read is recorded with its problem, not returned), so its
		// errors are writes to Legato's database, and the HEAD branch's pull
		// request is still looked up below.
		if err := s.agentOutputSources.Stacks.Record(ctx, *clone); err != nil {
			log.Printf("workflow run %s step %s: recording the stack of %s: %v", step.RunID, step.Path, clone.Path, err)
		}
	}
	if recorded, err := s.store.CloneStackByID(ctx, clone.ID); err == nil {
		_ = json.Unmarshal([]byte(recorded.Branches), &stack)
	}
	shown := make([]map[string]any, 0, len(stack))
	names := make([]string, 0, len(stack)+1)
	for _, b := range stack {
		shown = append(shown, map[string]any{"name": b.Name, "parent": b.Parent, "state": b.State, "head_sha": b.HeadSHA})
		names = append(names, b.Name)
	}
	out["stack"] = shown
	if branch != "" {
		out["top_branch"] = topOfStack(stack, branch)
		if !slices.Contains(names, branch) {
			names = append(names, branch)
		}
	}
	if s.agentOutputSources.PRs == nil || len(names) == 0 {
		return out, nil
	}
	prs := s.agentOutputSources.PRs
	if _, err := prs.AddTaskBranches(ctx, taskID, clone.Repo, names); err != nil {
		return out, fmt.Errorf("tracking the clone's branches: %w", err)
	}
	if err := prs.DiscoverAndRefreshPRs(ctx, taskID); err != nil {
		return out, fmt.Errorf("finding the pull requests of the clone's branches: %w", err)
	}
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return out, err
	}
	tracked, err := prs.TrackedBranches(ctx, taskID)
	if err != nil {
		return out, err
	}
	found := []map[string]any{}
	for _, pr := range TaskPullRequests(task, tracked) {
		if pr.Repo == clone.Repo && slices.Contains(names, pr.Branch) {
			found = append(found, map[string]any{"repo": pr.Repo, "number": pr.PRNumber, "url": pr.PRURL, "state": pr.State, "draft": pr.IsDraft, "branch": pr.Branch})
		}
	}
	out["prs"] = found
	return out, nil
}

// topOfStack is the top of the stack a branch is in: from the branch up
// through the first child each time; the branch itself when nothing is
// stacked on it.
func topOfStack(stack []TaskStackBranch, branch string) string {
	top := branch
	for range stack {
		i := slices.IndexFunc(stack, func(b TaskStackBranch) bool { return b.Parent == top })
		if i < 0 {
			break
		}
		top = stack[i].Name
	}
	return top
}

func capBytes(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	text = text[:limit]
	for !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text
}

// fillRecordedOutputs fills the outputs of a step its agent completed from
// its pane, which recorded only what the agent declared: the host does it
// before it moves the run on. When they cannot be filled (GitHub did not
// say which pull requests the branches have), the step fails as at any
// other signal, with error code WorkflowErrorOutputsUnavailable, since a
// later step reading stale pull requests could skip a review; its
// on_failure decides. It says whether the advance goes on: not when the
// step failed and its run stopped at it (fail, pause), nor when another
// host changed it first. A continued step goes on as a failed one.
func (s *WorkflowService) fillRecordedOutputs(ctx context.Context, advance *store.WorkflowAdvance) (bool, error) {
	if !waitRecorded(advance.Step) {
		return true, nil
	}
	var declared map[string]any
	_ = json.Unmarshal([]byte(advance.Step.OutputsJSON), &declared)
	if declared == nil {
		declared = map[string]any{}
	}
	if _, filled := declared["signal"]; filled {
		return true, nil
	}
	outputs, err := s.agentOutputs(ctx, advance.Step, advance.Attempt, workflowengine.SignalComplete)
	if err != nil {
		failed, goesOn, failErr := s.failUnfilled(ctx, advance, err)
		if failed || failErr != nil {
			return goesOn, failErr
		}
		log.Printf("workflow run %s step %s: %v; no later step will read it, so its prs output is left out", advance.Step.RunID, advance.Step.Path, err)
		delete(outputs, "prs")
	}
	for key, value := range declared {
		outputs[key] = value
	}
	encoded, _ := json.Marshal(outputs)
	if err := s.store.MergeWorkflowStepOutputs(ctx, advance.Step.RunID, advance.Step.Path, string(encoded)); err != nil {
		log.Printf("workflow run %s step %s: filling its outputs: %v", advance.Step.RunID, advance.Step.Path, err)
		return true, nil
	}
	advance.Step.OutputsJSON = string(encoded)
	return true, nil
}

// outputsUnfilled says a step of the run was completed by its agent and
// awaits the outputs a host fills (fillRecordedOutputs): until then the
// run's own outputs, which may read them, are not final.
func outputsUnfilled(view *store.WorkflowRunView) bool {
	for _, step := range view.Steps {
		if !step.AdvanceDue || step.Status != "succeeded" || !waitRecorded(step) {
			continue
		}
		var outputs map[string]any
		_ = json.Unmarshal([]byte(step.OutputsJSON), &outputs)
		if _, filled := outputs["signal"]; !filled {
			return true
		}
	}
	return false
}

// outputsUnavailable is the failure of a step whose outputs could not be
// filled.
func outputsUnavailable(cause error) *WorkflowStepError {
	return &WorkflowStepError{Code: WorkflowErrorOutputsUnavailable, Message: "filling the step's outputs: " + cause.Error(), Err: cause}
}

// failUnfilled fails a recorded step whose outputs could not be filled
// (fillRecordedOutputs), stopping its agent as Fail does, and says whether
// the step failed (here or by another host) and whether the advance goes
// on. It does not fail the last step of a run, whose record ended the run,
// nor one whose run has left the step after it (cancelled, say): no step
// will read its outputs, so they are recorded without prs.
func (s *WorkflowService) failUnfilled(ctx context.Context, advance *store.WorkflowAdvance, cause error) (failed, goesOn bool, err error) {
	step := advance.Step
	at, err := s.loadExecution(ctx, step.RunID, step.Path)
	if err != nil {
		return false, false, err
	}
	if at.view.Run.Status == "succeeded" {
		// The record of the run's last step ended the run.
		return false, true, nil
	}
	failure := outputsUnavailable(cause)
	mode, err := s.store.FailRecordedWorkflowStep(ctx, step.RunID, step.Path, advance.Attempt.ID, stepFailure(failure))
	if errors.Is(err, store.ErrWorkflowStateConflict) {
		// Another host failed it first, or the run has moved on.
		view, err := s.store.GetWorkflowRun(ctx, step.RunID)
		if err != nil {
			return false, false, err
		}
		for _, current := range view.Steps {
			if current.Path == step.Path && current.Status != "succeeded" {
				return true, false, nil
			}
		}
		return false, true, nil
	}
	if err != nil {
		return false, false, err
	}
	if executor := s.executors[advance.Attempt.ExecutorType]; executor != nil {
		stop := executor.Cancel
		if failer, ok := executor.(WorkflowStepFailer); ok {
			stop = failer.FailStep
		}
		if err := stop(ctx, advance.Attempt); err != nil {
			log.Printf("workflow run %s step %s: stopping the failed step's agent: %v", step.RunID, step.Path, err)
		}
	}
	if mode == store.FailurePaused {
		s.changed(ctx, step.RunID, step.Path, WorkflowChangeStepPaused)
		return true, false, nil
	}
	s.changed(ctx, step.RunID, step.Path, WorkflowChangeStepFailed)
	if mode != store.FailureContinued {
		return true, false, nil
	}
	encoded, _ := json.Marshal(store.FailureOutputs(stepFailure(failure)))
	advance.Step.Status, advance.Step.Error, advance.Step.OutputsJSON = "failed", failure.Message, string(encoded)
	return true, true, nil
}

// endedSignal says whether a recorded step ended because its agent did:
// there is no agent left to finish.
func endedSignal(step store.WorkflowStep) bool {
	var outputs map[string]any
	_ = json.Unmarshal([]byte(step.OutputsJSON), &outputs)
	return outputs["signal"] == workflowengine.SignalEnded
}

// timeoutClaimStale is how long a claimed timeout is another host's: a
// step still under way that long after its claim is swept again.
const timeoutClaimStale = 2 * time.Minute

// SweepTimeouts times out every step under way whose deadline is at or
// before now. Each deadline is claimed until timeoutClaimStale after now,
// so of the hosts that sweep it one times the step out, and a claim that
// did not end the step is taken again. It returns how many it timed out.
func (s *WorkflowService) SweepTimeouts(ctx context.Context, now time.Time) (int, error) {
	steps, err := s.store.ListWorkflowStepsPastDeadline(ctx, now)
	if err != nil {
		return 0, err
	}
	timedOut := 0
	var errs []error
	for _, step := range steps {
		claimed, err := s.store.ClaimWorkflowStepTimeout(ctx, step.RunID, step.Path, *step.DeadlineAt, now.Add(timeoutClaimStale))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !claimed {
			continue
		}
		if err := s.timeoutStep(ctx, step); err != nil {
			errs = append(errs, fmt.Errorf("run %s step %s: %w", step.RunID, step.Path, err))
			continue
		}
		timedOut++
	}
	return timedOut, errors.Join(errs...)
}

// timeoutStep ends a step its timeout ran out on (error code
// WorkflowErrorTimeout): a workflow's own agent is stopped by its
// executor's Cancel, a task's agent is left alone, and the step fails.
func (s *WorkflowService) timeoutStep(ctx context.Context, step store.WorkflowStep) error {
	var definition workflowengine.Step
	_ = json.Unmarshal([]byte(step.DefinitionJSON), &definition)
	view, err := s.store.GetWorkflowRun(ctx, step.RunID)
	if err != nil {
		return err
	}
	for _, attempt := range view.Attempts {
		if attempt.Path != step.Path || attempt.AttemptNumber != step.LatestAttempt {
			continue
		}
		reason := "timed out after " + firstNonEmpty(definition.Timeout, "its timeout")
		failure := &WorkflowStepError{Code: WorkflowErrorTimeout, Message: reason}
		if _, err := s.failWith(ctx, step.RunID, step.Path, attempt.ID, failure); err != nil {
			if errors.Is(err, store.ErrWorkflowStateConflict) {
				return nil
			}
			return err
		}
		// A call's child is cancelled only after the step's failure is
		// written, as Cancel orders it: the child's end then settles
		// nothing, and the step's on_failure has decided (LEG-489).
		for _, row := range view.Steps {
			if row.Path != step.Path || row.ChildRunID == "" {
				continue
			}
			if _, err := s.Cancel(ctx, row.ChildRunID, "its workflow.run step "+step.Path+" of "+step.RunID+" timed out"); err != nil && !errors.Is(err, store.ErrWorkflowStateConflict) {
				return err
			}
		}
		return nil
	}
	return nil
}
