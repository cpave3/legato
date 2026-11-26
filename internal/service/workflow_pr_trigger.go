package service

import (
	"context"
	"errors"
	"strconv"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// ChangesRequested fires each workflow's pr.changes_requested trigger for
// a new review requesting changes on a task's pull request: one request
// per review (its id the event key), counted by the task for the loop stop.
// A PR an open agent-authored review loop holds is the loop's: the review
// reaches its author through the loop, so each request is recorded as
// skipped, naming the loop. A person-authored loop's PR still fires.
func (c *WorkflowTriggerCoordinator) ChangesRequested(ctx context.Context, event events.PRChangesRequestedPayload) error {
	skip, err := c.heldByAgentAuthoredLoop(ctx, event.Repo, event.Number)
	if err != nil {
		return err
	}
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
			Key: event.Repo + "#" + strconv.Itoa(event.Number) + "/review/" + strconv.FormatInt(event.ReviewID, 10), Item: event.TaskID, TaskID: event.TaskID, Context: eventContext, Skip: skip}); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// heldByAgentAuthoredLoop is why a changes-requested review on repo#pr
// starts nothing, "" when no open agent-authored loop holds the PR.
func (c *WorkflowTriggerCoordinator) heldByAgentAuthoredLoop(ctx context.Context, repo string, pr int) (string, error) {
	loop, err := c.store.ActiveReviewLoop(ctx, repo, pr)
	if err != nil || loop == nil || loop.State != store.ReviewLoopOpenState || !loop.AgentAuthor {
		return "", err
	}
	return "held by review loop " + loop.ID, nil
}
