package service

import (
	"time"

	"github.com/cpave3/legato/internal/engine/events"
)

// The kinds of EventWorkflowChanged the workflow service publishes after
// each write of a run's state, so the web UI follows a run whichever
// process or loop moved it. None is "completed_step": the advance loop acts
// on that one (a step an agent recorded from its pane), and a completion in
// this process has advanced the run already.
const (
	WorkflowChangeRunStarted    = "run_started"
	WorkflowChangeRunCancelled  = "run_cancelled"
	WorkflowChangeStepStarted   = "step_started"
	WorkflowChangeStepWaiting   = "step_waiting"
	WorkflowChangeStepTriggered = "step_triggered"
	WorkflowChangeStepCompleted = "step_completed"
	WorkflowChangeStepFailed    = "step_failed"
	WorkflowChangeStepRetried   = "step_retried"
)

// SetEventBus has every change to a run's state published on bus.
func (s *WorkflowService) SetEventBus(bus *events.Bus) { s.bus = bus }

// SetChangeListener is also told of every change to a run's state, after
// the bus: the TUI tells a `legato serve` on the same database, whose web
// UI would not hear of it otherwise.
func (s *WorkflowService) SetChangeListener(listener func(events.WorkflowChangedPayload)) {
	s.changeListener = listener
}

// changed publishes a change to run's state, of step when it names one.
func (s *WorkflowService) changed(runID, stepID, kind string) {
	payload := events.WorkflowChangedPayload{RunID: runID, StepID: stepID, Kind: kind}
	if s.bus != nil {
		s.bus.Publish(events.Event{Type: events.EventWorkflowChanged, At: time.Now(), Payload: payload})
	}
	if s.changeListener != nil {
		s.changeListener(payload)
	}
}
