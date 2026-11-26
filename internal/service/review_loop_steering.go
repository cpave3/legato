package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// ReviewLoopMessenger queues a message for an agent's next turn.
type ReviewLoopMessenger interface {
	QueueMessage(ctx context.Context, taskID, message string) error
}

// ReviewLoopSteering is the operator steering a loop named by its ID from the
// host, outside any session (`legato review-pass
// another-pass|message|stop-watching|watch-again|end --loop`). It runs in a process without the engine's callers: today's
// task-keyed engine actions on the loop's host task, whose state the
// engine's own process picks up on its next tick. The engine's operator
// actions by loop ID (AnotherPassOn and the rest, LEG-582) need the
// callers, which only the engine's process has, so this writes the record.
type ReviewLoopSteering struct {
	DB *store.Store
	// Engine is an engine on DB for its task-keyed actions; it never ticks.
	Engine *ReviewPassService
	// Agents tells the author; only MessageAuthor needs it.
	Agents ReviewLoopMessenger
}

// AnotherPass is Another pass: a stuck loop's next pass (one past its
// limit, or a retry), or, for a workflow run's loop that waits for its
// author, the next pass now. ErrReviewNotStuck otherwise.
func (s ReviewLoopSteering) AnotherPass(ctx context.Context, loopID string) error {
	l, err := OperatorReviewLoop(ctx, s.DB, loopID)
	if err != nil {
		return err
	}
	switch {
	case l.Phase == ReviewPhaseStuck:
		return s.Engine.RecordAnotherPass(ctx, l.HostTask)
	case l.Phase == ReviewPhaseWaitingForAuthor && l.OwnerKind == store.ReviewLoopOwnerRun && l.HandoffID == "":
		return s.Engine.NextPass(ctx, l.HostTask)
	}
	return fmt.Errorf("review loop %s is %s: %w", loopID, l.Phase, ErrReviewNotStuck)
}

// MessageAuthor is Message the author: the operator's words, tagged, as
// the author agent's next message. An author that is not an agent is
// refused with ErrReviewAuthorNotAgent; an author whose agent is not running
// is refused, since this process cannot resume it as the engine's callers do.
func (s ReviewLoopSteering) MessageAuthor(ctx context.Context, loopID, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("a message needs text")
	}
	l, err := OperatorReviewLoop(ctx, s.DB, loopID)
	if err != nil {
		return err
	}
	author := ReviewLoopAuthor(ctx, s.DB, l.HostTask)
	if author == "" {
		return fmt.Errorf("review loop %s: %w", loopID, ErrReviewAuthorNotAgent)
	}
	if _, err := s.DB.GetAgentSessionByTaskID(ctx, author); err != nil {
		return fmt.Errorf("the author's agent on %s is not running; start it, then message it", author)
	}
	if s.Agents == nil {
		return errors.New("no agents to message on this Legato")
	}
	return s.Agents.QueueMessage(ctx, author, reviewOperatorPrefix+text)
}

// stewardLoop is a run's stewarded loop the operator names, its run still
// running; ErrStewardNotWatching for a loop with no steward watching it (a
// handoff's, or one not yet approved).
func (s ReviewLoopSteering) stewardLoop(ctx context.Context, loopID string) (*store.ReviewLoop, error) {
	l, err := OperatorReviewLoop(ctx, s.DB, loopID)
	if err != nil {
		return nil, err
	}
	if l.OwnerKind != store.ReviewLoopOwnerRun || handoffBound(l) || !stewarding(l) {
		return nil, fmt.Errorf("review loop %s has no steward watching it: %w", loopID, ErrStewardNotWatching)
	}
	if !s.runRuns(ctx, l.RunID) {
		return nil, fmt.Errorf("review loop %s: its run no longer runs: %w", loopID, ErrReviewNotWaiting)
	}
	return l, nil
}

// runRuns says a run still runs, so its loop takes actions (a failed run
// keeps its loop for Retry).
func (s ReviewLoopSteering) runRuns(ctx context.Context, runID string) bool {
	view, err := s.DB.GetWorkflowRun(ctx, runID)
	return err == nil && (view.Run.Status == "running" || view.Run.Status == "waiting")
}

// StopWatching is Stop watching on a stewarded loop: its steward holds,
// sending nothing and starting no pass until Watch again. It only writes
// the record, as the run page's action does.
func (s ReviewLoopSteering) StopWatching(ctx context.Context, loopID string) error {
	if _, err := s.stewardLoop(ctx, loopID); err != nil {
		return err
	}
	return s.Engine.stopWatching(ctx, loopID)
}

// WatchAgain is Watch again on a held steward, or one more re-entry for a
// steward stuck at its watch. It only writes the record.
func (s ReviewLoopSteering) WatchAgain(ctx context.Context, loopID string) error {
	if _, err := s.stewardLoop(ctx, loopID); err != nil {
		return err
	}
	return s.Engine.watchAgain(ctx, loopID)
}

// End ends a run's loop as the run page's End does: a stewarded one with
// outcome ended, a stuck one a review-loop step opened with outcome stuck.
// It closes the record; the engine's tick finishes the steps waiting on the
// loop and stops its reviewer. A stuck loop of the older review step ends
// only from its run page, and a handoff's ends with its handoff.
func (s ReviewLoopSteering) End(ctx context.Context, loopID string) error {
	l, err := OperatorReviewLoop(ctx, s.DB, loopID)
	if err != nil {
		return err
	}
	if l.OwnerKind != store.ReviewLoopOwnerRun || handoffBound(l) {
		return fmt.Errorf("review loop %s is a handoff's, which ends with its handoff (Return or Recall): %w", loopID, ErrReviewNotStuck)
	}
	if !s.runRuns(ctx, l.RunID) {
		return fmt.Errorf("review loop %s: its run no longer runs: %w", loopID, ErrReviewNotWaiting)
	}
	outcome := ReviewOutcomeEnded
	if !stewarding(l) {
		loop, err := s.DB.GetHandoffReview(ctx, l.HostTask)
		if err != nil || loop == nil || loop.Phase != ReviewPhaseStuck {
			return fmt.Errorf("review loop %s: %w", loopID, ErrReviewNotStuck)
		}
		if !s.DB.ReviewLoopOpenedByStep(ctx, l.AttemptID) {
			return fmt.Errorf("review loop %s is the older review step's: end it from its run page or Attention", loopID)
		}
		outcome = ReviewOutcomeStuck
	}
	_, err = s.Engine.Close(ctx, loopID, outcome)
	return err
}
