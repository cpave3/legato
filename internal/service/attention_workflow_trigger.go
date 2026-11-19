package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cpave3/legato/internal/engine/store"
)

type WorkflowTriggerAttentionAdapter struct {
	coordinator *WorkflowTriggerCoordinator
	store       *store.Store
}

func NewWorkflowTriggerAttentionAdapter(c *WorkflowTriggerCoordinator, s *store.Store) *WorkflowTriggerAttentionAdapter {
	return &WorkflowTriggerAttentionAdapter{coordinator: c, store: s}
}
func (a *WorkflowTriggerAttentionAdapter) Producer() string { return "workflow_triggers" }
func (a *WorkflowTriggerAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	requests, err := a.store.ListWorkflowTriggerRequests(ctx, "")
	if err != nil {
		return nil, err
	}
	transitions, err := a.store.ListTaskTransitions(ctx, 1000)
	if err != nil {
		return nil, err
	}
	byID := map[string]store.TaskTransition{}
	for _, transition := range transitions {
		byID[transition.ID] = transition
	}
	items := []AttentionInput{}
	for _, request := range requests {
		if request.Status != "waiting_input" && request.Status != "waiting_confirmation" && request.Status != "failed" && request.Status != "held" {
			continue
		}
		transition := byID[request.TransitionID]
		if request.TransitionID == "" {
			// A schedule's or a pull request's event: no column move.
			transition = store.TaskTransition{TaskID: eventTaskID(request)}
		}
		navigation := AttentionNavigation{Kind: "task", Target: transition.TaskID}
		if transition.TaskID == "" {
			navigation = AttentionNavigation{Kind: "workflows", Target: request.WorkflowName}
		}
		if request.Status == "held" {
			items = append(items, AttentionInput{Producer: a.Producer(), SignalKind: AttentionSignalWorkflowTriggerLoopStop, SourceKey: request.ID, SourceRevision: request.UpdatedAt + ":" + request.Status, TaskID: transition.TaskID, Title: request.WorkflowName,
				Reason: "Stopped to prevent a loop", Detail: fmt.Sprintf("%s: %s. Run it anyway, or pause the trigger here.", request.TriggerKind, request.Error), Category: "workflow", Urgency: "high", Navigation: navigation,
				Actions: []AttentionAction{{ID: "run_anyway", Label: "Run anyway", Kind: "execute"}, {ID: "pause_trigger", Label: "Pause trigger", Kind: "execute", RequiresConfirm: true}}})
			continue
		}
		missing := []string{}
		_ = json.Unmarshal([]byte(request.MissingInputsJSON), &missing)
		reason := "Workflow inputs required"
		detail := fmt.Sprintf("Task %s entered %s; missing: %v. Open the task to provide inputs or run the workflow manually.", transition.TaskID, transition.ToColumn, missing)
		if request.TransitionID == "" {
			detail = fmt.Sprintf("%s fired %s; missing: %v.", request.TriggerKind, request.WorkflowName, missing)
		}
		actions := []AttentionAction{{ID: "provide_inputs", Label: "Provide inputs", Kind: "navigate"}, {ID: "skip", Label: "Skip this trigger", Kind: "execute", RequiresConfirm: true}}
		signalKind := AttentionSignalWorkflowTriggerInput
		urgency := "normal"
		if request.Status == "waiting_confirmation" {
			signalKind = AttentionSignalWorkflowTriggerConfirmation
			reason = "Workflow confirmation required"
			detail = fmt.Sprintf("Task %s entered %s. Run %s?", transition.TaskID, transition.ToColumn, request.WorkflowName)
			if request.TransitionID == "" {
				detail = fmt.Sprintf("%s fired. Run %s?", request.TriggerKind, request.WorkflowName)
			}
			actions = []AttentionAction{{ID: "confirm", Label: "Run workflow", Kind: "execute"}, {ID: "skip", Label: "Skip this trigger", Kind: "execute", RequiresConfirm: true}}
		} else if request.Status == "failed" {
			signalKind = AttentionSignalWorkflowTriggerFailed
			urgency = "high"
			reason = "Automatic workflow failed"
		}
		items = append(items, AttentionInput{Producer: a.Producer(), SignalKind: signalKind, SourceKey: request.ID, SourceRevision: request.UpdatedAt + ":" + request.Status, TaskID: transition.TaskID, Title: request.WorkflowName, Reason: reason, Detail: detail, Category: "workflow", Urgency: urgency, Navigation: navigation, Actions: actions})
	}
	return items, nil
}
func (a *WorkflowTriggerAttentionAdapter) Execute(ctx context.Context, request AttentionActionRequest) error {
	switch request.ActionID {
	case "confirm":
		_, err := a.coordinator.Confirm(ctx, request.SourceKey)
		return err
	case "skip":
		return a.coordinator.Skip(ctx, request.SourceKey)
	case "run_anyway":
		_, err := a.coordinator.RunAnyway(ctx, request.SourceKey)
		return err
	case "pause_trigger":
		return a.coordinator.PauseTrigger(ctx, request.SourceKey)
	default:
		return fmt.Errorf("unsupported workflow trigger action %q", request.ActionID)
	}
}
