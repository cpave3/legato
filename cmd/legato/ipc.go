package main

import (
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/ipc"
)

// webNotifier is the subset of *server.Server needed by IPC handlers.
type webNotifier interface {
	NotifyAgentsChanged()
}

func handleIPCMessage(msg ipc.Message, bus *events.Bus, wn webNotifier) {
	switch msg.Type {
	case "task_update", "task_note", "agent_state":
		bus.Publish(events.Event{Type: events.EventCardUpdated, At: time.Now()})
		if wn != nil {
			wn.NotifyAgentsChanged()
		}
	case "pr_linked":
		bus.Publish(events.Event{Type: events.EventPRStatusUpdated, At: time.Now()})
		if wn != nil {
			wn.NotifyAgentsChanged()
		}
	case "swarm_changed":
		bus.Publish(events.Event{
			Type: events.EventSwarmChanged,
			At:   time.Now(),
			Payload: events.SwarmChangedPayload{
				ParentTaskID: msg.TaskID,
				SubtaskID:    msg.Status,
				NewStatus:    msg.Content,
			},
		})
	case "review_changed":
		bus.Publish(events.Event{
			Type: events.EventReviewChanged,
			At:   time.Now(),
			Payload: events.ReviewChangedPayload{
				TourID: msg.TourID,
				StepID: msg.StepID,
				Kind:   msg.Kind,
			},
		})
	case "plan_proposed", "plan_extension_proposed":
		bus.Publish(events.Event{
			Type: events.EventPlanProposed,
			At:   time.Now(),
			Payload: events.PlanProposedPayload{
				ParentTaskID: msg.TaskID,
				PlanPath:     msg.PlanPath,
				ReplySocket:  msg.ReplySocket,
				Mode:         msg.Mode,
			},
		})
	}
}
