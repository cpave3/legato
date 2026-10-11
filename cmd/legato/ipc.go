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

type workflowWebNotifier interface {
	NotifyWorkflowChanged(runID, stepID string)
}

// serveNotifier is the subset of *server.Server the standalone server's IPC
// handler needs.
type serveNotifier interface {
	webNotifier
	workflowWebNotifier
	NotifyAgentChanged(taskID string)
}

// handleServeIPCMessage is what `legato serve` does with a message from a
// CLI process. It has no TUI to refresh, so it acts on fewer messages than
// handleIPCMessage and tells the web server about most of them directly.
func handleServeIPCMessage(msg ipc.Message, bus *events.Bus, srv serveNotifier) {
	switch msg.Type {
	case "task_update", "task_note", "agent_state", "pr_linked":
		srv.NotifyAgentChanged(msg.TaskID)
	case "review_changed", "plan_changed", "agent_ui_changed":
		handleIPCMessage(msg, bus, nil)
	case "workflow_changed":
		srv.NotifyWorkflowChanged(msg.WorkflowRunID, msg.WorkflowStepID)
		if msg.Kind == "completed_step" {
			srv.NotifyAgentsChanged()
		}
	}
}

func handleIPCMessage(msg ipc.Message, bus *events.Bus, wn webNotifier) {
	switch msg.Type {
	case "task_update", "task_note", "worktree_changed":
		bus.Publish(events.Event{Type: events.EventCardUpdated, At: time.Now()})
		if wn != nil {
			wn.NotifyAgentsChanged()
		}
	case "agent_state":
		now := time.Now()
		bus.Publish(events.Event{Type: events.EventCardUpdated, At: now})
		bus.Publish(events.Event{Type: events.EventAgentActivityChanged, At: now, Payload: events.AgentActivityChangedPayload{TaskID: msg.TaskID, Activity: msg.Status}})
		if wn != nil {
			wn.NotifyAgentsChanged()
		}
	case "agents_changed":
		// A Claude Code bridge bound or unbound its agent's native endpoint
		// from its own process; the agent list's native flag is stale.
		bus.Publish(events.Event{Type: events.EventAgentChimeraDiscovered, At: time.Now(), Payload: events.AgentChimeraDiscoveredPayload{TaskID: msg.TaskID}})
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
				TaskID: msg.TaskID,
				TourID: msg.TourID,
				StepID: msg.StepID,
				Kind:   msg.Kind,
			},
		})
	case "agent_ui_changed":
		// An agent showed something through its own `legato mcp serve`.
		bus.Publish(events.Event{
			Type: events.EventAgentUIChanged,
			At:   time.Now(),
			Payload: events.AgentUIChangedPayload{
				TaskID: msg.TaskID, ItemID: msg.ItemID, Placement: msg.Placement, Revision: msg.Revision, Kind: msg.Kind,
			},
		})
	case "workflow_changed":
		if notifier, ok := wn.(workflowWebNotifier); ok {
			notifier.NotifyWorkflowChanged(msg.WorkflowRunID, msg.WorkflowStepID)
		}
		bus.Publish(events.Event{
			Type: events.EventWorkflowChanged, At: time.Now(),
			Payload: events.WorkflowChangedPayload{RunID: msg.WorkflowRunID, StepID: msg.WorkflowStepID, Kind: msg.Kind},
		})
		if msg.Kind == "completed_step" {
			bus.Publish(events.Event{Type: events.EventCardUpdated, At: time.Now()})
			if wn != nil {
				wn.NotifyAgentsChanged()
			}
		}
	case "plan_changed":
		bus.Publish(events.Event{
			Type: events.EventPlanChanged,
			At:   time.Now(),
			Payload: events.PlanChangedPayload{
				PlanID: msg.PlanID, TaskID: msg.TaskID, RevisionID: msg.RevisionID, Kind: msg.Kind,
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
