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
// the step after it (also after a step whose failure was continued). A failure to finish the agent leaves the advance due,
// for a later reconcile to try again once the claim is stale; a next step
// that fails is recorded as the run's failure, and the advance is done.
func (s *WorkflowService) processAdvance(ctx context.Context, advance store.WorkflowAdvance) error {
	// A step that failed under on_failure: continue has no agent to finish
	// and no outputs to fill: its failure stopped it (Fail cancels it) or it
	// had ended; it is left as a failed step's agent is. Otherwise the
	// agent's own completion recorded what it declared, and Legato fills the
	// rest while the agent is still there to read.
	continued := advance.Step.Status == "failed"
	if !continued {
		goesOn, err := s.fillRecordedOutputs(ctx, &advance)
		if err != nil || !goesOn {
			return err
		}
		// Outputs it could not fill fail the step; continued, it goes on
		// as a failed step, its agent already stopped.
		continued = advance.Step.Status == "failed"
		// A run the step's record ended has its outputs kept, and its end
		// told, only now that they are filled (outputsUnfilled; LEG-489).
		if waitRecorded(advance.Step) {
			s.keepRunOutputs(ctx, advance.Step.RunID)
			s.tellIfEnded(ctx, advance.Step.RunID)
		}
	}
	if !keepsAgent(advance.Step) && !continued && !endedSignal(advance.Step) {
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
// outputs are in, for a later step to end: keep (keep_agent in version 1),
// or an agent on a task (on: <task>) without stop.
func keepsAgent(step store.WorkflowStep) bool {
	var with map[string]any
	if err := json.Unmarshal([]byte(step.ResolvedInputsJSON), &with); err != nil {
		return false
	}
	return stepKeepsAgent(with)
}

func stepKeepsAgent(with map[string]any) bool {
	if workflowengine.AgentOnTask(with) {
		stop, _ := with["stop"].(bool)
		return !stop
	}
	keep, _ := with["keep_agent"].(bool)
	kept, _ := with["keep"].(bool)
	return keep || kept
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
// for any left due while no host ran, then on each change to a run (an
// agent recording a step is a completed_step workflow change, which reaches
// serve and the TUI by IPC), and every advanceReconcileInterval for what it
// missed. It also
// runs the run-ended hook's sweep (SweepEndedRuns). It returns a function
// that stops the loop.
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
	// Agent waits (LEG-485): a stage change or an agent's end may be a
	// step's signal; each check reads the tables, so one missed is found
	// at the next.
	signals, stopSignals := bus.SubscribeInvalidations(events.EventAgentStageChanged, events.EventAgentDied)
	checkWaits := func() {
		if err := s.CheckAgentWaits(ctx); err != nil {
			log.Printf("workflows: agent waits: %v", err)
		}
		if _, err := s.SweepTimeouts(ctx, time.Now()); err != nil {
			log.Printf("workflows: timeouts: %v", err)
		}
	}
	announce(s.AdvanceDue(ctx))
	checkWaits()
	// The run-ended hook's sweep: at once for the runs that ended while
	// no host ran, then on every change to a run (one another process or
	// the agent service ended reaches this process by IPC or on the bus)
	// and at each reconcile.
	s.SweepEndedRuns(ctx)
	s.ReconcileChildRuns(ctx, advanceClaimStale)
	go func() {
		ticker := time.NewTicker(advanceReconcileInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-changed:
				payload, ok := event.Payload.(events.WorkflowChangedPayload)
				if !ok || payload.Kind != "advanced" {
					// A step an agent recorded, or one whose failure was
					// continued (on_failure: continue) where the run could
					// not move on, such as by the agent service: both are
					// left due an advance.
					announce(s.AdvanceDue(ctx))
					s.SweepEndedRuns(ctx)
				}
			case <-signals:
				checkWaits()
			case <-ticker.C:
				announce(s.AdvanceDue(ctx))
				checkWaits()
				s.SweepEndedRuns(ctx)
				s.ReconcileChildRuns(ctx, advanceClaimStale)
			}
		}
	}()
	return func() {
		cancel()
		bus.Unsubscribe(changed)
		stopSignals()
	}
}
