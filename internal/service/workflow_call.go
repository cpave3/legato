package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// Calling a workflow (spec LEG-482 §1, §4; LEG-489). A workflow.run step
// starts its child run, linked to the step and its attempt
// (store.CreateChildWorkflowRun, unique per attempt), and waits on it. The
// child starts only once the step is marked waiting (Waiting), so the
// run-ended hook always finds the step it settles. When the child ends,
// the hook settles the step (childRuns): succeeded completes it with the
// child's outputs, status and run_id; failed, cancelled or stopped fails
// it with the child's error_code, and the step's on_failure decides.
// Cancelling or stopping the parent writes its end first and then cancels
// its children, so the child's cancellation never reaches the parent's
// on_failure. Retry resumes the same child: its failed step is reset,
// then run again once the step waits. ReconcileChildRuns picks up what a
// crash left between those writes.

// The error codes a workflow.run step fails with by itself; a child's own
// failure passes its error_code on.
const (
	WorkflowCallNotFound  = "workflow_not_found"
	WorkflowCallRecursion = "workflow_recursion"
	WorkflowCallTooDeep   = "workflow_too_deep"
	WorkflowCallInputs    = "workflow_inputs"
	WorkflowCallChildGone = "workflow_run_gone"
)

// SetDefinitions gives the service the workflows Legato finds, which
// workflow.run steps call by name.
func (s *WorkflowService) SetDefinitions(discover func() workflowengine.DiscoveryResult) {
	s.discover = discover
}

func (s *WorkflowService) definitions() workflowengine.DiscoveryResult {
	if s.discover == nil {
		return workflowengine.DiscoveryResult{}
	}
	return s.discover()
}

func (s *WorkflowService) definitionNamed(name string) *workflowengine.Definition {
	for _, definition := range s.definitions().Definitions {
		if definition.Name == name {
			return definition
		}
	}
	return nil
}

// workflowRunExecutor is the workflow.run step.
type workflowRunExecutor struct {
	workflows *WorkflowService
}

// Type is workflowengine.WorkflowRunStepType, spelt out for the catalog
// test.
func (e *workflowRunExecutor) Type() string { return "workflow.run" }

func (e *workflowRunExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	if err := e.workflows.startChild(ctx, step); err != nil {
		return WorkflowExecutionResult{}, err
	}
	return WorkflowExecutionResult{Status: WorkflowStepWaiting}, nil
}

// Cancel leaves the child alone: a run that ends cancels its children
// after its own end is written (cancelChildren), and a step failed by hand
// keeps its child for Retry.
func (e *workflowRunExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

// Waiting starts or resumes the child once the step waits on it.
func (e *workflowRunExecutor) Waiting(ctx context.Context, attempt store.WorkflowStepAttempt) {
	e.workflows.driveChild(ctx, attempt)
}

// WorkflowCallDetails is what the run page shows of a workflow.run step:
// its child run.
type WorkflowCallDetails struct {
	RunID    string `json:"run_id"`
	Workflow string `json:"workflow"`
	Title    string `json:"title"`
	Status   string `json:"status"`
}

func (e *workflowRunExecutor) Details(ctx context.Context, attempt store.WorkflowStepAttempt) any {
	row, ok := e.workflows.stepRow(ctx, attempt.RunID, attempt.Path)
	if !ok || row.ChildRunID == "" {
		return nil
	}
	child, err := e.workflows.store.GetWorkflowRun(ctx, row.ChildRunID)
	if err != nil {
		return nil
	}
	return WorkflowCallDetails{RunID: child.Run.ID, Workflow: child.Run.WorkflowName, Title: child.Run.WorkflowTitle, Status: child.Run.Status}
}

// workflowStepWaiter is an executor told when its step has been marked
// waiting, to start what the step waits on only then.
type workflowStepWaiter interface {
	Waiting(ctx context.Context, attempt store.WorkflowStepAttempt)
}

func (s *WorkflowService) stepRow(ctx context.Context, runID, path string) (store.WorkflowStep, bool) {
	view, err := s.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		return store.WorkflowStep{}, false
	}
	for _, step := range view.Steps {
		if step.Path == path {
			return step, true
		}
	}
	return store.WorkflowStep{}, false
}

// startChild makes the step's child, or finds the one it resumes: a child
// the step has that runs, waits or succeeded is kept; a failed one has its
// failed step reset to run again (Retry); a cancelled or stopped one is
// replaced. It is idempotent, for the reconcile.
func (s *WorkflowService) startChild(ctx context.Context, step ResolvedWorkflowStep) error {
	view, err := s.store.GetWorkflowRun(ctx, step.RunID)
	if err != nil {
		return err
	}
	attemptNumber := 0
	for _, attempt := range view.Attempts {
		if attempt.ID == step.AttemptID {
			attemptNumber = attempt.AttemptNumber
		}
	}
	if attemptNumber == 0 {
		return fmt.Errorf("workflow.run step %s: attempt %s not found", step.StepID, step.AttemptID)
	}
	for _, row := range view.Steps {
		if row.Path != step.StepID || row.ChildRunID == "" {
			continue
		}
		child, err := s.store.GetWorkflowRun(ctx, row.ChildRunID)
		if errors.Is(err, store.ErrNotFound) {
			break
		}
		if err != nil {
			return err
		}
		switch child.Run.Status {
		case "running", "waiting", "suspended", "succeeded":
			return nil
		case "failed":
			err := s.store.ResetWorkflowStepForRetry(ctx, child.Run.ID, child.Run.CurrentStepID)
			if err == nil {
				s.changed(ctx, child.Run.ID, child.Run.CurrentStepID, WorkflowChangeStepRetried)
			}
			if err == nil || errors.Is(err, store.ErrWorkflowStateConflict) {
				return nil
			}
			return err
		}
	}
	name, _ := step.With["workflow"].(string)
	definition := s.definitionNamed(name)
	if definition == nil {
		return &WorkflowStepError{Code: WorkflowCallNotFound, Message: fmt.Sprintf("workflow %q is not among the workflows Legato finds", name)}
	}
	chain, err := s.callChain(ctx, view.Run)
	if err != nil {
		return err
	}
	if slices.Contains(chain, name) {
		return &WorkflowStepError{Code: WorkflowCallRecursion, Message: fmt.Sprintf("workflow %q is already running in this chain of calls (%s): a workflow may not call itself", name, chainText(chain, name))}
	}
	if len(chain) > workflowengine.MaxCallDepth {
		return &WorkflowStepError{Code: WorkflowCallTooDeep, Message: fmt.Sprintf("calling workflow %q would nest calls deeper than %d", name, workflowengine.MaxCallDepth)}
	}
	given, _ := step.With["inputs"].(map[string]any)
	inputs, err := definition.NormalizeValues(given)
	if err != nil {
		return &WorkflowStepError{Code: WorkflowCallInputs, Message: fmt.Sprintf("workflow %q: %v", name, err), Err: err}
	}
	_, err = s.createRun(ctx, definition, inputs, store.WorkflowRun{ParentRunID: step.RunID, ParentStepPath: step.StepID, ParentAttempt: attemptNumber})
	return err
}

// callChain names the workflows of a run and its ancestors, the run's own
// first, up to one more than calls may nest.
func (s *WorkflowService) callChain(ctx context.Context, run store.WorkflowRun) ([]string, error) {
	chain := []string{run.WorkflowName}
	for parent := run.ParentRunID; parent != "" && len(chain) <= workflowengine.MaxCallDepth; {
		view, err := s.store.GetWorkflowRun(ctx, parent)
		if err != nil {
			return nil, err
		}
		chain = append(chain, view.Run.WorkflowName)
		parent = view.Run.ParentRunID
	}
	return chain, nil
}

// chainText is the chain of calls from the root to name, called again.
func chainText(chain []string, name string) string {
	text := ""
	for i := len(chain) - 1; i >= 0; i-- {
		text += chain[i] + " → "
	}
	return text + name
}

// driveChild starts or resumes the child of a step that waits on it (one
// no process is advancing: running at a pending step), then settles the
// step if the child has ended.
func (s *WorkflowService) driveChild(ctx context.Context, attempt store.WorkflowStepAttempt) {
	if s.recordOnly {
		return
	}
	row, ok := s.stepRow(ctx, attempt.RunID, attempt.Path)
	if !ok || row.ChildRunID == "" {
		return
	}
	child, err := s.store.GetWorkflowRun(ctx, row.ChildRunID)
	if err == nil && child.Run.Status == "running" && stepPending(child, child.Run.CurrentStepID) {
		if err := s.resumeRun(ctx, child); err != nil {
			log.Printf("workflows: child run %s of %s: %v", child.Run.ID, attempt.RunID, err)
		}
	}
	s.settleChild(ctx, attempt.RunID, attempt.Path, row.ChildRunID)
}

// resumeRun advances a run from its current step.
func (s *WorkflowService) resumeRun(ctx context.Context, view *store.WorkflowRunView) error {
	return s.advance(ctx, view.Run.ID, "", false)
}

// settleChild settles a parent's step at path, when it still waits on
// childID and that child has ended.
func (s *WorkflowService) settleChild(ctx context.Context, parentRunID, path, childID string) {
	if s.recordOnly {
		return
	}
	view, err := s.store.GetWorkflowRun(ctx, parentRunID)
	if err != nil {
		return
	}
	var waiting *store.WorkflowStepAttempt
	for i := range view.Attempts {
		attempt := &view.Attempts[i]
		if attempt.Path == path && attempt.Status == WorkflowStepWaiting && attempt.ExecutorType == workflowengine.WorkflowRunStepType {
			waiting = attempt
		}
	}
	if waiting == nil {
		return
	}
	for _, row := range view.Steps {
		if row.Path == path && row.ChildRunID != childID {
			return
		}
	}
	end, ended, err := workflowRunEnd(ctx, s.store, childID)
	if err != nil || !ended {
		return
	}
	if end.Status == "succeeded" {
		outputs := map[string]any{}
		if child, err := s.store.GetWorkflowRun(ctx, childID); err == nil {
			outputs, _ = s.runOutputs(ctx, child)
		}
		outputs["status"], outputs["run_id"] = "succeeded", childID
		_, err = s.CompleteWaiting(ctx, parentRunID, path, waiting.ID, outputs)
	} else {
		_, err = s.FailWaitingError(ctx, parentRunID, path, waiting.ID, s.childFailure(ctx, end))
	}
	if err != nil && !errors.Is(err, store.ErrWorkflowStateConflict) {
		log.Printf("workflows: settling step %s of %s on its child %s: %v", path, parentRunID, childID, err)
	}
}

// childFailure is how a child's end that is not a success fails its
// parent's step: with the child's status, its error_code and link (the
// failed step's), and its run_id.
func (s *WorkflowService) childFailure(ctx context.Context, end WorkflowRunEnd) *WorkflowStepError {
	failure := &WorkflowStepError{Status: end.Status, Outputs: map[string]any{"run_id": end.RunID}}
	if end.Gone {
		failure.Status, failure.Code = "failed", WorkflowCallChildGone
		failure.Message = fmt.Sprintf("the child run %s is gone", end.RunID)
		return failure
	}
	name := end.RunID
	if child, err := s.store.GetWorkflowRun(ctx, end.RunID); err == nil {
		name = child.Run.WorkflowName
		for _, step := range child.Steps {
			if step.Path == child.Run.CurrentStepID && (step.Status == "failed" || step.Status == WorkflowStepPaused) {
				failure.Code, failure.Link = step.ErrorCode, step.ErrorLink
			}
		}
	}
	switch end.Status {
	case "cancelled":
		failure.Message = fmt.Sprintf("workflow %s was cancelled", name)
	case WorkflowRunStopped:
		failure.Message = fmt.Sprintf("workflow %s stopped", name)
	default:
		failure.Message = fmt.Sprintf("workflow %s failed", name)
	}
	if end.Error != "" {
		failure.Message += ": " + end.Error
	}
	return failure
}

// cancelChildren cancels the children of a run that has ended cancelled or
// stopped, and theirs: the run's own end is written first, so no child's
// cancellation settles one of its steps.
func (s *WorkflowService) cancelChildren(ctx context.Context, runID string) {
	children, err := s.store.ListChildWorkflowRuns(ctx, runID)
	if err != nil {
		log.Printf("workflows: the children of %s: %v", runID, err)
		return
	}
	for _, child := range children {
		if child.Status == "succeeded" || child.Status == "cancelled" || child.Status == WorkflowRunStopped {
			continue
		}
		s.cancelChild(ctx, child)
	}
}

// cancelChild cancels a child run because its parent ended.
func (s *WorkflowService) cancelChild(ctx context.Context, child store.WorkflowRun) {
	if _, err := s.Cancel(ctx, child.ID, "its parent run "+child.ParentRunID+" ended"); err != nil && !errors.Is(err, store.ErrWorkflowStateConflict) {
		log.Printf("workflows: cancelling child run %s of %s: %v", child.ID, child.ParentRunID, err)
	}
}

// childRuns is the run-ended hook's listener for workflow.run steps: the
// children their waiting steps hold, and settling a step when its child
// ends.
type childRuns struct {
	workflows *WorkflowService
}

func (c childRuns) RunEnded(ctx context.Context, end WorkflowRunEnd) {
	if end.ParentRunID != "" {
		c.workflows.settleChild(ctx, end.ParentRunID, end.ParentStepPath, end.RunID)
		return
	}
	if !end.Gone {
		return
	}
	// A child that is gone no longer names its parent: find the step that
	// holds it.
	for _, held := range c.held(ctx) {
		if held.child == end.RunID {
			c.workflows.settleChild(ctx, held.attempt.RunID, held.attempt.Path, end.RunID)
		}
	}
}

func (c childRuns) RunsHeld(ctx context.Context) ([]string, error) {
	var runs []string
	for _, held := range c.held(ctx) {
		runs = append(runs, held.child)
	}
	return runs, nil
}

type heldChild struct {
	attempt store.WorkflowStepAttempt
	child   string
}

// held are the waiting workflow.run attempts, each with its step's child.
func (c childRuns) held(ctx context.Context) []heldChild {
	attempts, err := c.workflows.store.ListWaitingWorkflowAttemptsByExecutor(ctx, workflowengine.WorkflowRunStepType)
	if err != nil {
		log.Printf("workflows: waiting workflow.run steps: %v", err)
		return nil
	}
	var held []heldChild
	for _, attempt := range attempts {
		if row, ok := c.workflows.stepRow(ctx, attempt.RunID, attempt.Path); ok && row.ChildRunID != "" {
			held = append(held, heldChild{attempt: attempt, child: row.ChildRunID})
		}
	}
	return held
}

// ReconcileChildRuns picks up workflow.run steps a crash left between
// writes: an attempt still running (started over olderThan ago) has its
// child made or found, is marked waiting, and its child started; a waiting
// one has a child no process advances resumed, and an ended child
// settled. A child whose parent ended cancelled or stopped, but which that
// end's cancelChildren never reached, is cancelled: its parent's step has
// ended, so nothing settles it. The advance loop runs it with the run-ended
// hook's sweep.
func (s *WorkflowService) ReconcileChildRuns(ctx context.Context, olderThan time.Duration) {
	if s.recordOnly {
		return
	}
	orphans, err := s.store.ListUncancelledChildrenOfEndedWorkflowRuns(ctx)
	if err != nil {
		log.Printf("workflows: children of ended runs: %v", err)
	}
	for _, child := range orphans {
		s.cancelChild(ctx, child)
	}
	running, err := s.store.ListWorkflowAttemptsByExecutor(ctx, workflowengine.WorkflowRunStepType, "running")
	if err != nil {
		log.Printf("workflows: running workflow.run steps: %v", err)
	}
	for _, attempt := range running {
		if started, err := time.Parse(time.DateTime, attempt.StartedAt); err == nil && time.Since(started) < olderThan {
			continue
		}
		row, ok := s.stepRow(ctx, attempt.RunID, attempt.Path)
		if !ok {
			continue
		}
		var with map[string]any
		_ = json.Unmarshal([]byte(row.ResolvedInputsJSON), &with)
		step := ResolvedWorkflowStep{RunID: attempt.RunID, AttemptID: attempt.ID, TaskID: attempt.TaskID, StepID: attempt.Path, Title: row.Title, Type: attempt.ExecutorType, With: with}
		if err := s.startChild(ctx, step); err != nil {
			if failErr := s.failRecorded(ctx, attempt.RunID, attempt.Path, attempt.ID, err); failErr != nil {
				log.Printf("workflows: failing step %s of %s: %v", attempt.Path, attempt.RunID, failErr)
			}
			continue
		}
		if err := s.store.WaitWorkflowStepOnService(ctx, attempt.RunID, attempt.Path, attempt.ID); err != nil {
			continue
		}
		s.changed(ctx, attempt.RunID, attempt.Path, WorkflowChangeStepWaiting)
		attempt.Status = WorkflowStepWaiting
		s.driveChild(ctx, attempt)
	}
	waiting, err := s.store.ListWaitingWorkflowAttemptsByExecutor(ctx, workflowengine.WorkflowRunStepType)
	if err != nil {
		log.Printf("workflows: waiting workflow.run steps: %v", err)
	}
	for _, attempt := range waiting {
		s.driveChild(ctx, attempt)
	}
}

// runOutputs computes a succeeded run's declared outputs from its inputs,
// its task and its steps, with a warning for each that does not resolve.
func (s *WorkflowService) runOutputs(ctx context.Context, view *store.WorkflowRunView) (map[string]any, []string) {
	var definition workflowengine.Definition
	if err := json.Unmarshal([]byte(view.Run.DefinitionJSON), &definition); err != nil || len(definition.Outputs) == 0 {
		return map[string]any{}, nil
	}
	var inputs map[string]any
	_ = json.Unmarshal([]byte(view.Run.InputsJSON), &inputs)
	var task map[string]any
	if found, err := s.store.GetTask(ctx, view.Run.TaskID); err == nil {
		task = workflowTaskContext(found)
	}
	steps := map[string]map[string]any{}
	for _, step := range view.Steps {
		if step.ParentPath != "" || (step.Status != "succeeded" && step.Status != "failed") {
			continue
		}
		var outputs map[string]any
		if json.Unmarshal([]byte(step.OutputsJSON), &outputs) == nil {
			steps[step.StepID] = outputs
		}
	}
	return definition.ResolveOutputs(inputs, task, steps)
}

// keepRunOutputs keeps a run's outputs once it has succeeded and a host
// has filled what its agents' completions left (outputsUnfilled).
func (s *WorkflowService) keepRunOutputs(ctx context.Context, runID string) {
	view, err := s.store.GetWorkflowRun(ctx, runID)
	if err != nil || view.Run.Status != "succeeded" || outputsUnfilled(view) {
		return
	}
	outputs, _ := s.runOutputs(ctx, view)
	if len(outputs) == 0 {
		return
	}
	encoded, _ := json.Marshal(outputs)
	if err := s.store.SetWorkflowRunOutputs(ctx, runID, string(encoded)); err != nil {
		log.Printf("workflows: keeping the outputs of %s: %v", runID, err)
	}
}

// addRunOutputs puts a succeeded run's outputs, and the warnings of those
// that did not resolve, on its view.
func (s *WorkflowService) addRunOutputs(ctx context.Context, view *store.WorkflowRunView) {
	if view.Run.Status != "succeeded" {
		return
	}
	outputs, warnings := s.runOutputs(ctx, view)
	view.OutputWarnings = warnings
	if len(outputs) > 0 && (view.Run.OutputsJSON == "" || view.Run.OutputsJSON == "{}") {
		encoded, _ := json.Marshal(outputs)
		view.Run.OutputsJSON = string(encoded)
	}
}
