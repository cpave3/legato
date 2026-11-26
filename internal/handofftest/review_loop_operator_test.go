package handofftest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// loopID is the ID of the loop a task hosts.
func loopID(t *testing.T, w *Worker, hostTask string) string {
	t.Helper()
	l, err := w.Store.CurrentReviewLoop(context.Background(), hostTask)
	if err != nil || l == nil {
		t.Fatalf("no loop on %s: %v", hostTask, err)
	}
	return l.ID
}

// On the host, outside any session, the operator reads a loop's feedback
// and answers it by the loop's ID, with no task (story 24).
func TestTheOperatorReadsAndAnswersALoopByItsID(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	ctx := context.Background()
	view, finding := agentPassOne(t, r)
	operator := service.ReviewAnswerer{Operator: true, LoopID: loopID(t, r.worker, view.Run.TaskID)}

	feedback, err := service.OwnReviewFeedback(ctx, r.worker.Store, operator)
	if err != nil || feedback.TaskID != view.Run.TaskID || len(feedback.Items) != 1 || feedback.Items[0].ID != finding {
		t.Fatalf("feedback = %+v, %v", feedback, err)
	}
	loopTask, err := service.AnswerOwnReviewItem(ctx, r.worker.Store, operator, finding, service.AnswerReplied, "An empty name never reaches greet.", r.worker.Clock.Now())
	if err != nil || loopTask != view.Run.TaskID {
		t.Fatalf("answer = %q, %v", loopTask, err)
	}
	r.tick()
	if replies := r.worker.ReviewGitHub.Replies(); len(replies) != 1 || !strings.Contains(replies[0], "An empty name never reaches greet.") {
		t.Fatalf("replies = %v", replies)
	}
}

// A loop ID that names no loop, or a closed one, is refused.
func TestTheOperatorCannotActOnAnUnknownOrClosedLoop(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	if _, err := service.OwnReviewFeedback(ctx, r.worker.Store, service.ReviewAnswerer{Operator: true, LoopID: "rl-000000000000"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown loop: %v", err)
	}

	view := r.startLoop(t, pr12)
	id := loopID(t, r.worker, view.Run.TaskID)
	if _, err := r.runs.Workflows.Cancel(ctx, view.Run.ID, ""); err != nil {
		t.Fatal(err)
	}
	r.worker.Settle()
	if _, err := service.OwnReviewFeedback(ctx, r.worker.Store, service.ReviewAnswerer{Operator: true, LoopID: id}); !errors.Is(err, store.ErrReviewLoopClosed) {
		t.Fatalf("closed loop: %v", err)
	}
}

// hostSteering is the steering the CLI builds on the host: an engine of its
// own on the worker's store, which never ticks, beside the worker's.
func hostSteering(w *Worker, opts ReviewPassOptions) service.ReviewLoopSteering {
	engine := service.NewReviewPassService(w.Store, service.ReviewPassOptions{Config: opts.config, Now: w.Clock.Now})
	return service.ReviewLoopSteering{DB: w.Store, Engine: engine, Agents: w.Agents.(service.ReviewLoopMessenger)}
}

// Another pass by loop ID runs a stuck run loop's next pass, past its limit.
func TestTheOperatorRunsAnotherPassOfAStuckRunLoopByItsID(t *testing.T) {
	r, view := stuckRun(t)
	if phase := r.loopPhase(t, view.Run.TaskID).Phase; phase != service.ReviewPhaseStuck {
		t.Fatalf("phase %q", phase)
	}
	if err := hostSteering(r.worker, ReviewPassOptions{MaxPasses: 1}).AnotherPass(context.Background(), loopID(t, r.worker, view.Run.TaskID)); err != nil {
		t.Fatal(err)
	}
	r.tick()
	if loop := r.loopPhase(t, view.Run.TaskID); loop.Phase != service.ReviewPhaseReviewing || loop.Pass != 2 || !r.reviewer(t, view.Run.TaskID) {
		t.Fatalf("after another pass: %+v", loop)
	}
}

// Another pass by loop ID starts the next pass of a run loop that waits for
// its author, as Run another pass does on the run page.
func TestTheOperatorStartsTheNextPassOfAWaitingRunLoopByItsID(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view, _ := agentPassOne(t, r)
	if phase := r.loopPhase(t, view.Run.TaskID).Phase; phase != service.ReviewPhaseWaitingForAuthor {
		t.Fatalf("phase %q", phase)
	}
	if err := hostSteering(r.worker, ReviewPassOptions{}).AnotherPass(context.Background(), loopID(t, r.worker, view.Run.TaskID)); err != nil {
		t.Fatal(err)
	}
	r.tick()
	if loop := r.loopPhase(t, view.Run.TaskID); loop.Phase != service.ReviewPhaseReviewing || loop.Pass != 2 {
		t.Fatalf("after another pass: %+v", loop)
	}
}

// A handoff's stuck loop takes another pass by its ID on the worker's host;
// one that is reviewing is refused.
func TestTheOperatorRunsAnotherPassOfAStuckHandoffLoopByItsID(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	ctx := context.Background()
	r.finish(t)
	steering := hostSteering(r.worker, ReviewPassOptions{})
	id := loopID(t, r.worker, r.card)
	if err := steering.AnotherPass(ctx, id); !errors.Is(err, service.ErrReviewNotStuck) {
		t.Fatalf("another pass while reviewing: %v", err)
	}
	r.worker.Clock.Advance(46 * time.Minute)
	r.tick()
	if phase := r.loop(t).Phase; phase != service.ReviewPhaseStuck {
		t.Fatalf("phase %q", phase)
	}
	if err := steering.AnotherPass(ctx, id); err != nil {
		t.Fatal(err)
	}
	r.tick()
	if loop := r.loop(t); loop.Phase != service.ReviewPhaseReviewing || loop.Pass != 2 {
		t.Fatalf("after another pass: %+v", loop)
	}
}

// Message the author by loop ID reaches the author's agent, tagged; a loop
// whose author is a person has no agent to tell.
func TestTheOperatorMessagesALoopsAuthorByItsID(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	ctx := context.Background()
	view, _ := agentPassOne(t, r)
	steering := hostSteering(r.worker, ReviewPassOptions{})
	if err := steering.MessageAuthor(ctx, loopID(t, r.worker, view.Run.TaskID), "Rebase on main first."); err != nil {
		t.Fatal(err)
	}
	if typed := r.worker.typedTo("legato-board-1"); !strings.Contains(typed, "[legato review pass] From the operator: Rebase on main first.") {
		t.Fatalf("typed to the author: %q", typed)
	}

	r.worker.ReviewGitHub.OpenPR(13, "feature/other", "ddd444")
	person := r.startLoop(t, map[string]string{"repo": "acme/widget", "pr_number": "13"})
	if person.Run.Status != "running" {
		t.Fatalf("run %q: %s", person.Run.Status, person.Run.Error)
	}
	if err := steering.MessageAuthor(ctx, loopID(t, r.worker, person.Run.TaskID), "Hello"); !errors.Is(err, service.ErrReviewAuthorNotAgent) {
		t.Fatalf("message to a person: %v", err)
	}
}
