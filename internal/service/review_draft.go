package service

import (
	"context"
	"fmt"
	"log"
	"slices"

	"github.com/cpave3/legato/internal/engine/store"
)

// Draft until approved (LEG-712). An agent-authored review loop that marks
// its stack ready (agent_author and ready) keeps GitHub's draft state as
// its signal, so "ready for review" means approved at the head: it turns
// its open pull requests into drafts as the author App when it opens, and
// again whenever they go back to review (a pass starts, the author has
// feedback, a counted check fails after approval). Only github.pr.ready,
// after a confirmed approval at the heads, marks them ready.
//
// A refusal never fails the loop: it is recorded on the layer
// (ReviewLoopPR.DraftProblem), shown on the run and Shipping pages, and
// tried again at the next return to review.

// ReviewDraftGitHub turns a pull request back into a draft as the author
// App (ReviewPublisher.MarkDraft): false when it was a draft already.
type ReviewDraftGitHub interface {
	MarkDraft(ctx context.Context, repo string, number int) (bool, error)
}

// Why a loop keeps its pull requests drafts, for the log.
const (
	draftWhyOpened = "the review loop opened"
	draftWhyPass   = "a review pass started"
	draftWhyAuthor = "the author has feedback"
	draftWhyCheck  = "a counted check failed after approval"
)

// keepsDraft says whether a loop keeps its pull requests drafts under
// review: an open loop on an agent's pull requests that marks them ready
// once approved. A loop without ready would leave them drafts for good,
// and a person's pull request is theirs to mark.
func keepsDraft(rec *store.ReviewLoop) bool {
	return rec != nil && rec.State == store.ReviewLoopOpenState && rec.AgentAuthor && rec.Ready
}

// keepDraft turns a loop's open layers (numbers, or all when nil) back
// into drafts as it goes back to review, when the loop keeps drafts and
// still owns its pull requests.
func (s *ReviewPassService) keepDraft(ctx context.Context, l ReviewLoop, numbers []int, why string) {
	rec, err := s.db.CurrentReviewLoop(ctx, l.TaskID)
	if err != nil || !keepsDraft(rec) {
		return
	}
	if err := s.db.CheckHandoffReviewWrite(ctx, l.TaskID, 0, l.Owner); err != nil {
		return // no longer the owner's to write to GitHub for
	}
	s.draftLayers(ctx, rec, numbers, why)
}

// draftLayers turns each open, ready layer of rec named in numbers (all
// when nil) into a draft, recording what became of each.
func (s *ReviewPassService) draftLayers(ctx context.Context, rec *store.ReviewLoop, numbers []int, why string) {
	if !keepsDraft(rec) {
		return
	}
	layers, err := s.db.ListReviewLoopPRs(ctx, rec.ID)
	if err != nil {
		log.Printf("review passes: %s: the layers to keep drafts: %v", rec.ID, err)
		return
	}
	drafts, ok := s.opts.GitHub.(ReviewDraftGitHub)
	for _, layer := range layers {
		if numbers != nil && !slices.Contains(numbers, layer.Number) {
			continue
		}
		if layer.State == "merged" || layer.State == "closed" {
			continue
		}
		if !ok {
			s.draftProblem(rec, layer, "this Legato cannot turn pull requests into drafts")
			continue
		}
		pr, state, err := s.opts.GitHub.PullRequest(ctx, rec.Repo, layer.Number)
		if err != nil {
			log.Printf("review passes: %s: reading #%d to keep it a draft (%s): %v", rec.ID, layer.Number, why, err)
			continue
		}
		if state != "open" || pr.Draft {
			continue
		}
		if _, err := drafts.MarkDraft(ctx, rec.Repo, layer.Number); err != nil {
			s.draftProblem(rec, layer, fmt.Sprintf("the author App cannot turn #%d into a draft: %v", layer.Number, err))
			continue
		}
		log.Printf("review passes: %s: turned %s#%d into a draft: %s", rec.ID, rec.Repo, layer.Number, why)
		if err := s.db.SetReviewLoopPRDraft(ctx, rec.ID, layer.Number, true, s.now(), ""); err != nil {
			log.Printf("review passes: %s: recording #%d as a draft: %v", rec.ID, layer.Number, err)
		}
		s.changed(rec.HostTask)
	}
}

// draftProblem records why a layer could not be turned into a draft,
// logging it once per change of reason.
func (s *ReviewPassService) draftProblem(rec *store.ReviewLoop, layer store.ReviewLoopPR, problem string) {
	if layer.DraftProblem == problem {
		return
	}
	log.Printf("review passes: %s: %s", rec.ID, problem)
	if err := s.db.SetReviewLoopPRDraft(context.Background(), rec.ID, layer.Number, true, "", problem); err != nil {
		log.Printf("review passes: %s: recording #%d's draft problem: %v", rec.ID, layer.Number, err)
		return
	}
	s.changed(rec.HostTask)
}

// backToAuthor tells the loop's caller the author has feedback, and keeps
// its pull requests drafts while they work on it.
func (s *ReviewPassService) backToAuthor(ctx context.Context, l ReviewLoop) {
	l.stage(ctx, ReviewStageAuthor)
	s.keepDraft(ctx, l, nil, draftWhyAuthor)
}
