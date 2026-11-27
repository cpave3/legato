package service

import (
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/ipc"
)

// EventStream is what a presentation layer needs of the event bus: to
// subscribe to events and to publish its own. *events.Bus implements it;
// the TUI holds this interface, never the bus itself.
type EventStream interface {
	Subscribe(eventType events.EventType) <-chan events.Event
	Publish(event events.Event)
}

var _ EventStream = (*events.Bus)(nil)

// PlanVerdict is the operator's answer to a swarm conductor's proposed
// plan: "approved" (with the plan's path) or "rejected" (with notes).
type PlanVerdict struct {
	ParentTaskID string
	Status       string
	Notes        string
	PlanPath     string
}

// SendPlanVerdict answers the conductor waiting on replySocket with a
// plan_verdict message.
func SendPlanVerdict(replySocket string, v PlanVerdict) error {
	return ipc.Send(replySocket, ipc.Message{
		Type:     "plan_verdict",
		TaskID:   v.ParentTaskID,
		Status:   v.Status,
		Notes:    v.Notes,
		PlanPath: v.PlanPath,
	})
}
