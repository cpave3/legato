package service

import (
	"context"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// What other parts of Legato ask of open review loops, read from the
// store rather than from a review engine, so a process that runs none (the
// one sweeping job clones, or updating) still sees every loop: a worker's
// handed-off cards with a review loop and the github.pr.review_passes
// step's runs, both on the loop record.

// OpenReviewLoopClones are the IDs of the live job clones open review loops
// use: an open handoff's card clone that its loop reviews, and a waiting
// run's reviewer clone and its agent author's clone, of the loop's
// repository. The job-clone sweep spares them.
func OpenReviewLoopClones(ctx context.Context, db *store.Store) (map[int64]bool, error) {
	out := map[int64]bool{}
	add := func(taskID, repo string) error {
		if taskID == "" {
			return nil
		}
		clones, err := db.ListLiveTaskJobClones(ctx, taskID)
		if err != nil {
			return err
		}
		for _, c := range clones {
			if strings.EqualFold(c.Repo, repo) {
				out[c.ID] = true
			}
		}
		return nil
	}
	handoffs, err := db.ListOpenHandoffs(ctx)
	if err != nil {
		return nil, err
	}
	for _, h := range handoffs {
		loop, err := db.GetHandoffReview(ctx, h.TaskID)
		if err != nil {
			return nil, err
		}
		if !reviewLoopOpen(loop) {
			continue
		}
		if err := add(h.TaskID, h.Repo); err != nil {
			return nil, err
		}
	}
	attempts, err := db.ListWaitingWorkflowAttemptsByExecutor(ctx, ReviewPassesStepType)
	if err != nil {
		return nil, err
	}
	// A loop an open step handed to its run is kept while it is open,
	// whatever step of the run waits (a failed run's too, for Retry).
	stepLoops, err := db.ListOpenReviewLoops(ctx)
	if err != nil {
		return nil, err
	}
	for _, l := range stepLoops {
		if l.OwnerKind == store.ReviewLoopOwnerRun && db.ReviewLoopOpenedByStep(ctx, l.AttemptID) {
			attempts = append(attempts, store.WorkflowStepAttempt{TaskID: l.HostTask})
		}
	}
	for _, attempt := range attempts {
		run, err := readReviewRun(ctx, db, attempt.TaskID)
		if err != nil {
			continue // a step that has not recorded its loop yet has no clone
		}
		loop, err := db.GetHandoffReview(ctx, attempt.TaskID)
		if err != nil {
			return nil, err
		}
		if !reviewLoopOpen(loop) {
			continue
		}
		if run.CloneID != 0 {
			out[run.CloneID] = true
		}
		if err := add(attempt.TaskID, run.Repo); err != nil {
			return nil, err
		}
		if err := add(run.AuthorTask, run.Repo); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// reviewLoopOpen says whether a loop record is a loop that has not ended.
func reviewLoopOpen(loop *store.HandoffReview) bool {
	return loop != nil && loop.Phase != ReviewPhaseEnded && loop.Phase != ReviewPhaseSkipped
}

// ReviewPassMidPost says whether a review pass is being posted to GitHub: the
// reviewer submitted it and its loop, still reviewing, has not finished
// posting it (a post under way, or one a rate limit left for the next tick).
// An automatic update waits for it.
func ReviewPassMidPost(ctx context.Context, db *store.Store) (bool, error) {
	return db.AnyCurrentReviewPassIn(ctx, ReviewPhaseReviewing, ReviewPassSubmitted)
}
