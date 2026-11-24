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

// ReviewAnswerer is who a `legato review-pass` command comes from. Inside an
// agent session it is the session, as the host resolved it (the broker
// token's execution, never a task the agent named). The operator on the host
// names a task instead, and has no slot to check.
type ReviewAnswerer struct {
	TaskID      string
	ExecutionID string
	// Operator is the operator on the host, outside any session.
	Operator bool
}

// AnswererReviewLoop is the task whose ledger holds the review loop the
// answerer reads and answers. A session finds the loop its task's agent
// authors: its card's (a handoff) or the newest open run loop that names its
// task as author_task. Only the author task's main slot is let through; a
// named slot (the reviewer), another task, and a loop whose author is a
// person are refused with ErrReviewAnswerRefused.
func AnswererReviewLoop(ctx context.Context, db *store.Store, who ReviewAnswerer) (string, error) {
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
		return "", fmt.Errorf("%w: the author of %s's review loop is a person, who answers on GitHub", ErrReviewAnswerRefused, loopTask)
	}
	if author != who.TaskID {
		return "", fmt.Errorf("%w: the review loop on %s is answered by its author, task %s, not by task %s", ErrReviewAnswerRefused, loopTask, author, who.TaskID)
	}
	return loopTask, nil
}

// ReviewLoopAuthor is the task whose agent authors the loop on loopTask: a
// run's author_task ("" when the author is a person), else the handoff's own
// card.
func ReviewLoopAuthor(ctx context.Context, db *store.Store, loopTask string) string {
	if run, err := readReviewRun(ctx, db, loopTask); err == nil {
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
		return "", fmt.Errorf("%w: the author of %s's review loop is a person, who answers on GitHub", ErrReviewAnswerRefused, loopTask)
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
