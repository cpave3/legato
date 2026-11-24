package handofftest

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// An answer GitHub took before Legato could mark it posted (Legato crashed,
// or GitHub's answer never came back) is found by its marker after the
// restart: GitHub has one reply, and the answer is marked posted (AC 18).
func TestAnAnswerIsPostedOnceAcrossACrashBetweenItsPostAndItsMark(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	finding := changesPass(t, r, "The loop has no bound.")
	view, err := service.ReviewFeedback(context.Background(), r.worker.Store, r.card)
	if err != nil || len(view.Items) != 2 {
		t.Fatalf("feedback = %+v, %v", view, err)
	}
	nit := view.Items[1].ID
	r.answer(t, finding, service.AnswerFixed, "bounded the retries")
	r.answer(t, nit, service.AnswerSkipped, "")

	// GitHub takes both answers, but Legato never hears back.
	r.worker.ReviewGitHub.LoseAnswers(2)
	r.tick()
	if replies, comments := r.worker.ReviewGitHub.Replies(), r.worker.ReviewGitHub.Comments(); len(replies) != 1 || len(comments) != 1 {
		t.Fatalf("replies %v, comments %v", replies, comments)
	}
	for _, id := range []string{finding, nit} {
		if item, _ := r.worker.Store.GetHandoffReviewItem(context.Background(), r.card, id); item == nil || item.Posted {
			t.Fatalf("%s marked posted with no answer from GitHub: %+v", id, item)
		}
	}

	r.worker.Restart()
	r.tick()
	r.tick()
	if replies, comments := r.worker.ReviewGitHub.Replies(), r.worker.ReviewGitHub.Comments(); len(replies) != 1 || len(comments) != 1 ||
		!strings.Contains(replies[0], "bounded the retries") {
		t.Fatalf("after the restart: replies %v, comments %v", replies, comments)
	}
	for _, id := range []string{finding, nit} {
		if item, _ := r.worker.Store.GetHandoffReviewItem(context.Background(), r.card, id); item == nil || !item.Posted {
			t.Fatalf("%s not marked posted after the restart: %+v", id, item)
		}
	}
}

// The same words answering two findings are two answers: the marker, not
// the text, says whether an answer is on GitHub.
func TestTwoIdenticalAnswersToTwoFindingsArePostedEach(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.finish(t)
	result, err := r.submit(t, service.ReviewSubmission{Summary: "Two bugs.", Findings: []service.SubmittedFinding{
		{PR: 7, Severity: "blocking", Title: "Retries never stop", Body: "b1", FailureScenario: "s1", Place: &service.ReviewPlace{Path: "login.go", Line: 20}},
		{PR: 7, Severity: "blocking", Title: "Logout never stops", Body: "b2", FailureScenario: "s2", Place: &service.ReviewPlace{Path: "login.go", Line: 30}},
	}})
	if err != nil || len(result.Findings) != 2 {
		t.Fatalf("submit = %+v, %v", result, err)
	}
	r.tick()
	for _, id := range result.Findings {
		r.answer(t, id, service.AnswerFixed, "")
	}
	r.tick()
	if replies := r.worker.ReviewGitHub.Replies(); len(replies) != 2 {
		t.Fatalf("replies %v", replies)
	}
}
