package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// loopStop says why a trigger must not start another run for an item: it
// started its limit of runs (max_runs_per_day, 3 by default) for that
// item in the last 24 hours. "" lets it start.
func (c *WorkflowTriggerCoordinator) loopStop(ctx context.Context, definition *workflowengine.Definition, kind, item string) (string, error) {
	common, ok := definition.On.Common(kind)
	if !ok || item == "" {
		return "", nil
	}
	limit := common.Limit()
	started, err := c.store.CountStartedTriggerRuns(ctx, definition.Name, kind, item, c.clock().Add(-24*time.Hour))
	if err != nil {
		return "", err
	}
	if started < limit {
		return "", nil
	}
	return fmt.Sprintf("stopped to prevent a loop: %s started %d runs for %s in the last day (max_runs_per_day %d)", definition.Name, started, item, limit), nil
}

// RunAnyway starts a request the loop stop held, past the limit.
func (c *WorkflowTriggerCoordinator) RunAnyway(ctx context.Context, requestID string) (*store.WorkflowTriggerRequest, error) {
	request, err := c.store.GetWorkflowTriggerRequest(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if request.Status != "held" {
		return nil, fmt.Errorf("trigger request is %s, not held", request.Status)
	}
	if err := c.store.TransitionWorkflowTriggerRequest(ctx, request.ID, "held", "pending"); err != nil {
		return nil, err
	}
	request.Status = "pending"
	var definition workflowengine.Definition
	if err := json.Unmarshal([]byte(request.DefinitionJSON), &definition); err != nil {
		return nil, fmt.Errorf("decode workflow definition: %w", err)
	}
	inputs := map[string]string{}
	_ = json.Unmarshal([]byte(request.InputsJSON), &inputs)
	if request.TransitionID != "" {
		transition, err := c.store.GetTaskTransition(ctx, request.TransitionID)
		if err != nil {
			return nil, err
		}
		if err := c.processColumn(ctx, *transition, &definition, request, inputs, false, true); err != nil {
			return nil, err
		}
	} else if err := c.processEvent(ctx, &definition, request, inputs, false, true); err != nil {
		return nil, err
	}
	return c.store.GetWorkflowTriggerRequest(ctx, requestID)
}

// PauseTrigger pauses, in this Legato, the trigger that made a held
// request, and lets the request go.
func (c *WorkflowTriggerCoordinator) PauseTrigger(ctx context.Context, requestID string) error {
	request, err := c.store.GetWorkflowTriggerRequest(ctx, requestID)
	if err != nil {
		return err
	}
	if request.Status != "held" {
		return fmt.Errorf("trigger request is %s, not held", request.Status)
	}
	if err := c.store.SetWorkflowTriggerSwitch(ctx, request.WorkflowName, request.TriggerKind, false); err != nil {
		return err
	}
	return c.update(ctx, request.ID, "skipped", "", request.InputsJSON, `[]`, "the trigger was paused here to stop a loop")
}
