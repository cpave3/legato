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
		if request.Status != "waiting_input" && request.Status != "waiting_confirmation" && request.Status != "failed" {
			continue
		}
		transition := byID[request.TransitionID]
		missing := []string{}
		_ = json.Unmarshal([]byte(request.MissingInputsJSON), &missing)
		reason := "Workflow inputs required"
		detail := fmt.Sprintf("Task %s entered %s; missing: %v. Open the task to provide inputs or run the workflow manually.", transition.TaskID, transition.ToColumn, missing)
		actions := []AttentionAction{{ID: "provide_inputs", Label: "Provide inputs", Kind: "navigate"}, {ID: "skip", Label: "Skip this trigger", Kind: "execute", RequiresConfirm: true}}
		if request.Status == "waiting_confirmation" {
			reason = "Workflow confirmation required"
			detail = fmt.Sprintf("Task %s entered %s. Run %s?", transition.TaskID, transition.ToColumn, request.WorkflowName)
			actions = []AttentionAction{{ID: "confirm", Label: "Run workflow", Kind: "execute"}, {ID: "skip", Label: "Skip this trigger", Kind: "execute", RequiresConfirm: true}}
		} else if request.Status == "failed" {
			reason = "Automatic workflow failed"
		}
		items = append(items, AttentionInput{Producer: a.Producer(), SourceKey: request.ID, SourceRevision: request.UpdatedAt + ":" + request.Status, TaskID: transition.TaskID, Title: request.WorkflowName, Reason: reason, Detail: detail, Category: "workflow", Urgency: "normal", Navigation: AttentionNavigation{Kind: "task", Target: transition.TaskID}, Actions: actions})
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
	default:
		return fmt.Errorf("unsupported workflow trigger action %q", request.ActionID)
	}
}
