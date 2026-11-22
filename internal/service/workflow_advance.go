package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// Host-side advance (spec LEG-482 §5.4, LEG-483). `legato workflow step
// complete` runs inside the agent's own pane, so it only records the step
// (store.RecordWorkflowStepCompletion marks it advance due). A host process
// (serve, the TUI) then finishes the step's agent, unless the step keeps
// it, and only then starts the next step: the next agent.run never meets
// the last one still running (LEG-452), and nothing that moves the run on
// runs in a pane that is about to be stopped.

// advanceReconcileInterval is how often a host looks for advances it was
// not told of: an IPC message lost, or a process that died holding one.
const advanceReconcileInterval = 30 * time.Second

// advanceClaimStale is how long a claimed advance is another process's
// before a host takes it again, its claimer having died.
const advanceClaimStale = 2 * time.Minute

// SetRecordOnly makes completions record their step and nothing more, for
// a process that is not a host (the CLI an agent runs).
func (s *WorkflowService) SetRecordOnly() { s.recordOnly = true }

// AdvanceDue moves on every run with a recorded step due an advance that
// no live process holds. It returns the runs it moved on.
func (s *WorkflowService) AdvanceDue(ctx context.Context) ([]string, error) {
	claimed, err := s.store.ClaimWorkflowAdvances(ctx, "", "", advanceClaimStale)
	if err != nil {
		return nil, err
	}
	var moved []string
	var errs []error
	for _, advance := range claimed {
		if err := s.processAdvance(ctx, advance); err != nil {
			errs = append(errs, fmt.Errorf("run %s step %s: %w", advance.Step.RunID, advance.Step.Path, err))
		}
		moved = append(moved, advance.Step.RunID)
	}
	return moved, errors.Join(errs...)
}

// advanceRecorded is AdvanceDue for one step, which this process just
// recorded.
func (s *WorkflowService) advanceRecorded(ctx context.Context, runID, path string) (bool, error) {
	claimed, err := s.store.ClaimWorkflowAdvances(ctx, runID, path, advanceClaimStale)
	if err != nil || len(claimed) == 0 {
		return false, err
	}
	return true, s.processAdvance(ctx, claimed[0])
}

// processAdvance finishes or keeps a recorded step's agent, then starts
// the step after it. A failure to finish the agent leaves the advance due,
// for a later reconcile to try again once the claim is stale; a next step
// that fails is recorded as the run's failure, and the advance is done.
func (s *WorkflowService) processAdvance(ctx context.Context, advance store.WorkflowAdvance) error {
	if !keepsAgent(advance.Step) {
		if executor, ok := s.executors[advance.Attempt.ExecutorType].(interface {
			Finish(context.Context, store.WorkflowStepAttempt) error
		}); ok {
			if err := executor.Finish(ctx, advance.Attempt); err != nil {
				return fmt.Errorf("finishing workflow execution: %w", err)
			}
		}
	}
	next, err := s.startNextStep(ctx, advance.Step)
	if finishErr := s.store.FinishWorkflowAdvance(ctx, advance.Step.RunID, advance.Step.Path); finishErr != nil {
		return errors.Join(err, finishErr)
	}
	if err != nil {
		return &StepRecordedError{StepID: advance.Step.Path, NextStepID: next, Err: err}
	}
	return nil
}

// keepsAgent reports whether the step leaves its agent running once its
// outputs are in (keep_agent), for a later step to end.
func keepsAgent(step store.WorkflowStep) bool {
	var with map[string]any
	if err := json.Unmarshal([]byte(step.ResolvedInputsJSON), &with); err != nil {
		return false
	}
	keep, _ := with["keep_agent"].(bool)
	return keep
}

// startNextStep starts the step after a recorded one, when the run is
// still at it and nothing has started it: the record moved the run there.
func (s *WorkflowService) startNextStep(ctx context.Context, recorded store.WorkflowStep) (string, error) {
	view, definition, inputs, prior, index, err := s.loadExecution(ctx, recorded.RunID, recorded.Path)
	if err != nil {
		return "", err
	}
	if index+1 >= len(definition.Steps) {
		return "", nil
	}
	next := workflowengine.StepPath(recorded.ParentPath, definition.Steps[index+1].ID)
	if view.Run.Status != "running" || view.Run.CurrentStepID != next || !stepPending(view, next) {
		return next, nil
	}
	var outputs map[string]any
	if err := json.Unmarshal([]byte(recorded.OutputsJSON), &outputs); err != nil {
		return next, err
	}
	prior[definition.Steps[index].ID] = outputs
	return next, s.advance(ctx, definition, inputs, recorded.RunID, view.Run.TaskID, index+1, prior, "", false)
}

func stepPending(view *store.WorkflowRunView, path string) bool {
	for _, step := range view.Steps {
		if step.Path == path {
			return step.Status == "pending"
		}
	}
	return false
}

// StartAdvanceLoop makes this process a host that advances runs: at once
// for any left due while no host ran, then each time an agent records a
// step (a completed_step workflow change, which reaches serve and the TUI
// by IPC), and every advanceReconcileInterval for what it missed. It
// returns a function that stops the loop.
func (s *WorkflowService) StartAdvanceLoop(ctx context.Context, bus *events.Bus) func() {
	ctx, cancel := context.WithCancel(ctx)
	announce := func(runs []string, err error) {
		if err != nil {
			log.Printf("workflows: advance: %v", err)
		}
		for _, runID := range runs {
			bus.Publish(events.Event{Type: events.EventWorkflowChanged, At: time.Now(), Payload: events.WorkflowChangedPayload{RunID: runID, Kind: "advanced"}})
		}
	}
	changed := bus.Subscribe(events.EventWorkflowChanged)
	announce(s.AdvanceDue(ctx))
	go func() {
		ticker := time.NewTicker(advanceReconcileInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-changed:
				if payload, ok := event.Payload.(events.WorkflowChangedPayload); !ok || payload.Kind != "completed_step" {
					continue
				}
				announce(s.AdvanceDue(ctx))
			case <-ticker.C:
				announce(s.AdvanceDue(ctx))
			}
		}
	}()
	return func() {
		cancel()
		bus.Unsubscribe(changed)
	}
}
