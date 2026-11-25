package handofftest

import (
	"context"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// Every GitHub write of a review loop first checks that the loop is still
// open and still its owner's: the same workflow run attempt, or the same
// handoff (spec LEG-559 D11). A Cancel, a Recall or a Drop that lands while
// Legato is about to post stops what comes after it.

// readsBeforeAPost is how many pull request reads a tick makes for a run's
// submitted pass before its last check, the one just before the review is
// posted: the author's round (AuthorDone), then the check of every layer.
const readsBeforeAPost = 2

// submittedPass leaves a run's pass 1 submitted with a clean verdict and
// not yet posted, as when the posting was cut short; the next tick posts it.
func submittedPass(t *testing.T, r reviewRun, task string) {
	t.Helper()
	r.worker.HoldStarts(true)
	r.submit(t, task, service.ReviewSubmission{Summary: "Looks right."})
	r.worker.HoldStarts(false)
	pass, err := r.worker.Store.GetHandoffReviewPass(context.Background(), task, 1)
	if err != nil || pass.State != service.ReviewPassSubmitted {
		t.Fatalf("pass 1 = %+v, %v; want it submitted and not posted", pass, err)
	}
}

// waitFor polls until ok, for at most five seconds.
func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Errorf("timed out waiting for %s", what)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestACancelWhileAPassIsSubmittedPostsNoReviewAndTheRunEndsCancelled(t *testing.T) {
	for name, cancel := range map[string]func(t *testing.T, r reviewRun, view *store.WorkflowRunView) (wait func()){
		// A Cancel from a process without the engine (a TUI beside
		// serve) only writes the run: the holder's post finds its
		// attempt cancelled.
		"in a process without the engine": func(t *testing.T, r reviewRun, view *store.WorkflowRunView) func() {
			r.worker.ReviewGitHub.OnRead(0, func() {
				if _, err := otherProcess(r.worker).Cancel(context.Background(), view.Run.ID, "not needed"); err != nil {
					t.Errorf("cancel: %v", err)
				}
			})
			return func() {}
		},
		// A Cancel in the holder itself, while its tick holds the engine,
		// lands just before the review would be posted.
		"in the holder, mid-post": func(t *testing.T, r reviewRun, view *store.WorkflowRunView) func() {
			done := make(chan struct{})
			r.worker.ReviewGitHub.OnRead(readsBeforeAPost, func() {
				go func() {
					defer close(done)
					if _, err := r.runs.Workflows.Cancel(context.Background(), view.Run.ID, "not needed"); err != nil {
						t.Errorf("cancel: %v", err)
					}
				}()
				waitFor(t, "the cancel to close the loop", func() bool {
					loop, _ := r.worker.Store.GetHandoffReview(context.Background(), view.Run.TaskID)
					return loop != nil && loop.Phase == service.ReviewPhaseEnded
				})
			})
			return func() {
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("the cancel never returned")
				}
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newReviewRun(t, ReviewPassOptions{})
			view := r.start(t, pr12)
			task := view.Run.TaskID
			r.tick()
			submittedPass(t, r, task)

			wait := cancel(t, r, view)
			r.tick()
			wait()
			r.tick()

			if posted := r.worker.ReviewGitHub.Posted(); len(posted) != 0 {
				t.Fatalf("posted %+v after the cancel", posted)
			}
			if got := r.show(t, view.Run.ID); got.Run.Status != "cancelled" {
				t.Fatalf("run %q; want cancelled", got.Run.Status)
			}
			pass, err := r.worker.Store.GetHandoffReviewPass(context.Background(), task, 1)
			if err != nil || pass.State != service.ReviewPassDiscarded {
				t.Fatalf("pass 1 = %+v, %v; want it discarded", pass, err)
			}
		})
	}
}

// readsBeforeAHandoffPost is how many pull request reads a tick makes for a
// handoff's submitted pass before the check just before its review is
// posted: the check of every layer.
const readsBeforeAHandoffPost = 1

// A Recall or a Drop that lands while the holder is about to post a
// handoff's submitted pass: nothing posts then or after.
func TestEndingAHandoffWhileItsPassIsSubmittedPostsNothing(t *testing.T) {
	for name, end := range map[string]func(r reviewed) error{
		"recall": func(r reviewed) error {
			_, err := r.main.Handoffs.Recall(context.Background(), r.taskID)
			return err
		},
		"drop": func(r reviewed) error {
			h, err := r.worker.Store.OpenHandoffForTask(context.Background(), r.card)
			if err != nil {
				return err
			}
			_, err = r.worker.Handoffs.Drop(context.Background(), h.MainInstanceID, h.HandoffID)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newReviewed(t, ReviewPassOptions{})
			ctx := context.Background()
			r.finish(t)
			r.worker.HoldStarts(true)
			if _, err := r.submit(t, service.ReviewSubmission{Summary: "Good."}); err != nil {
				t.Fatal(err)
			}
			r.worker.HoldStarts(false)
			if pass, err := r.worker.Store.GetHandoffReviewPass(ctx, r.card, 1); err != nil || pass.State != service.ReviewPassSubmitted {
				t.Fatalf("pass 1 = %+v, %v; want it submitted and not posted", pass, err)
			}

			done := make(chan error, 1)
			r.worker.ReviewGitHub.OnRead(readsBeforeAHandoffPost, func() {
				go func() { done <- end(r) }()
				// Return marks the handoff returning, Drop ends the
				// loop: either is in the store before the engine is
				// free.
				waitFor(t, "the "+name+" to reach the store", func() bool {
					loop, _ := r.worker.Store.GetHandoffReview(ctx, r.card)
					open, _ := r.worker.Store.OpenHandoffForTask(ctx, r.card)
					return (loop != nil && loop.Phase == service.ReviewPhaseEnded) || open == nil || open.State == "returning"
				})
			})
			r.tick()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("the %s never returned", name)
			}
			r.tick()

			if posted := r.worker.ReviewGitHub.Posted(); len(posted) != 0 {
				t.Fatalf("posted %+v after the %s", posted, name)
			}
			pass, err := r.worker.Store.GetHandoffReviewPass(ctx, r.card, 1)
			if err != nil || pass.State != service.ReviewPassDiscarded {
				t.Fatalf("pass 1 = %+v, %v; want it discarded", pass, err)
			}
		})
	}
}

// An answer the author gave before a Cancel is not posted after it, though
// the tick that would post it had already found the loop.
func TestAnAnswerQueuedBeforeACancelIsNotPostedAfterIt(t *testing.T) {
	for name, cancel := range map[string]func(t *testing.T, r reviewRun, view *store.WorkflowRunView) (wait func()){
		"in a process without the engine": func(t *testing.T, r reviewRun, view *store.WorkflowRunView) func() {
			if _, err := otherProcess(r.worker).Cancel(context.Background(), view.Run.ID, "not needed"); err != nil {
				t.Errorf("cancel: %v", err)
			}
			return func() {}
		},
		"in the holder": func(t *testing.T, r reviewRun, view *store.WorkflowRunView) func() {
			done := make(chan struct{})
			go func() {
				defer close(done)
				if _, err := r.runs.Workflows.Cancel(context.Background(), view.Run.ID, "not needed"); err != nil {
					t.Errorf("cancel: %v", err)
				}
			}()
			waitFor(t, "the cancel to close the loop", func() bool {
				loop, _ := r.worker.Store.GetHandoffReview(context.Background(), view.Run.TaskID)
				return loop != nil && loop.Phase == service.ReviewPhaseEnded
			})
			return func() {
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("the cancel never returned")
				}
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newReviewRun(t, ReviewPassOptions{})
			withBoardAuthor(t, r)
			ctx := context.Background()
			view, finding := agentPassOne(t, r)
			if err := service.AnswerReviewItem(ctx, r.worker.Store, "board-1", finding, service.AnswerFixed, "guarded", r.worker.Clock.Now()); err != nil {
				t.Fatal(err)
			}

			// The tick reads what the author pushed, for the answer's
			// "Fixed in", just before it would post the answer.
			wait := func() {}
			r.worker.ReviewGitHub.OnRead(0, func() { wait = cancel(t, r, view) })
			r.tick()
			wait()
			r.tick()

			if replies, comments := r.worker.ReviewGitHub.Replies(), r.worker.ReviewGitHub.Comments(); len(replies) != 0 || len(comments) != 0 {
				t.Fatalf("replies %v, comments %v after the cancel", replies, comments)
			}
			if got := r.show(t, view.Run.ID); got.Run.Status != "cancelled" {
				t.Fatalf("run %q; want cancelled", got.Run.Status)
			}
		})
	}
}
