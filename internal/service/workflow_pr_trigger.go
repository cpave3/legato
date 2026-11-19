package service

import (
	"context"
	"errors"
	"strconv"

	"github.com/cpave3/legato/internal/engine/events"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// ChangesRequested fires each workflow's pr.changes_requested trigger for
// a new review requesting changes on a task's pull request: one request
// per review (its id the event key), counted by the task for the loop stop.
func (c *WorkflowTriggerCoordinator) ChangesRequested(ctx context.Context, event events.PRChangesRequestedPayload) error {
	var errs []error
	for _, definition := range c.definitions().Definitions {
		if definition.On.PRChangesRequested == nil {
			continue
		}
		eventContext := map[string]any{"event": map[string]any{
			"task": event.TaskID, "repo": event.Repo, "pr_number": event.Number, "pr_url": event.URL,
			"reviewer": event.Reviewer, "review_id": event.ReviewID,
		}}
		if _, err := c.Fire(ctx, TriggerEvent{Definition: definition, Kind: workflowengine.TriggerPRChangesRequested,
			Key: event.Repo + "#" + strconv.Itoa(event.Number) + "/review/" + strconv.FormatInt(event.ReviewID, 10), Item: event.TaskID, TaskID: event.TaskID, Context: eventContext}); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
