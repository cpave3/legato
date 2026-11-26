package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// The steward's budget and the operator's hold on it (spec LEG-559 § The
// steward › Budgets, Held and End; LEG-592):
//
//   - a push past the loop's max_reentries makes it stuck at its watch: no
//     pass starts and no fresh agent; Watch again allows one more re-entry;
//   - Stop watching holds it: nothing is sent and nothing is reviewed, but a
//     close (and a merge) still ends the loop, and so does idleness; Watch
//     again resumes it, and a push made while it was held re-enters once;
//   - End ends it with outcome ended, wherever it is;
//   - seven days with no push, comment, review or check change end it with
//     outcome idle.

// The steward's operator actions by loop ID (ActOnLoop), beside Another
// pass, Message the author and End.
const (
	ReviewLoopActionStopWatching = "stop_watching"
	ReviewLoopActionWatchAgain   = "watch_again"
)

// ErrStewardNotWatching refuses Stop watching to a steward that does not
// watch an approved pull request now: in review after a push, stuck, or
// already held.
var ErrStewardNotWatching = errors.New("the steward is not watching an approved pull request now")

// ErrStewardNotHeld refuses Watch again to a steward that is neither held
// nor stuck at its watch.
var ErrStewardNotHeld = errors.New("the steward is neither held nor stuck at its watch")

// reentryBudgetReason is a steward stuck at its re-entry budget: the
// re-entries used and allowed.
const reentryBudgetReason = "a push came after %d re-entries, this loop's limit of %d (max_reentries), so no pass started: Watch again allows one more re-entry, End ends the steward"

// approvalStands says a loop's approval stands at the heads it was
// confirmed at: approved and watched, or stuck at the watch (a push past
// the budget). A loop in review after a push has a later pass.
func approvalStands(loop *store.HandoffReview) bool {
	return loop != nil && (loop.Phase == ReviewPhaseClean || loop.Phase == ReviewPhaseStuck) &&
		!loop.ConfirmClean && loop.ConfirmedAt != "" && loop.CleanPass == loop.Pass
}

// idlePast says a stewarding loop's idle deadline has passed.
func (s *ReviewPassService) idlePast(rec *store.ReviewLoop) bool {
	deadline, err := time.Parse(time.RFC3339, rec.IdleDeadline)
	return err == nil && !s.opts.Now().Before(deadline)
}

// endSteward closes a stewarding loop with outcome (idle, ended): the
// watch waiting on it completes with that outcome, and the close step
// gives it.
func (s *ReviewPassService) endSteward(ctx context.Context, rec *store.ReviewLoop, outcome string) error {
	_, err := s.Close(ctx, rec.ID, outcome)
	return err
}

// stewardHolds says a stewarding loop sends nothing now: held, or stuck.
func (s *ReviewPassService) stewardHolds(ctx context.Context, rec *store.ReviewLoop) bool {
	if rec.Held {
		return true
	}
	loop, err := s.db.GetHandoffReview(ctx, rec.HostTask)
	return err == nil && loop != nil && loop.LoopID == rec.ID && loop.Phase == ReviewPhaseStuck
}

// reviewStewardActor is a caller whose loops a steward may watch: it takes
// Stop watching and Watch again while the step that owns the loop waits.
type reviewStewardActor interface {
	stopWatchingOn(ctx context.Context, l *store.ReviewLoop) error
	watchAgainOn(ctx context.Context, l *store.ReviewLoop) error
}

// stewardActorFor is the caller that takes a steward's actions on a loop.
func (s *ReviewPassService) stewardActorFor(ctx context.Context, loopID string) (*store.ReviewLoop, reviewStewardActor, error) {
	l, err := s.openLoopByID(ctx, loopID)
	if err != nil {
		return nil, nil, err
	}
	actor, err := s.actorFor(l)
	if err != nil {
		return nil, nil, err
	}
	steward, ok := actor.(reviewStewardActor)
	if !ok || !l.Steward {
		return nil, nil, fmt.Errorf("review loop %s has no steward: %w", loopID, ErrStewardNotWatching)
	}
	return l, steward, nil
}

// StopWatchingOn is Stop watching on a loop by its ID: its steward holds,
// sending nothing and reviewing nothing until Watch again. It only writes
// the record.
func (s *ReviewPassService) StopWatchingOn(ctx context.Context, loopID string) error {
	l, actor, err := s.stewardActorFor(ctx, loopID)
	if err != nil {
		return err
	}
	return actor.stopWatchingOn(ctx, l)
}

// WatchAgainOn is Watch again on a loop by its ID: a held steward watches
// again (a push made while it was held re-enters once, on the tick); a
// steward stuck at its watch gets one more re-entry. It only writes the
// record.
func (s *ReviewPassService) WatchAgainOn(ctx context.Context, loopID string) error {
	l, actor, err := s.stewardActorFor(ctx, loopID)
	if err != nil {
		return err
	}
	return actor.watchAgainOn(ctx, l)
}

// stopWatching holds a steward that watches an approved pull request.
func (s *ReviewPassService) stopWatching(ctx context.Context, loopID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, loop, err := s.stewardState(ctx, loopID)
	if err != nil {
		return err
	}
	if rec.Held || !confirmed(loop) {
		return fmt.Errorf("review loop %s: %w", loopID, ErrStewardNotWatching)
	}
	if _, err := s.db.UpdateReviewLoop(ctx, rec.ID, rec.Rev, func(t *store.ReviewLoopTx) error {
		t.Loop.Held = true
		return nil
	}); err != nil {
		return err
	}
	s.changed(rec.HostTask)
	return nil
}

// watchAgain resumes a held steward, or gives a steward stuck at its watch
// one more re-entry: its allowed re-entries become one past those used
// (never fewer than before), and it watches again, so the push that went
// past the budget re-enters on the tick.
func (s *ReviewPassService) watchAgain(ctx context.Context, loopID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, loop, err := s.stewardState(ctx, loopID)
	if err != nil {
		return err
	}
	switch {
	case rec.Held:
		if _, err := s.db.UpdateReviewLoop(ctx, rec.ID, rec.Rev, func(t *store.ReviewLoopTx) error {
			t.Loop.Held = false
			return nil
		}); err != nil {
			return err
		}
	case loop.Phase == ReviewPhaseStuck && approvalStands(loop):
		if _, err := s.db.UpdateReviewLoop(ctx, rec.ID, rec.Rev, func(t *store.ReviewLoopTx) error {
			t.Loop.MaxReentries = max(t.Loop.MaxReentries, t.Loop.Reentries+1)
			return nil
		}); err != nil {
			return err
		}
		loop.Phase, loop.Reason = ReviewPhaseClean, ""
		if err := s.save(ctx, loop); err != nil {
			return err
		}
	default:
		return fmt.Errorf("review loop %s: %w", loopID, ErrStewardNotHeld)
	}
	s.changed(rec.HostTask)
	return nil
}

// stewardState is a stewarding loop's record and its loop, refused when
// its steward has not begun watching.
func (s *ReviewPassService) stewardState(ctx context.Context, loopID string) (*store.ReviewLoop, *store.HandoffReview, error) {
	rec, err := s.db.GetReviewLoop(ctx, loopID)
	if err != nil {
		return nil, nil, err
	}
	if !stewarding(rec) {
		return nil, nil, fmt.Errorf("review loop %s is not stewarded yet: %w", loopID, ErrStewardNotWatching)
	}
	loop, err := s.db.GetHandoffReview(ctx, rec.HostTask)
	if err != nil {
		return nil, nil, err
	}
	if loop == nil || loop.LoopID != rec.ID {
		return nil, nil, fmt.Errorf("review loop %s: %w", loopID, ErrStewardNotWatching)
	}
	return rec, loop, nil
}

// The run caller's steward actions (reviewStewardActor): each acts only
// while the step that owns the loop waits on it.

func (c *ReviewRunCaller) stopWatchingOn(ctx context.Context, l *store.ReviewLoop) error {
	if !c.stepWaits(ctx, l) {
		return ErrReviewNotWaiting
	}
	return c.engine.stopWatching(ctx, l.ID)
}

func (c *ReviewRunCaller) watchAgainOn(ctx context.Context, l *store.ReviewLoop) error {
	if !c.stepWaits(ctx, l) {
		return ErrReviewNotWaiting
	}
	return c.engine.watchAgain(ctx, l.ID)
}
