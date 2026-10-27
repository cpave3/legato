package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/cpave3/legato/internal/engine/reviewbriefing"
	"github.com/cpave3/legato/internal/engine/store"
)

// ErrNoReviewBriefing is returned when an agent has no checkout to read a
// briefing from: it is not running, or it works outside a Git repository.
var ErrNoReviewBriefing = errors.New("the agent has no checkout to read a review briefing from")

// ErrUnknownReviewPage is returned for a path that is not a page of the
// agent's briefing.
var ErrUnknownReviewPage = reviewbriefing.ErrUnknownPage

// ReviewBriefingService reads the review briefing (.review/) a running
// agent's branch carries, so it can be read before the pull request exists.
// Nothing is stored: the checkout is read each time.
type ReviewBriefingService struct {
	store *store.Store
}

func NewReviewBriefingService(s *store.Store) *ReviewBriefingService {
	return &ReviewBriefingService{store: s}
}

// Briefing lists the pages of the briefing in the agent's checkout. A branch
// without one has no pages.
func (s *ReviewBriefingService) Briefing(ctx context.Context, taskID string) (*reviewbriefing.Index, error) {
	dir, err := s.checkout(ctx, taskID)
	if err != nil {
		return nil, err
	}
	index, err := reviewbriefing.Find(ctx, dir)
	if errors.Is(err, reviewbriefing.ErrNoCheckout) {
		return nil, ErrNoReviewBriefing
	}
	return index, err
}

// Page is one page of the agent's briefing, with the files it uses put in.
func (s *ReviewBriefingService) Page(ctx context.Context, taskID, path string) (string, error) {
	index, err := s.Briefing(ctx, taskID)
	if err != nil {
		return "", err
	}
	return reviewbriefing.Render(ctx, index, path)
}

// checkout is the directory the agent works in: where its execution
// started, else the directory the task last recorded.
func (s *ReviewBriefingService) checkout(ctx context.Context, taskID string) (string, error) {
	session, err := s.store.GetAgentSessionByTaskID(ctx, taskID)
	if errors.Is(err, store.ErrNotFound) {
		return "", ErrNoReviewBriefing
	}
	if err != nil {
		return "", err
	}
	if session.ExecutionID != "" {
		execution, err := s.store.GetExecution(ctx, session.ExecutionID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return "", err
		}
		if execution != nil && execution.WorkingDir != "" {
			return execution.WorkingDir, nil
		}
	}
	task, err := s.store.GetTask(ctx, taskID)
	if errors.Is(err, store.ErrNotFound) {
		return "", ErrNoReviewBriefing
	}
	if err != nil {
		return "", err
	}
	codeContext, err := store.ParseCodeContext(task.CodeContext)
	if err != nil {
		return "", fmt.Errorf("parsing task code context: %w", err)
	}
	if codeContext == nil || codeContext.WorkingDir == "" {
		return "", ErrNoReviewBriefing
	}
	return codeContext.WorkingDir, nil
}
