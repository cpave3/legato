package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/cpave3/legato/internal/engine/store"
)

// Kinds of loop a lookup reports: whose the loop is.
const (
	// ReviewLoopKindRun is a loop a workflow run's step opened.
	ReviewLoopKindRun = "run"
	// ReviewLoopKindChildRun is a loop a child run opened: a run a
	// workflow.run step started (LEG-489).
	ReviewLoopKindChildRun = "child_run"
	// ReviewLoopKindHandoff is a handed-off card's loop.
	ReviewLoopKindHandoff = "handoff"
)

// ErrBadReviewLoopQuery is a lookup with a repository that is not
// owner/name or a pull request number that is not positive.
var ErrBadReviewLoopQuery = errors.New("a review loop is looked up by repo (owner/name) and pr (a pull request number)")

// FoundReviewLoop is the loop that holds a pull request, as
// GET /api/review-loops reports it (spec LEG-559 § API contract).
type FoundReviewLoop struct {
	LoopID string `json:"loop_id"`
	// Kind is ReviewLoopKindRun, ReviewLoopKindChildRun or
	// ReviewLoopKindHandoff.
	Kind string `json:"kind"`
	// RunID and StepPath are the owning run's step; empty for a handoff.
	RunID    string `json:"run_id"`
	StepPath string `json:"step_path"`
	Phase    string `json:"phase"`
	State    string `json:"state"`
	// Held is a steward the operator stopped watching (slice 3); false
	// until then.
	Held    bool   `json:"held"`
	Outcome string `json:"outcome"`
	// Link is where the web UI shows the loop: the run's page, or the
	// handed-off card on the board.
	Link string `json:"link"`
}

// CheckReviewLoopQuery says whether repo and pr can name a pull request:
// ErrBadReviewLoopQuery when repo is not owner/name or pr is not positive.
func CheckReviewLoopQuery(repo string, pr int) error {
	if validRepo(repo) != nil || pr <= 0 {
		return fmt.Errorf("%w: got repo %q, pr %d", ErrBadReviewLoopQuery, repo, pr)
	}
	return nil
}

// ReviewLoopLookup finds the loop that holds a pull request, whichever
// caller opened it, from the claims on the loop record. It reads only, so
// a process without the review engine can answer.
type ReviewLoopLookup struct {
	db *store.Store
}

// NewReviewLoopLookup reads loops from db.
func NewReviewLoopLookup(db *store.Store) *ReviewLoopLookup {
	return &ReviewLoopLookup{db: db}
}

// ActiveReviewLoop returns the loop that holds repo's pull request pr (the
// repository's case ignored), nil when none does. A repository that is not
// owner/name, or a number that is not positive, is ErrBadReviewLoopQuery.
func (l *ReviewLoopLookup) ActiveReviewLoop(ctx context.Context, repo string, pr int) (*FoundReviewLoop, error) {
	if err := CheckReviewLoopQuery(repo, pr); err != nil {
		return nil, err
	}
	loop, err := l.db.ActiveReviewLoop(ctx, repo, pr)
	if err != nil || loop == nil {
		return nil, err
	}
	kind, err := l.kindOf(ctx, loop)
	if err != nil {
		return nil, err
	}
	_, link := reviewLoopName(loop)
	return &FoundReviewLoop{
		LoopID: loop.ID, Kind: kind, RunID: loop.RunID, StepPath: loop.StepPath,
		Phase: loop.Phase, State: loop.State, Held: loop.Held, Outcome: loop.Outcome, Link: link,
	}, nil
}

// kindOf says whose a loop is: a handoff's, or a run's, which is a child
// run when a workflow.run step started it.
func (l *ReviewLoopLookup) kindOf(ctx context.Context, loop *store.ReviewLoop) (string, error) {
	if loop.OwnerKind == store.ReviewLoopOwnerHandoff {
		return ReviewLoopKindHandoff, nil
	}
	run, err := l.db.GetWorkflowRun(ctx, loop.RunID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return ReviewLoopKindRun, nil
	case err != nil:
		return "", err
	case run.Run.ParentRunID != "":
		return ReviewLoopKindChildRun, nil
	}
	return ReviewLoopKindRun, nil
}
