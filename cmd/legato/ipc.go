package main

import (
	"os"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/service"
)

// webNotifier is the subset of *server.Server needed by IPC handlers.
type webNotifier interface {
	NotifyAgentsChanged()
}

type workflowWebNotifier interface {
	NotifyWorkflowChanged(runID, stepID string)
}

// workflowKindNotifier is a web server that publishes a workflow change on
// the bus with its kind, and sends it to its clients from there.
type workflowKindNotifier interface {
	NotifyWorkflowChangedKind(runID, stepID, kind string)
}

// agentWebNotifier is a web server that tells the run pages of a task's
// agent steps that the agent changed.
type agentWebNotifier interface {
	NotifyAgentChanged(taskID string)
}

// workflowChangeBroadcaster tells the other Legato processes of each change
// the TUI's workflow service makes, so a `legato serve` on the same database
// shows it in its web UI. The message names this process, which ignores it
// (handleIPCMessage).
func workflowChangeBroadcaster(send func(ipc.Message)) func(events.WorkflowChangedPayload) {
	pid := os.Getpid()
	return func(change events.WorkflowChangedPayload) {
		send(ipc.Message{Type: "workflow_changed", WorkflowRunID: change.RunID, WorkflowStepID: change.StepID, Kind: change.Kind, PID: pid})
	}
}

// serveNotifier is the subset of *server.Server the standalone server's IPC
// handler needs.
type serveNotifier interface {
	webNotifier
	workflowWebNotifier
	NotifyAgentChanged(taskID string)
	// NotifyWorkflowChangedKind keeps the kind, so a completed_step from
	// an agent's pane wakes serve's advance loop.
	NotifyWorkflowChangedKind(runID, stepID, kind string)
}

// handleServeIPCMessage is what `legato serve` does with a message from a
// CLI process. It has no TUI to refresh, so it acts on fewer messages than
// handleIPCMessage and tells the web server about most of them directly.
func handleServeIPCMessage(msg ipc.Message, bus *events.Bus, srv serveNotifier) {
	switch msg.Type {
	case "task_update", "task_note", "agent_state", "pr_linked":
		srv.NotifyAgentChanged(msg.TaskID)
		// A worker's handoffs follow their cards and agents: its Events
		// connections send a main each change (docs/ai/integrations.md
		// § Live away state).
		switch msg.Type {
		case "agent_state":
			bus.Publish(events.Event{Type: events.EventAgentActivityChanged, At: time.Now(), Payload: events.AgentActivityChangedPayload{TaskID: msg.TaskID, Activity: msg.Status}})
		case "task_update":
			bus.Publish(events.Event{Type: events.EventCardUpdated, At: time.Now()})
		}
	case "review_changed", "plan_changed", "agent_ui_changed", "approval_changed", "mqtt_changed", "server_theme_changed", "scope_changed", "operator_items_changed":
		handleIPCMessage(msg, bus, nil)
	case "agent_stage_changed":
		handleIPCMessage(msg, bus, nil)
		srv.NotifyAgentsChanged()
	case agentLinksChanged, service.ReeveWaitChanged:
		srv.NotifyAgentsChanged()
	case "workflow_changed":
		srv.NotifyWorkflowChangedKind(msg.WorkflowRunID, msg.WorkflowStepID, msg.Kind)
		if msg.Kind == "completed_step" {
			srv.NotifyAgentsChanged()
		}
	}
}

func handleIPCMessage(msg ipc.Message, bus *events.Bus, wn webNotifier) {
	switch msg.Type {
	case "server_theme_changed":
		bus.Publish(events.Event{Type: events.EventServerThemeInvalidated, At: time.Now()})
	case "mqtt_changed":
		bus.Publish(events.Event{Type: events.EventMQTTInvalidated, At: time.Now()})
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
		// A step's agent: its run page shows what the agent is doing.
		if notifier, ok := wn.(agentWebNotifier); ok && msg.TaskID != "" {
			notifier.NotifyAgentChanged(msg.TaskID)
		}
	case "agents_changed":
		// A Claude Code bridge bound or unbound its agent's native endpoint
		// from its own process; the agent list's native flag is stale.
		bus.Publish(events.Event{Type: events.EventAgentChimeraDiscovered, At: time.Now(), Payload: events.AgentChimeraDiscoveredPayload{TaskID: msg.TaskID}})
		if wn != nil {
			wn.NotifyAgentsChanged()
		}
	case agentLinksChanged:
		// One agent messaged another from another process: the graph of
		// agents has a new link.
		if wn != nil {
			wn.NotifyAgentsChanged()
		}
	case service.ReeveWaitChanged:
		// An agent started or stopped waiting on the Reeve, or a wait went
		// unanswered: its glance moved.
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
				TaskID: msg.TaskID,
				TourID: msg.TourID,
				StepID: msg.StepID,
				Kind:   msg.Kind,
			},
		})
	case "approval_changed":
		// An approval was created or closed in another Legato process.
		bus.Publish(events.Event{
			Type: events.EventApprovalChanged, At: time.Now(),
			Payload: events.ApprovalChangedPayload{ApprovalID: msg.ItemID, TaskID: msg.TaskID, Kind: msg.Kind},
		})
	case "scope_changed":
		// An agent's page_pin tool changed an origin's pins from a `legato
		// mcp serve` with no broker; Content is the origin.
		bus.Publish(events.Event{Type: events.EventScopeChanged, At: time.Now(), Payload: events.ScopeChangedPayload{Origin: msg.Content}})
	case "agent_stage_changed":
		// An agent reported its stage through its `legato mcp serve`, the
		// CLI or the broker.
		bus.Publish(events.Event{Type: events.EventAgentStageChanged, At: time.Now(), Payload: service.StageChangedFromIPC(msg)})
		if wn != nil {
			wn.NotifyAgentsChanged()
		}
	case "operator_items_changed":
		// An agent posted or cleared an item for the operator through its
		// `legato mcp serve`, or through the broker.
		bus.Publish(events.Event{Type: events.EventOperatorItemsChanged, At: time.Now(), Payload: events.OperatorItemsChangedPayload{TaskID: msg.TaskID, ItemID: msg.ItemID}})
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
		if msg.PID == os.Getpid() {
			// This process's own change, already on its bus.
			return
		}
		// The web server publishes it on the bus, which it shares.
		if notifier, ok := wn.(workflowKindNotifier); ok {
			notifier.NotifyWorkflowChangedKind(msg.WorkflowRunID, msg.WorkflowStepID, msg.Kind)
		} else {
			bus.Publish(events.Event{
				Type: events.EventWorkflowChanged, At: time.Now(),
				Payload: events.WorkflowChangedPayload{RunID: msg.WorkflowRunID, StepID: msg.WorkflowStepID, Kind: msg.Kind},
			})
		}
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
