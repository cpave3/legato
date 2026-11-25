package handofftest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// --fixed names the commit the author pushed, not the head the reviewer's
// clone was reset to for the pass (S1, LEG-571's probe: before the fix, the
// reply read "Fixed in bbb222", the reviewed head).
func TestAFixedAnswerNamesTheAuthorsPushedTip(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	ctx := context.Background()
	_, finding := agentPassOne(t, r)

	if err := service.AnswerReviewItem(ctx, r.worker.Store, "board-1", finding, service.AnswerFixed, "Guarded.", r.worker.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	r.worker.ReviewGitHub.Push(12, "fff666")
	r.tick()
	replies := r.worker.ReviewGitHub.Replies()
	if len(replies) != 1 || !strings.Contains(replies[0], "Fixed in fff666: Guarded.") {
		t.Fatalf("replies = %v", replies)
	}
}

// session is the answerer a task's running agent in slot is, as the host
// resolves its broker token.
func session(t *testing.T, w *Worker, taskID, slot string) service.ReviewAnswerer {
	t.Helper()
	s, err := w.Store.GetAgentSessionInSlot(context.Background(), taskID, slot)
	if err != nil || s.ExecutionID == "" {
		t.Fatalf("no %s session on %s: %+v, %v", slot, taskID, s, err)
	}
	return service.ReviewAnswerer{TaskID: taskID, ExecutionID: s.ExecutionID}
}

func refusedAnswer(t *testing.T, err error, want string) {
	t.Helper()
	if !errors.Is(err, service.ErrReviewAnswerRefused) || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want a refusal naming %q", err, want)
	}
}

// The author's own session reads and answers its handoff's feedback with
// no task ID: the loop is found from the session (story 80).
func TestTheAuthorsSessionAnswersItsHandoffsFeedbackWithNoTaskID(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	finding := changesPass(t, r, "The loop has no bound.")
	ctx := context.Background()
	author := session(t, r.worker, r.card, store.SlotMain)

	view, err := service.OwnReviewFeedback(ctx, r.worker.Store, author)
	if err != nil || view.TaskID != r.card || len(view.Items) != 2 || view.Items[0].ID != finding {
		t.Fatalf("feedback = %+v, %v", view, err)
	}
	loopTask, err := service.AnswerOwnReviewItem(ctx, r.worker.Store, author, finding, service.AnswerFixed, "bounded", r.worker.Clock.Now())
	if err != nil || loopTask != r.card {
		t.Fatalf("answer = %q, %v", loopTask, err)
	}
	if item, _ := r.worker.Store.GetHandoffReviewItem(ctx, r.card, finding); item.Answer != service.AnswerFixed {
		t.Fatalf("item = %+v", item)
	}
}

// The reviewer runs on the card too, in its own slot: it is refused,
// while it reviews and after (AC 15).
func TestTheReviewerSlotDoesNotAnswerTheFeedback(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	ctx := context.Background()
	r.finish(t)
	reviewer := session(t, r.worker, r.card, service.SlotReviewer)
	if _, err := service.OwnReviewFeedback(ctx, r.worker.Store, reviewer); err == nil {
		t.Fatal("the reviewer read the author's feedback")
	}
	result, err := r.submit(t, service.ReviewSubmission{Summary: "One bug.", Findings: []service.SubmittedFinding{
		{PR: 7, Severity: "blocking", Title: "Retries never stop", Body: "x", FailureScenario: "y", Place: &service.ReviewPlace{Path: "login.go", Line: 20}}}})
	if err != nil {
		t.Fatal(err)
	}
	r.tick()
	_, err = service.AnswerOwnReviewItem(ctx, r.worker.Store, reviewer, result.Findings[0], service.AnswerReplied, "It is fine.", r.worker.Clock.Now())
	refusedAnswer(t, err, "author")
	if item, _ := r.worker.Store.GetHandoffReviewItem(ctx, r.card, result.Findings[0]); item.Answer != "" {
		t.Fatalf("the reviewer's answer was kept: %+v", item)
	}
}

// Another task's session cannot answer a loop it does not author, and a
// run's reviewer slot cannot answer the run's loop (AC 15).
func TestAnotherSessionOrARunsReviewerDoesNotAnswerARunsLoop(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	ctx := context.Background()
	if err := r.worker.Store.CreateTask(ctx, store.Task{ID: "board-2", Title: "Other", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := r.worker.Agents.SpawnAgent(ctx, "board-2", 0, 0, service.AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	r.worker.Settle()
	view := r.start(t, agentAuthored)
	r.tick()
	reviewer := session(t, r.worker, view.Run.TaskID, service.SlotReviewer)
	_, err := service.AnswerOwnReviewItem(ctx, r.worker.Store, reviewer, "f-any", service.AnswerFixed, "", r.worker.Clock.Now())
	refusedAnswer(t, err, "reviewer slot")
	result := r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{blockingFinding}})
	r.tick()
	finding := result.Findings[0]

	other := session(t, r.worker, "board-2", store.SlotMain)
	_, err = service.AnswerOwnReviewItem(ctx, r.worker.Store, other, finding, service.AnswerFixed, "", r.worker.Clock.Now())
	if err == nil {
		t.Fatal("another task's session answered the loop")
	}
	// The reviewer, ended with its pass, is refused all the same.
	_, err = service.AnswerOwnReviewItem(ctx, r.worker.Store, reviewer, finding, service.AnswerFixed, "", r.worker.Clock.Now())
	refusedAnswer(t, err, "")

	// The author's session finds the run's loop from its own task.
	author := session(t, r.worker, "board-1", store.SlotMain)
	feedback, err := service.OwnReviewFeedback(ctx, r.worker.Store, author)
	if err != nil || feedback.TaskID != view.Run.TaskID || len(feedback.Items) != 1 {
		t.Fatalf("feedback = %+v, %v", feedback, err)
	}
	if _, err := service.AnswerOwnReviewItem(ctx, r.worker.Store, author, finding, service.AnswerFixed, "", r.worker.Clock.Now()); err != nil {
		t.Fatal(err)
	}
}

// A loop whose author is a person takes no answers from the command, not
// even the operator's on the host: the person answers on GitHub.
func TestAPersonAuthoredLoopTakesNoCommandAnswers(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	view := r.start(t, pr12)
	r.tick()
	result := r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{blockingFinding}})
	r.tick()
	operator := service.ReviewAnswerer{TaskID: view.Run.TaskID, Operator: true}
	_, err := service.AnswerOwnReviewItem(ctx, r.worker.Store, operator, result.Findings[0], service.AnswerReplied, "Fine.", r.worker.Clock.Now())
	refusedAnswer(t, err, view.Run.TaskID)
	if !errors.Is(err, service.ErrReviewAuthorNotAgent) {
		t.Fatalf("err = %v, want %v", err, service.ErrReviewAuthorNotAgent)
	}
}
