package service

import (
	"context"
	"errors"
	"log"

	"github.com/cpave3/legato/internal/engine/store"
)

// mayWrite is the check before each GitHub write of a loop (a review, an
// answer, a reply, a thread resolved; spec LEG-559 D11), made in one
// transaction: the loop has not ended, its pass (when not 0) is still
// submitted, and its owner is the same workflow run attempt or handoff and
// still holds it. False drops the write and every later one: a pass in
// flight is discarded and its reviewer stopped, and a loop its owner left
// (a run cancelled where no engine runs, a handoff recalled) ends and frees
// its reviewer slot. An error reading the store holds the write back, to be
// tried again.
//
// The check does not cover what happens between it and the write: a cancel
// that lands while a write is under way does not stop that write, only the
// ones after it.
func (s *ReviewPassService) mayWrite(ctx context.Context, l ReviewLoop, pass int) (bool, error) {
	err := s.db.CheckHandoffReviewWrite(ctx, l.TaskID, pass, l.Owner)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, store.ErrReviewPassNotSubmitted):
		return false, nil // set aside already: nothing more of it posts
	case !errors.Is(err, store.ErrReviewLoopClosed) && !errors.Is(err, store.ErrReviewLoopNotOwned):
		return false, err
	}
	reason := ReviewOutcomeClosed
	if l.Owner.AttemptID != "" {
		reason = ReviewOutcomeCancelled
	}
	ended, endErr := s.db.EndHandoffReview(ctx, l.TaskID, reason, "the review loop ended before the pass was posted", s.now())
	if endErr != nil {
		return false, endErr
	}
	if pass > 0 {
		_ = s.opts.Agents.KillAgentInSlot(ctx, l.TaskID, SlotReviewer)
	}
	log.Printf("review passes: %s: nothing more is posted: %v", l.TaskID, err)
	if ended {
		s.setWaiting(l.TaskID, "")
		for _, caller := range s.callers {
			caller.Changed(l.TaskID)
		}
	}
	return false, nil
}
