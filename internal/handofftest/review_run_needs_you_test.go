package handofftest

import (
	"context"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// LEG-543: a loop waiting for its author needs the operator only when the
// author cannot be counted on to move it: a person, or an agent author that
// waits on the operator, or that stopped with findings unanswered, or ended,
// for longer than the quiet time. An agent author at work is the agent's
// wait.

func (r reviewRun) details(t *testing.T, runID string) service.ReviewRunDetails {
	t.Helper()
	got, ok := r.show(t, runID).Details["review"].(service.ReviewRunDetails)
	if !ok {
		t.Fatalf("details = %+v", r.show(t, runID).Details)
	}
	return got
}

// authorActivity sets the author agent's activity as its hooks report it:
// "idle" for none, background with a job still running.
func (r reviewRun) authorActivity(t *testing.T, activity string) {
	t.Helper()
	ctx := context.Background()
	if activity == store.ActivityBackground {
		session, err := r.worker.Store.GetAgentSessionByTaskID(ctx, "board-1")
		if err != nil {
			t.Fatal(err)
		}
		if err := r.worker.Store.SetAgentHookState(ctx, session.ID, activity, store.BackgroundJobs{{Kind: "shell", ID: "b1", Label: "go test ./..."}}, ""); err != nil {
			t.Fatal(err)
		}
		return
	}
	if activity == "idle" {
		activity = ""
	}
	if err := r.worker.Agents.SetAgentActivity(ctx, "board-1", activity, ""); err != nil {
		t.Fatal(err)
	}
}

// reviewAttempt is the review step's latest attempt as the run view shows it.
func reviewAttempt(t *testing.T, view *store.WorkflowRunView) store.WorkflowStepAttempt {
	t.Helper()
	var latest *store.WorkflowStepAttempt
	for i := range view.Attempts {
		if view.Attempts[i].Path == "review" {
			latest = &view.Attempts[i]
		}
	}
	if latest == nil {
		t.Fatalf("no review attempt: %+v", view.Attempts)
	}
	return *latest
}

func TestAnAgentAuthorAtWorkOnTheFindingsDoesNotNeedTheOperator(t *testing.T) {
	for _, activity := range []string{store.ActivityWorking, store.ActivityBackground} {
		t.Run(activity, func(t *testing.T) {
			r := newReviewRun(t, ReviewPassOptions{})
			withBoardAuthor(t, r)
			view, _ := agentPassOne(t, r)
			r.authorActivity(t, activity)

			if items := r.attention(t); len(items) != 0 {
				t.Fatalf("items = %+v", items)
			}
			if d := r.details(t, view.Run.ID); d.NeedsYou || d.Summary != "Author fixing · 1 blocking" {
				t.Fatalf("details = %+v", d)
			}
			if attempt := reviewAttempt(t, r.show(t, view.Run.ID)); attempt.NeedsAttention {
				t.Fatalf("attempt = %+v", attempt)
			}
		})
	}
}

func TestAnAgentAuthorWaitingOnTheOperatorNeedsTheOperatorAtOnce(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view, _ := agentPassOne(t, r)
	r.authorActivity(t, store.ActivityWorking)
	r.authorActivity(t, store.ActivityWaiting)

	items := r.attention(t)
	if len(items) != 1 || items[0].SignalKind != service.AttentionSignalWorkflowReviewAuthor || items[0].Navigation.Target != view.Run.ID {
		t.Fatalf("items = %+v", items)
	}
	if d := r.details(t, view.Run.ID); !d.NeedsYou {
		t.Fatalf("details = %+v", d)
	}
}

func TestAnAgentAuthorThatStopsWithFindingsUnansweredNeedsTheOperatorAfterTheQuietTime(t *testing.T) {
	for _, activity := range []string{"idle", store.ActivityError} {
		t.Run(activity, func(t *testing.T) {
			r := newReviewRun(t, ReviewPassOptions{})
			withBoardAuthor(t, r)
			view, _ := agentPassOne(t, r)
			r.authorActivity(t, store.ActivityWorking)
			r.authorActivity(t, activity)

			// Between a turn and the next one, or a push and the next
			// pass, a stopped author is not yet the operator's.
			r.tick()
			if items := r.attention(t); len(items) != 0 {
				t.Fatalf("within the quiet time: items = %+v", items)
			}
			if d := r.details(t, view.Run.ID); d.NeedsYou {
				t.Fatalf("within the quiet time: details = %+v", d)
			}
			r.worker.Clock.Advance(3 * time.Minute)
			r.tick()

			items := r.attention(t)
			if len(items) != 1 || items[0].SignalKind != service.AttentionSignalWorkflowReviewAuthor || items[0].Navigation.Target != view.Run.ID {
				t.Fatalf("items = %+v", items)
			}
			if got := actionIDs(items[0]); got != "open,another_pass,message" {
				t.Fatalf("actions = %s", got)
			}
			if d := r.details(t, view.Run.ID); !d.NeedsYou || d.Summary != "Waiting for author · 1 blocking" {
				t.Fatalf("details = %+v", d)
			}
		})
	}
}

func TestAnIdleAgentAuthorWithEveryFindingAnsweredDoesNotNeedTheOperator(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view, finding := agentPassOne(t, r)
	if err := service.AnswerReviewItem(context.Background(), r.worker.Store, "board-1", finding, service.AnswerFixed, "Guarded.", r.worker.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	r.authorActivity(t, "idle")
	r.tick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	if items := r.attention(t); len(items) != 0 {
		t.Fatalf("items = %+v", items)
	}
	if d := r.details(t, view.Run.ID); d.NeedsYou {
		t.Fatalf("details = %+v", d)
	}
}

func TestAnAgentAuthorThatEndedNeedsTheOperator(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view, _ := agentPassOne(t, r)
	if err := r.worker.Agents.KillAgent(context.Background(), "board-1"); err != nil {
		t.Fatal(err)
	}
	r.tick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	items := r.attention(t)
	if len(items) != 1 || items[0].SignalKind != service.AttentionSignalWorkflowReviewAuthor {
		t.Fatalf("items = %+v", items)
	}
	if d := r.details(t, view.Run.ID); !d.NeedsYou {
		t.Fatalf("details = %+v", d)
	}
}

func TestALoopWaitingForAPersonAuthorNeedsTheOperator(t *testing.T) {
	r, view := afterPassOne(t, ReviewPassOptions{})
	items := r.attention(t)
	if len(items) != 1 || items[0].SignalKind != service.AttentionSignalWorkflowReviewAuthor || items[0].Navigation.Target != view.Run.ID {
		t.Fatalf("items = %+v", items)
	}
	if got := actionIDs(items[0]); got != "open,another_pass" {
		t.Fatalf("actions = %s", got)
	}
	if d := r.details(t, view.Run.ID); !d.NeedsYou || d.Summary != "Waiting for author · 1 blocking" {
		t.Fatalf("details = %+v", d)
	}
}

func actionIDs(item service.AttentionInput) string {
	var ids string
	for i, action := range item.Actions {
		if i > 0 {
			ids += ","
		}
		ids += action.ID
	}
	return ids
}
