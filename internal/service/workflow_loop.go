package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// Loops (spec LEG-482 §1, LEG-490). The advance follows the run's
// current step, wherever it is in the step tree: a loop's pass's steps run
// as the run's own do, and after a pass's last step the run goes back to
// its loop, which ends the pass (its result), checks until and max, and
// makes the next pass's steps or ends. Everything the loop decides from is
// in the tables (its row's loop_state_json, its pass's rows), so any host
// goes on with a run another started, after a restart too.

// WorkflowChangeLoopPass is the change a loop publishes when it starts a
// pass.
const WorkflowChangeLoopPass = "loop_pass"

// runAt is a run at one of its step instances: what running, completing
// or skipping the step needs.
type runAt struct {
	view       *store.WorkflowRunView
	definition *workflowengine.Definition
	inputs     map[string]any
	// path is the step instance's, step its definition (a next: loop's
	// query is made from the loop), row its instance.
	path string
	step *workflowengine.Step
	row  store.WorkflowStep
	// prior are the outputs the step reads (the nearest step of each ID
	// before it), loop the loop context in a pass.
	prior map[string]map[string]any
	loop  map[string]any
	// next is where the run goes after the step (NextPath).
	next string
	rows map[string]store.WorkflowStep
}

// loadExecution loads a run at one of its steps, by path ("" for the step
// the run is at).
func (s *WorkflowService) loadExecution(ctx context.Context, runID, path string) (*runAt, error) {
	view, err := s.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	var definition workflowengine.Definition
	if err := json.Unmarshal([]byte(view.Run.DefinitionJSON), &definition); err != nil {
		return nil, err
	}
	var inputs map[string]any
	if err := json.Unmarshal([]byte(view.Run.InputsJSON), &inputs); err != nil {
		return nil, err
	}
	if path == "" {
		path = view.Run.CurrentStepID
	}
	at := &runAt{view: view, definition: &definition, inputs: inputs, path: path, rows: make(map[string]store.WorkflowStep, len(view.Steps))}
	for _, row := range view.Steps {
		at.rows[row.Path] = row
	}
	if path == "" {
		return at, nil
	}
	if at.step, err = definition.StepAt(path); err != nil {
		return nil, err
	}
	at.row = at.rows[path]
	levels, err := definition.Scopes(path)
	if err != nil {
		return nil, err
	}
	if at.prior, at.loop, err = scopeOutputs(levels, at.rows); err != nil {
		return nil, err
	}
	if at.next, err = definition.NextPath(path); err != nil {
		return nil, err
	}
	return at, nil
}

// scopeOutputs are what a step in levels reads: the outputs of the steps
// before it in each scope, outermost first, so the nearest step of an ID
// wins (one that did not run hides an enclosing one of its ID), and the
// innermost loop's context.
func scopeOutputs(levels []workflowengine.ScopeLevel, rows map[string]store.WorkflowStep) (map[string]map[string]any, map[string]any, error) {
	prior := map[string]map[string]any{}
	var loop map[string]any
	for _, level := range levels {
		for _, step := range level.Steps[:level.Before] {
			row, ok := rows[workflowengine.StepPath(level.Path, step.ID)]
			// A step that failed before this one was continued: its
			// outputs are the failure (status, error, error_code).
			if !ok || (row.Status != "succeeded" && row.Status != "failed") {
				delete(prior, step.ID)
				continue
			}
			var values map[string]any
			if err := json.Unmarshal([]byte(row.OutputsJSON), &values); err != nil {
				return nil, nil, err
			}
			prior[step.ID] = values
		}
		if level.Loop != "" {
			state, err := workflowengine.ParseLoopState(rows[level.Loop].LoopStateJSON)
			if err != nil {
				return nil, nil, err
			}
			loop = workflowengine.LoopContext(state, level.Pass)
		}
	}
	return prior, loop, nil
}

// scope is what the step's ${{ }} reads: the run's inputs, its task, the
// steps before it and the loop context.
func (s *WorkflowService) scope(ctx context.Context, at *runAt) (workflowengine.Scope, error) {
	task, err := s.store.GetTask(ctx, at.view.Run.TaskID)
	if err != nil {
		return workflowengine.Scope{}, fmt.Errorf("load workflow task %q: %w", at.view.Run.TaskID, err)
	}
	return workflowengine.Scope{Inputs: at.inputs, Task: workflowTaskContext(task), Steps: at.prior, Loop: at.loop}, nil
}

// advance runs a run on from the step it is at: each step in turn, a
// loop's passes included, until a step waits (on an agent, its service or
// the operator), pauses, fails the run, or the run ends. selected is a
// step the operator started (a manual one, or Retry), which does not wait
// for the operator again; force goes with it to worktree.remove.
func (s *WorkflowService) advance(ctx context.Context, runID, selected string, force bool) error {
	for {
		at, err := s.loadExecution(ctx, runID, "")
		if err != nil {
			return err
		}
		if at.view.Run.Status != "running" || at.path == "" {
			return nil
		}
		if at.step.Loop != nil && at.row.Status == "running" {
			moved, err := s.loopTick(ctx, at)
			if err != nil || !moved {
				return err
			}
			continue
		}
		if at.row.Status != "pending" {
			return nil
		}
		stop, err := s.runStep(ctx, at, selected == at.path, force)
		if err != nil || stop {
			return err
		}
	}
}

// ReconcileLoops moves on each run left at a running loop, which only a
// host that died between a pass's end and the loop's next move leaves:
// the loop decides from the tables, and a host moving it at the same time
// wins or loses on the loop's state. The advance loop runs it at its start
// and each reconcile.
func (s *WorkflowService) ReconcileLoops(ctx context.Context) {
	if s.recordOnly {
		return
	}
	runs, err := s.store.ListWorkflowRunsAtLoops(ctx)
	if err != nil {
		log.Printf("workflows: runs at a loop: %v", err)
		return
	}
	for _, runID := range runs {
		if err := s.advance(ctx, runID, "", false); err != nil {
			log.Printf("workflows: moving run %s on from its loop: %v", runID, err)
		}
	}
}

// runStep runs the pending step a run is at. stop is true when the run
// does not go on from here at once: the step waits or paused.
func (s *WorkflowService) runStep(ctx context.Context, at *runAt, triggered, force bool) (stop bool, err error) {
	runID, path, step, definition := at.view.Run.ID, at.path, *at.step, at.definition
	taskID := at.view.Run.TaskID
	scope, err := s.scope(ctx, at)
	if err != nil {
		return true, err
	}
	// if: is checked before anything else: a step it skips leaves its
	// outputs missing.
	runs, resolveErr := definition.StepRunsAt(path, scope)
	if resolveErr == nil && !runs {
		if err := s.store.SkipWorkflowStep(ctx, runID, path, at.next); err != nil {
			return true, err
		}
		s.changed(ctx, runID, path, WorkflowChangeStepSkipped)
		return false, nil
	}
	if resolveErr == nil && step.Loop != nil {
		return s.startLoop(ctx, at, scope)
	}
	// An if: or an expression that cannot be resolved, such as a skipped
	// step's output with no fallback, fails the step below as its
	// executor's error would, so its on_failure applies (LEG-486).
	resolved := &workflowengine.ResolvedStep{With: map[string]any{}}
	if resolveErr == nil && step.Loop == nil {
		if got, err := definition.ResolveStepAt(path, scope); err != nil {
			resolveErr = err
		} else {
			resolved = got
		}
	}
	if resolveErr == nil && triggered && step.Type == "worktree.remove" {
		resolved.With["force"] = force
	}
	// A step that waits for the task's agent to end has nothing to wait
	// for when no agent is running.
	agentGone := step.Start == workflowengine.StartAgentEnded && !s.taskHasRunningAgent(ctx, taskID)
	if resolveErr == nil && step.WaitsForPerson() && !triggered && !agentGone {
		resolvedJSON, _ := json.Marshal(resolved.With)
		if err := s.store.WaitWorkflowStepManual(ctx, runID, path, string(resolvedJSON)); err != nil {
			return true, err
		}
		s.changed(ctx, runID, path, WorkflowChangeStepWaiting)
		return true, nil
	}
	attemptID := workflowID("wa-")
	resolvedJSON, _ := json.Marshal(resolved.With)
	executorType := step.Type
	if step.Loop != nil {
		executorType = workflowengine.LoopStepType
	}
	attempt := store.WorkflowStepAttempt{ID: attemptID, RunID: runID, Path: path, StepID: step.ID, AttemptNumber: at.row.LatestAttempt + 1, ExecutorType: executorType, TaskID: taskID, Status: "running"}
	if err := s.store.StartWorkflowStepAttempt(ctx, attempt, string(resolvedJSON)); err != nil {
		return true, err
	}
	// A timeout's deadline is absolute: time Legato is down counts.
	if timeout, _ := step.TimeoutDuration(); timeout > 0 {
		if err := s.store.SetWorkflowStepDeadline(ctx, runID, path, time.Now().Add(timeout)); err != nil {
			return true, err
		}
	}
	s.changed(ctx, runID, path, WorkflowChangeStepStarted)
	started := ResolvedWorkflowStep{RunID: runID, AttemptID: attemptID, TaskID: taskID, RelatedTaskID: at.view.Run.RelatedTaskID, StepID: path, Title: step.Title, Type: step.Type, With: resolved.With, Outputs: resolved.Outputs,
		SchemaVersion: definition.SchemaVersion, WaitFor: workflowengine.EffectiveWaitFor(step)}
	var result WorkflowExecutionResult
	execErr := resolveErr
	if execErr == nil {
		execErr = s.resolveStepAgent(ctx, definition, runID, &started)
	}
	if execErr == nil {
		execErr = s.resolveStepClone(ctx, at.prior, &started)
	}
	waits := beginsWait(started)
	if execErr == nil && waits {
		execErr = s.beginAgentWait(ctx, &started)
	}
	if execErr == nil {
		result, execErr = s.executors[step.Type].Start(ctx, started)
	}
	if execErr != nil {
		failure := stepFailure(execErr)
		failure.InProcess = true
		mode, err := s.store.FailWorkflowStepWith(ctx, runID, path, attemptID, failure)
		if err != nil {
			return true, execErr
		}
		switch mode {
		case store.FailureContinued, store.FailurePassEnded:
			// The failure is the step's outputs, and the run goes on: in
			// its scope, or back at its loop.
			s.changed(ctx, runID, path, WorkflowChangeStepFailed)
			return false, nil
		case store.FailurePaused:
			s.changed(ctx, runID, path, WorkflowChangeStepPaused)
			return true, nil
		}
		s.changed(ctx, runID, path, WorkflowChangeStepFailed)
		return true, execErr
	}
	if result.Status == "waiting_agent" || result.Status == WorkflowStepWaiting {
		var err error
		if result.Status == "waiting_agent" {
			// The wait is bound to its agent before the step waits, so no
			// host finds it waiting on nothing.
			if waits && result.AgentSession != nil {
				err = s.store.BindWorkflowWait(ctx, runID, path, attemptID, SessionPlace(*result.AgentSession), result.AgentSession.ID)
			}
			if err == nil {
				err = s.store.MarkWorkflowStepWaiting(ctx, runID, path, attemptID, result.AgentSession, result.WorkingDir, result.TaskID)
			}
		} else {
			err = s.store.WaitWorkflowStepOnService(ctx, runID, path, attemptID)
		}
		if err != nil {
			return true, err
		}
		s.changed(ctx, runID, path, WorkflowChangeStepWaiting)
		// What the step waits on starts only now (a call's child), so its
		// end finds the step waiting.
		if waiter, ok := s.executors[step.Type].(workflowStepWaiter); ok && result.Status == WorkflowStepWaiting {
			attempt.Status = WorkflowStepWaiting
			waiter.Waiting(ctx, attempt)
		}
		if waits && result.Status == "waiting_agent" {
			// A signal may be here already (none: the prompt was
			// delivered).
			return true, s.checkAgentWaitAt(ctx, runID, path)
		}
		return true, nil
	}
	if result.Status != "succeeded" {
		return true, fmt.Errorf("executor %q returned invalid status %q", step.Type, result.Status)
	}
	outputsJSON, _ := json.Marshal(result.Outputs)
	if err := s.store.CompleteWorkflowStep(ctx, runID, path, attemptID, string(outputsJSON), at.next); err != nil {
		return true, err
	}
	s.changed(ctx, runID, path, WorkflowChangeStepCompleted)
	return false, nil
}

// startLoop starts a loop step the run has reached: an attempt for it,
// and its state, the over: list read now. A loop retried after it failed
// keeps its passes and goes on from them.
func (s *WorkflowService) startLoop(ctx context.Context, at *runAt, scope workflowengine.Scope) (bool, error) {
	runID, path := at.view.Run.ID, at.path
	attempt := store.WorkflowStepAttempt{ID: workflowID("wa-"), RunID: runID, Path: path, StepID: at.step.ID, AttemptNumber: at.row.LatestAttempt + 1, ExecutorType: workflowengine.LoopStepType, TaskID: at.view.Run.TaskID, Status: "running"}
	if err := s.store.StartWorkflowStepAttempt(ctx, attempt, "{}"); err != nil {
		return true, err
	}
	s.changed(ctx, runID, path, WorkflowChangeStepStarted)
	state, err := workflowengine.ParseLoopState(at.row.LoopStateJSON)
	if err == nil {
		state, err = s.loopStarted(at, scope, state)
	}
	if err != nil {
		// A loop that cannot start fails; its on_failure applies.
		moved, failErr := s.failLoop(ctx, runID, path, attempt.ID, err, nil, nil)
		return !moved, failErr
	}
	if err := s.store.SetWorkflowLoopState(ctx, runID, path, at.row.LoopStateJSON, state.Encode()); err != nil {
		return true, conflictIsNotMoved(err)
	}
	return false, nil
}

// loopStarted is a loop's state as its step starts: the first time, an
// over: loop's list is read; a Retry goes on from its passes.
func (s *WorkflowService) loopStarted(at *runAt, scope workflowengine.Scope, state workflowengine.LoopState) (workflowengine.LoopState, error) {
	var err error
	if !state.Started {
		state.Started = true
		if at.step.Loop.Over != nil {
			state.Items, err = at.definition.LoopItems(at.path, scope)
		}
	}
	state.EndedBy = ""
	return state, err
}

// loopTick moves a run that is at its running loop on: it ends the pass
// that ran (or picks a next: loop's item once its query has run), then
// makes the next pass or ends the loop. moved is false when another host
// moved it first.
func (s *WorkflowService) loopTick(ctx context.Context, at *runAt) (bool, error) {
	runID, path, loop := at.view.Run.ID, at.path, at.step.Loop
	old := at.row.LoopStateJSON
	state, err := workflowengine.ParseLoopState(old)
	if err != nil {
		return false, err
	}
	attemptID := s.latestAttemptID(at.view, path, at.row.LatestAttempt)
	scope, err := s.scope(ctx, at)
	if err != nil {
		return false, err
	}
	if !state.Started {
		// A host stopped between starting the loop's step and writing its
		// state: the loop starts now.
		started, err := s.loopStarted(at, scope, state)
		if err != nil {
			return s.failLoop(ctx, runID, path, attemptID, err, nil, nil)
		}
		if err := s.store.SetWorkflowLoopState(ctx, runID, path, old, started.Encode()); err != nil {
			return false, conflictIsNotMoved(err)
		}
		return true, nil
	}
	if open := state.Open(); open != nil {
		query := at.rows[workflowengine.StepPath(workflowengine.PassPath(path, open.Index), workflowengine.NextStepID)]
		if !open.Body && query.Status == "succeeded" {
			return s.pickNext(ctx, at, scope, state, query)
		}
		status, errorText := passOutcome(at.view, workflowengine.PassPath(path, open.Index))
		passScope, err := s.passScope(at, scope, open.Index)
		if err != nil {
			return false, err
		}
		open.Result = at.definition.PassResult(path, passScope, status, errorText)
		open.Open = false
		if open.Result["status"] == workflowengine.PassFailed && loop.Failure() != workflowengine.OnFailureContinue {
			// Under fail, or a result too big under pause (there is no
			// step to hold the run at), the loop fails, its state written
			// with the failure.
			state.EndedBy = workflowengine.LoopEndedFailure
			return s.failLoop(ctx, runID, path, attemptID, fmt.Errorf("pass %d failed: %v", open.Index, open.Result["error"]), state.Outputs(), &store.LoopStateWrite{Old: old, New: state.Encode()})
		}
		scope.Loop = workflowengine.UntilContext(state)
		if done, err := at.definition.LoopUntil(path, scope); err != nil {
			return s.failLoop(ctx, runID, path, attemptID, err, state.Outputs(), nil)
		} else if done {
			return s.endLoop(ctx, at, old, state, workflowengine.LoopEndedUntil)
		}
	}
	index := len(state.Passes)
	if index >= loop.Limit() {
		return s.endLoop(ctx, at, old, state, workflowengine.LoopEndedMax)
	}
	pass := workflowengine.LoopPass{Index: index, Open: true, Body: loop.Next == nil}
	switch {
	case loop.Over != nil:
		found := false
		for state.Cursor < len(state.Items) && !found {
			item := state.Items[state.Cursor]
			state.Cursor++
			key, err := s.itemKey(at, scope, state, index, item)
			if err != nil {
				// An item the loop cannot tell apart fails it.
				return s.failLoop(ctx, runID, path, attemptID, err, state.Outputs(), nil)
			}
			if loop.Key != "" && state.HasSeen(key) {
				continue
			}
			found, pass.Item, pass.Key = true, item, key
			if loop.Key != "" {
				state.Seen = append(state.Seen, key)
			}
		}
		if !found {
			return s.endLoop(ctx, at, old, state, workflowengine.LoopEndedItems)
		}
	}
	state.Passes = append(state.Passes, pass)
	return s.startPass(ctx, at, old, state, index, loop.Next != nil)
}

// startPass makes a pass's steps (or only a next: loop's query) and moves
// the run to the first.
func (s *WorkflowService) startPass(ctx context.Context, at *runAt, old string, state workflowengine.LoopState, index int, query bool) (bool, error) {
	steps, err := at.definition.PassSteps(at.path, query)
	if err != nil {
		return false, err
	}
	scope := workflowengine.PassPath(at.path, index)
	instances := stepInstances(at.view.Run.ID, scope, &index, steps)
	if query {
		// Before the pass's other steps, which come once it has its item.
		instances[0].StepOrder = -1
	}
	if err := s.store.AdvanceWorkflowLoop(ctx, at.view.Run.ID, at.path, old, state.Encode(), instances, instances[0].Path); err != nil {
		return false, conflictIsNotMoved(err)
	}
	s.changed(ctx, at.view.Run.ID, at.path, WorkflowChangeLoopPass)
	return true, nil
}

// pickNext takes a next: loop's item from its query's items: the first
// whose key no pass has used. With none, the loop ends (next_empty), and
// the pass that asked is not one.
func (s *WorkflowService) pickNext(ctx context.Context, at *runAt, scope workflowengine.Scope, state workflowengine.LoopState, query store.WorkflowStep) (bool, error) {
	old := at.row.LoopStateJSON
	open := state.Open()
	var outputs map[string]any
	decoder := json.NewDecoder(strings.NewReader(query.OutputsJSON))
	decoder.UseNumber()
	_ = decoder.Decode(&outputs)
	items, isList := outputs["items"].([]any)
	if !isList && outputs["items"] != nil {
		// Nothing to take an item from: the pass fails, as its policy says.
		open.Open, open.Body = false, true
		open.Result = map[string]any{"status": workflowengine.PassFailed, "error": "the next: query's items output is not a list"}
		if at.step.Loop.Failure() != workflowengine.OnFailureContinue {
			state.EndedBy = workflowengine.LoopEndedFailure
			return s.failLoop(ctx, at.view.Run.ID, at.path, s.latestAttemptID(at.view, at.path, at.row.LatestAttempt), fmt.Errorf("pass %d: %v", open.Index, open.Result["error"]), state.Outputs(), &store.LoopStateWrite{Old: old, New: state.Encode()})
		}
		return s.startNextPassAfter(ctx, at, old, state)
	}
	for _, item := range items {
		key, err := s.itemKey(at, scope, state, open.Index, item)
		if err != nil {
			return s.failLoop(ctx, at.view.Run.ID, at.path, s.latestAttemptID(at.view, at.path, at.row.LatestAttempt), err, state.Outputs(), nil)
		}
		if state.HasSeen(key) {
			continue
		}
		open.Item, open.Key, open.Body = item, key, true
		state.Seen = append(state.Seen, key)
		steps, err := at.definition.PassSteps(at.path, false)
		if err != nil {
			return false, err
		}
		index := open.Index
		instances := stepInstances(at.view.Run.ID, workflowengine.PassPath(at.path, index), &index, steps)
		if err := s.store.AdvanceWorkflowLoop(ctx, at.view.Run.ID, at.path, old, state.Encode(), instances, instances[0].Path); err != nil {
			return false, conflictIsNotMoved(err)
		}
		s.changed(ctx, at.view.Run.ID, at.path, WorkflowChangeLoopPass)
		return true, nil
	}
	state.Passes = state.Passes[:len(state.Passes)-1]
	return s.endLoop(ctx, at, old, state, workflowengine.LoopEndedNextEmpty)
}

// startNextPassAfter goes on after a pass that ended in pickNext: its
// until is not checked (it has no outputs), max is.
func (s *WorkflowService) startNextPassAfter(ctx context.Context, at *runAt, old string, state workflowengine.LoopState) (bool, error) {
	index := len(state.Passes)
	if index >= at.step.Loop.Limit() {
		return s.endLoop(ctx, at, old, state, workflowengine.LoopEndedMax)
	}
	state.Passes = append(state.Passes, workflowengine.LoopPass{Index: index, Open: true})
	return s.startPass(ctx, at, old, state, index, true)
}

// itemKey is an item's key in pass index.
func (s *WorkflowService) itemKey(at *runAt, scope workflowengine.Scope, state workflowengine.LoopState, index int, item any) (string, error) {
	context := workflowengine.LoopContext(state, index)
	context["item"] = item
	scope.Loop = context
	return at.definition.LoopKey(at.path, scope)
}

// passScope is what a pass's results read once it has ended: its steps'
// outputs and its loop context.
func (s *WorkflowService) passScope(at *runAt, scope workflowengine.Scope, index int) (workflowengine.Scope, error) {
	levels, err := at.definition.PassScopes(at.path, index)
	if err != nil {
		return scope, err
	}
	if scope.Steps, scope.Loop, err = scopeOutputs(levels, at.rows); err != nil {
		return scope, err
	}
	return scope, nil
}

// passOutcome is how a pass ended, from its steps: failed when a step
// failed that did not continue (a next: loop's query included), skipped
// when every step of its body was (or it has none: its query was
// skipped), else succeeded.
func passOutcome(view *store.WorkflowRunView, passPath string) (string, string) {
	body, skipped := 0, 0
	for _, row := range view.Steps {
		if row.ParentPath != passPath {
			continue
		}
		if row.Status == "failed" {
			var definition workflowengine.Step
			_ = json.Unmarshal([]byte(row.DefinitionJSON), &definition)
			if definition.OnFailure != workflowengine.OnFailureContinue {
				return workflowengine.PassFailed, fmt.Sprintf("step %s: %s", row.StepID, row.Error)
			}
		}
		if row.StepOrder >= 0 {
			body++
			if row.Status == "skipped" {
				skipped++
			}
		}
	}
	// A pass whose query you skipped has no body: it is skipped too.
	if body == skipped {
		return workflowengine.PassSkipped, ""
	}
	return workflowengine.PassSucceeded, ""
}

// endLoop ends a loop with its outputs, and the run goes on after it.
func (s *WorkflowService) endLoop(ctx context.Context, at *runAt, old string, state workflowengine.LoopState, endedBy string) (bool, error) {
	state.EndedBy = endedBy
	encoded, _ := json.Marshal(state.Outputs())
	attemptID := s.latestAttemptID(at.view, at.path, at.row.LatestAttempt)
	if err := s.store.EndWorkflowLoop(ctx, at.view.Run.ID, at.path, attemptID, old, state.Encode(), string(encoded), at.next); err != nil {
		return false, conflictIsNotMoved(err)
	}
	s.changed(ctx, at.view.Run.ID, at.path, WorkflowChangeStepCompleted)
	return true, nil
}

// failLoop fails a loop step with its outputs so far, and with it writes
// the loop's state when state is set; the step's own on_failure decides
// what becomes of the run.
func (s *WorkflowService) failLoop(ctx context.Context, runID, path, attemptID string, cause error, outputs map[string]any, state *store.LoopStateWrite) (bool, error) {
	mode, err := s.store.FailWorkflowStepWith(ctx, runID, path, attemptID, store.StepFailure{Message: cause.Error(), InProcess: true, Outputs: outputs, LoopState: state})
	if err != nil {
		return false, conflictIsNotMoved(err)
	}
	if mode == store.FailurePaused {
		s.changed(ctx, runID, path, WorkflowChangeStepPaused)
		return false, nil
	}
	s.changed(ctx, runID, path, WorkflowChangeStepFailed)
	return mode == store.FailureContinued || mode == store.FailurePassEnded, nil
}

// latestAttemptID is the ID of a step's attempt of that number.
func (s *WorkflowService) latestAttemptID(view *store.WorkflowRunView, path string, number int) string {
	for _, attempt := range view.Attempts {
		if attempt.Path == path && attempt.AttemptNumber == number {
			return attempt.ID
		}
	}
	return ""
}

// conflictIsNotMoved takes a conflict as another host having moved the
// run first.
func conflictIsNotMoved(err error) error {
	if errors.Is(err, store.ErrWorkflowStateConflict) {
		return nil
	}
	return err
}
