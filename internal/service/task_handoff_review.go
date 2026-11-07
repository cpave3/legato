package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/cpave3/legato/internal/engine/store"
)

// A main's actions on a stuck review of an away task, and the worker's
// side of them: run another pass, and message the author. See
// docs/ai/integrations.md § Review passes.

// Review actions a main asks of a worker.
const (
	ReviewActionAnotherPass = "another-pass"
	ReviewActionMessage     = "message"
)

// HandoffReviewPath is the worker's route for a review action on a handoff.
func HandoffReviewPath(handoffID, action string) string {
	return HandoffCallPath + "/" + url.PathEscape(handoffID) + "/review/" + action
}

// ReviewAction carries out a main's review action on one of its handoffs.
func (s *HandoffWorkerService) ReviewAction(ctx context.Context, mainInstanceID, handoffID, action, text string) error {
	if s.opts.Reviews == nil {
		return handoffErr("no_review_passes", "this worker does not run review passes")
	}
	h, err := s.db.GetHandoff(ctx, mainInstanceID, handoffID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && h.ClosedAt != nil) {
		return handoffErr("not_found", "no open handoff %s", handoffID)
	}
	if err != nil {
		return err
	}
	switch action {
	case ReviewActionAnotherPass:
		err = s.opts.Reviews.AnotherPass(ctx, h.TaskID)
		if errors.Is(err, ErrReviewNotStuck) {
			return handoffErr("not_stuck", "the review of %s is not stuck", handoffID)
		}
	case ReviewActionMessage:
		err = s.opts.Reviews.MessageAuthor(ctx, h.TaskID, text)
	default:
		return handoffErr("bad_request", "no review action %q", action)
	}
	return err
}

// AnotherPass asks the worker to run another pass of an away task's stuck
// review.
func (s *TaskHandoffService) AnotherPass(ctx context.Context, taskID string) error {
	return s.reviewAction(ctx, taskID, ReviewActionAnotherPass, "")
}

// MessageAuthor sends the away task's author the person's words.
func (s *TaskHandoffService) MessageAuthor(ctx context.Context, taskID, text string) error {
	return s.reviewAction(ctx, taskID, ReviewActionMessage, text)
}

func (s *TaskHandoffService) reviewAction(ctx context.Context, taskID, action, text string) error {
	row, err := s.db.OpenAwayTask(ctx, taskID)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: task %s is not away", ErrReturnRefused, taskID)
	}
	if err != nil {
		return err
	}
	away := awayTaskFromRow(*row)
	name := orUnknown(away.WorkerName)
	if away.Review == nil {
		return &handoffWorkerError{ErrWorkerRefused, fmt.Sprintf("%s does not run review passes for this task", name)}
	}
	client, err := s.opts.Workers.Client(ctx, row.WorkerInstanceID)
	if err != nil {
		return err
	}
	var body any
	if action == ReviewActionMessage {
		body = map[string]string{"text": text}
	}
	if err := client.Call(ctx, http.MethodPost, HandoffReviewPath(row.HandoffID, action), body, nil); err != nil {
		if errors.Is(err, ErrWorkerUnreachable) {
			s.opts.Workers.MarkUnreachable(ctx, client.Worker.InstanceID)
		}
		var refusal *WorkerRefusal
		if errors.As(err, &refusal) {
			return &handoffWorkerError{ErrWorkerRefused, fmt.Sprintf("%s refused: %s", name, refusal.Message)}
		}
		return err
	}
	return nil
}

// WorkerReviewPasses is what a worker says of its review passes, for the
// Hand off dialog; nil when it runs none.
func (s *TaskHandoffService) WorkerReviewPasses(ctx context.Context, workerID string) (*ReviewPassesAdvert, error) {
	client, err := s.opts.Workers.Client(ctx, workerID)
	if err != nil {
		return nil, err
	}
	meta, err := client.Meta(ctx)
	if err != nil {
		return nil, err
	}
	return meta.ReviewPasses, nil
}
