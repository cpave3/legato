package server

import (
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/service"
)

// The kinds of workflow_changed that are not about a run: the web UI reads
// the definitions again (their trigger_states) only for these, so a run's
// events never rediscover the workflow files.
const (
	// WorkflowDefinitionsChanged is a workflow file created, saved, copied
	// or deleted, or a trigger switched or reset.
	WorkflowDefinitionsChanged = "definitions"
	// WorkflowTriggerChanged is a trigger request that moved on or a
	// schedule that fired (EventWorkflowTriggerChanged).
	WorkflowTriggerChanged = "trigger"
)

// ReviewLoopChanged is the kind of EventWorkflowChanged a run's review loop
// publishes when it moves on (service.ReviewRunCaller).
const ReviewLoopChanged = service.ReviewLoopChangedKind

// StartWorkflowEvents sends the web UI every workflow change published on
// the bus, whoever published it: the workflow service's writes, the advance
// and agent-ended loops, review loops, an agent that exited, and
// NotifyWorkflowChangedKind. A trigger change goes as kind "trigger".
func (s *Server) StartWorkflowEvents() {
	if s.bus == nil {
		return
	}
	runs := s.bus.Subscribe(events.EventWorkflowChanged)
	triggers := s.bus.Subscribe(events.EventWorkflowTriggerChanged)
	s.workflowForwarding.Store(true)
	go func() {
		for event := range runs {
			payload, ok := event.Payload.(events.WorkflowChangedPayload)
			if !ok {
				continue
			}
			s.hub.Broadcast(WSMessage{Type: MsgWorkflowChanged, WorkflowRunID: payload.RunID, WorkflowStepID: payload.StepID, Kind: payload.Kind})
		}
	}()
	go func() {
		for range triggers {
			s.hub.Broadcast(WSMessage{Type: MsgWorkflowChanged, Kind: WorkflowTriggerChanged})
		}
	}()
}

// notifyWorkflowDefinitionsChanged tells the web UI that the workflows'
// definitions or trigger switches changed.
func (s *Server) notifyWorkflowDefinitionsChanged() {
	s.hub.Broadcast(WSMessage{Type: MsgWorkflowChanged, Kind: WorkflowDefinitionsChanged})
}
