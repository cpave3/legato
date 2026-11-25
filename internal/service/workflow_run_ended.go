package service

import (
	"context"
	"errors"
	"log"
	"slices"

	"github.com/cpave3/legato/internal/engine/store"
)

// The run-ended hook (spec LEG-559 B3; LEG-581, shared with LEG-489): one
// subscription that hears a workflow run end with any terminal status. A
// listener is told at once of an end this process's workflow service makes,
// and at each sweep (SweepEndedRuns: at the start of the advance loop and
// at each of its wakes) of every run it still holds that has ended, which
// catches an end in another process (a TUI without the review engine, the
// CLI, the agent service failing a step) or while Legato was down. Delivery
// is at least once: a run the listener still holds is told again at the
// next sweep, so RunEnded must be idempotent. See docs/ai/workflows.md
// § The run-ended hook.

// WorkflowRunStopped is a run a gate's Stop ended (Stop; LEG-486).
const WorkflowRunStopped = "stopped"

// WorkflowRunTerminal says a run status is an end: succeeded, failed,
// cancelled or stopped. Only a failed run can run again (Retry).
func WorkflowRunTerminal(status string) bool {
	switch status {
	case "succeeded", "failed", "cancelled", WorkflowRunStopped:
		return true
	}
	return false
}

// WorkflowRunEnd is a run that ended: its status, its error, its task and
// the workflow.run step it is the child of (LEG-489), if any. Gone is a run
// that no longer exists (its task was deleted), with no status: what a
// listener held for it has nothing left to wait on.
type WorkflowRunEnd struct {
	RunID          string
	Status         string
	Gone           bool
	Error          string
	TaskID         string
	ParentRunID    string
	ParentStepPath string
}

// WorkflowRunEndedListener is told of the runs that end. RunsHeld are the
// runs it still waits on (a review loop's owner, a child a parent step
// waits on), which the sweep checks.
type WorkflowRunEndedListener interface {
	RunEnded(ctx context.Context, end WorkflowRunEnd)
	RunsHeld(ctx context.Context) ([]string, error)
}

// OnRunEnded subscribes listener to every run's end.
func (s *WorkflowService) OnRunEnded(listener WorkflowRunEndedListener) {
	s.endedMu.Lock()
	defer s.endedMu.Unlock()
	s.endedListeners = append(s.endedListeners, listener)
}

func (s *WorkflowService) runEndedListeners() []WorkflowRunEndedListener {
	s.endedMu.Lock()
	defer s.endedMu.Unlock()
	return slices.Clone(s.endedListeners)
}

// endsRun says a change of this kind can end its run.
func endsRun(kind string) bool {
	switch kind {
	case WorkflowChangeStepCompleted, WorkflowChangeStepFailed, WorkflowChangeStepSkipped, WorkflowChangeRunCancelled, WorkflowChangeRunStopped:
		return true
	}
	return false
}

// tellIfEnded tells every listener of runID's end, when it has ended.
func (s *WorkflowService) tellIfEnded(ctx context.Context, runID string) {
	listeners := s.runEndedListeners()
	if len(listeners) == 0 {
		return
	}
	end, ended, err := workflowRunEnd(ctx, s.store, runID)
	if err != nil {
		log.Printf("workflows: whether run %s ended: %v", runID, err)
		return
	}
	if !ended {
		return
	}
	for _, listener := range listeners {
		listener.RunEnded(ctx, end)
	}
}

// SweepEndedRuns tells each listener of every run it holds that has ended
// or is gone. The advance loop runs it at its start and at each wake.
func (s *WorkflowService) SweepEndedRuns(ctx context.Context) {
	for _, listener := range s.runEndedListeners() {
		held, err := listener.RunsHeld(ctx)
		if err != nil {
			log.Printf("workflows: the runs a listener holds: %v", err)
			continue
		}
		slices.Sort(held)
		for _, runID := range slices.Compact(held) {
			end, ended, err := workflowRunEnd(ctx, s.store, runID)
			if err != nil {
				log.Printf("workflows: whether run %s ended: %v", runID, err)
				continue
			}
			if ended {
				listener.RunEnded(ctx, end)
			}
		}
	}
}

// workflowRunEnd is runID's end, and whether it has ended: a terminal
// status, or gone. A run whose last step an agent completed has not ended
// until a host has filled that step's outputs (processAdvance), so no
// listener reads them before.
func workflowRunEnd(ctx context.Context, db *store.Store, runID string) (WorkflowRunEnd, bool, error) {
	view, err := db.GetWorkflowRun(ctx, runID)
	if errors.Is(err, store.ErrNotFound) {
		return WorkflowRunEnd{RunID: runID, Gone: true}, true, nil
	}
	if err != nil {
		return WorkflowRunEnd{}, false, err
	}
	run := view.Run
	if !WorkflowRunTerminal(run.Status) || outputsUnfilled(view) {
		return WorkflowRunEnd{}, false, nil
	}
	return WorkflowRunEnd{RunID: run.ID, Status: run.Status, Error: run.Error, TaskID: run.TaskID,
		ParentRunID: run.ParentRunID, ParentStepPath: run.ParentStepPath}, true, nil
}
