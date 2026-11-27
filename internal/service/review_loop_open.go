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
	hosts := make([]string, 0, len(attempts))
	for _, attempt := range attempts {
		hosts = append(hosts, attempt.TaskID)
	}
	// A loop an open step handed to its run is kept while it is open,
	// whatever step of the run waits (a failed run's too, for Retry).
	stepHosts, err := stepOwnedLoopHosts(ctx, db)
	if err != nil {
		return nil, err
	}
	for _, host := range append(hosts, stepHosts...) {
		run, err := readReviewRun(ctx, db, host)
		if err != nil {
			continue // a step that has not recorded its loop yet has no clone
		}
		loop, err := db.GetHandoffReview(ctx, host)
		if err != nil {
			return nil, err
		}
		if !reviewLoopOpen(loop) {
			continue
		}
		if run.CloneID != 0 {
			out[run.CloneID] = true
		}
		if err := add(host, run.Repo); err != nil {
			return nil, err
		}
		if err := add(run.AuthorTask, run.Repo); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// stepOwnedLoopHosts are the host tasks of the open loops that an open
// step (github.pr.review.open) handed to its run.
func stepOwnedLoopHosts(ctx context.Context, db *store.Store) ([]string, error) {
	loops, err := db.ListOpenReviewLoops(ctx)
	if err != nil {
		return nil, err
	}
	var hosts []string
	for _, l := range loops {
		if l.OwnerKind == store.ReviewLoopOwnerRun && db.ReviewLoopOpenedByStep(ctx, l.AttemptID) {
			hosts = append(hosts, l.HostTask)
		}
	}
	return hosts, nil
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
