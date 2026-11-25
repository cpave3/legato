package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// ErrReviewAnswerRefused is a review-pass command from someone who may not
// read or answer the loop: another task's session, a reviewer slot, or a
// loop whose author is a person (B4, LEG-571).
var ErrReviewAnswerRefused = errors.New("refused")

// ErrReviewAuthorNotAgent is an action that needs an agent author on a loop
// whose author is the pull request's own, not an agent: it answers on
// GitHub, and Legato neither takes its answers nor messages it.
var ErrReviewAuthorNotAgent = errors.New("the pull request's author is not an agent, and answers on GitHub")

// ReviewAnswerer is who a `legato review-pass` command comes from. Inside an
// agent session it is the session, as the host resolved it (the broker
// token's execution, never a task the agent named). The operator on the host
// names a loop by its ID (LoopID, --loop) or a task (--task), and has no slot
// to check.
type ReviewAnswerer struct {
	TaskID      string
	ExecutionID string
	// Operator is the operator on the host, outside any session.
	Operator bool
	// LoopID is the loop the operator names (rl-…); it wins over TaskID.
	LoopID string
}

// OperatorReviewLoop is the open loop the operator names by its ID:
// store.ErrNotFound when there is no such loop, store.ErrReviewLoopClosed
// when it is closed. The task-keyed readers and actions find it again on its
// host task, which hosts one open loop at a time.
func OperatorReviewLoop(ctx context.Context, db *store.Store, loopID string) (*store.ReviewLoop, error) {
	l, err := db.GetReviewLoop(ctx, loopID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("no review loop %s: %w", loopID, store.ErrNotFound)
		}
		return nil, err
	}
	if l.State != store.ReviewLoopOpenState {
		return nil, fmt.Errorf("review loop %s is closed (%s): %w", loopID, l.Outcome, store.ErrReviewLoopClosed)
	}
	if current, err := db.CurrentReviewLoop(ctx, l.HostTask); err != nil || current == nil || current.ID != l.ID {
		return nil, fmt.Errorf("review loop %s is not the loop its task %s hosts now: %w", loopID, l.HostTask, store.ErrReviewLoopClosed)
	}
	return l, nil
}

// AnswererReviewLoop is the task that hosts the loop record of the review
// loop the answerer reads and answers. A session finds the loop its task's agent
// authors: its card's (a handoff) or the newest open run loop that names its
// task as author_task. Only the author task's main slot is let through; a
// named slot (the reviewer), another task, and a loop whose author is a
// person are refused with ErrReviewAnswerRefused.
func AnswererReviewLoop(ctx context.Context, db *store.Store, who ReviewAnswerer) (string, error) {
	if who.Operator && who.LoopID != "" {
		l, err := OperatorReviewLoop(ctx, db, who.LoopID)
		if err != nil {
			return "", err
		}
		return l.HostTask, nil
	}
	if who.TaskID == "" {
		return "", fmt.Errorf("%w: no session and no task to find a review loop for", ErrReviewAnswerRefused)
	}
	if !who.Operator {
		session, err := db.GetRunningAgentSession(ctx, who.TaskID, who.ExecutionID)
		if err != nil {
			return "", fmt.Errorf("%w: task %s has no running agent session to answer from", ErrReviewAnswerRefused, who.TaskID)
		}
		// An execution that is not the session's is another slot's, or a
		// session's whose link is not made yet: neither is the author's.
		if who.ExecutionID != "" && session.ExecutionID != who.ExecutionID {
			return "", fmt.Errorf("%w: this session is not task %s's main agent; only the author's own agent answers its review", ErrReviewAnswerRefused, who.TaskID)
		}
		if slot := session.Slot; slot != "" && slot != store.SlotMain {
			return "", fmt.Errorf("%w: the %s slot of task %s does not answer review feedback; only the author's own agent does", ErrReviewAnswerRefused, slot, who.TaskID)
		}
	}
	loopTask := reviewLoopTask(ctx, db, who.TaskID)
	loop, err := db.GetHandoffReview(ctx, loopTask)
	if err != nil {
		return "", err
	}
	if loop == nil {
		return "", fmt.Errorf("%s has no review pass", who.TaskID)
	}
	if who.Operator {
		return loopTask, nil
	}
	author := ReviewLoopAuthor(ctx, db, loopTask)
	if author == "" {
		return "", fmt.Errorf("%w: the review loop on %s: %w", ErrReviewAnswerRefused, loopTask, ErrReviewAuthorNotAgent)
	}
	if author != who.TaskID {
		return "", fmt.Errorf("%w: the review loop on %s is answered by its author, task %s, not by task %s", ErrReviewAnswerRefused, loopTask, author, who.TaskID)
	}
	return loopTask, nil
}

// ReviewLoopAuthor is the task whose agent authors the loop on loopTask: a
// run's author_task ("" when the author is a person), else the handoff's own
// card (a loop the handoff owns, or its review-loop run's).
func ReviewLoopAuthor(ctx context.Context, db *store.Store, loopTask string) string {
	if run, err := readReviewRun(ctx, db, loopTask); err == nil && !run.handoff {
		return run.AuthorTask
	}
	return loopTask
}

// AnswerOwnReviewItem records an answer for whoever may give it: the loop
// is the answerer's (AnswererReviewLoop), and a loop whose author is a
// person takes no answers from the command, even the operator's.
func AnswerOwnReviewItem(ctx context.Context, db *store.Store, who ReviewAnswerer, id, answer, note string, now time.Time) (string, error) {
	loopTask, err := AnswererReviewLoop(ctx, db, who)
	if err != nil {
		return "", err
	}
	if ReviewLoopAuthor(ctx, db, loopTask) == "" {
		return "", fmt.Errorf("%w: the review loop on %s: %w", ErrReviewAnswerRefused, loopTask, ErrReviewAuthorNotAgent)
	}
	return loopTask, AnswerReviewItem(ctx, db, loopTask, id, answer, note, now)
}

// OwnReviewFeedback is the current round of the answerer's loop.
func OwnReviewFeedback(ctx context.Context, db *store.Store, who ReviewAnswerer, opts ...ReviewFeedbackOptions) (ReviewFeedbackView, error) {
	loopTask, err := AnswererReviewLoop(ctx, db, who)
	if err != nil {
		return ReviewFeedbackView{}, err
	}
	return ReviewFeedback(ctx, db, loopTask, opts...)
}
