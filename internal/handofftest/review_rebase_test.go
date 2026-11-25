package handofftest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
)

// The author rebases onto a main that changed other files: the diff since
// pass 1 covers main's lines too, but the pull request's own diff does
// not. A new blocking finding on main's lines is refused unless critical.
func TestAfterARebaseANewBlockingFindingOnAnUpstreamLineIsRefusedUnlessCritical(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	finding := changesPass(t, r, "The loop has no bound.")
	r.answer(t, finding, service.AnswerFixed, "bounded the retries")
	r.worker.Checkout.File("session.go", 60)
	r.worker.Checkout.Commit(r.branch, "bbb222")
	r.worker.Checkout.Change("bbb222", "login.go", 18, 24)   // the fix
	r.worker.Checkout.Change("bbb222", "session.go", 1, 60)  // main's, by the rebase
	r.worker.ReviewGitHub.Diff("bbb222", "login.go", 10, 30) // the pull request's own
	r.worker.ReviewGitHub.OpenPR(7, r.branch, "bbb222")
	r.tick()
	r.finish(t)
	resolved := []service.SubmittedRuling{{Finding: finding, Status: "resolved", Reason: "the retries are bounded"}}

	spawns := r.worker.Tmux.Spawned()
	if brief := briefOf(t, spawns[len(spawns)-1]); !strings.Contains(brief, "the pull request's own diff") {
		t.Errorf("pass 2's brief does not say a new blocking finding must be in the pull request's own diff:\n%s", brief)
	}

	upstream := service.SubmittedFinding{PR: 7, Severity: "blocking", Title: "Sessions never expire", Body: "x",
		FailureScenario: "y", Place: &service.ReviewPlace{Path: "session.go", Line: 12}}
	_, err := r.submit(t, service.ReviewSubmission{Summary: "s", Findings: []service.SubmittedFinding{upstream}, Rulings: resolved})
	var refused *service.ReviewSubmissionError
	if !errors.As(err, &refused) || len(refused.Problems) != 1 || !strings.Contains(refused.Problems[0], "own diff") ||
		!strings.Contains(refused.Problems[0], "critical") {
		t.Fatalf("err = %v", err)
	}

	upstream.Critical = "Every session outlives its logout, so the next person at the machine is signed in."
	result, err := r.submit(t, service.ReviewSubmission{Summary: "s", Findings: []service.SubmittedFinding{upstream}, Rulings: resolved})
	if err != nil || result.Verdict != service.VerdictChanges {
		t.Fatalf("submit = %+v, %v", result, err)
	}
}

// A pass the reviewer could not do posted nothing: the next pass reviews
// what changed since the last pass that was posted, and its brief says so.
func TestALaterPassReviewsWhatChangedSinceTheLastPostedPass(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	finding := changesPass(t, r, "The loop has no bound.")
	r.answer(t, finding, service.AnswerFixed, "bounded the retries")
	r.worker.Checkout.Commit(r.branch, "bbb222")
	r.worker.Checkout.Change("bbb222", "login.go", 18, 24)
	r.worker.ReviewGitHub.OpenPR(7, r.branch, "bbb222")
	r.tick()
	r.finish(t)
	if _, err := r.submit(t, service.ReviewSubmission{Unable: "the build tool is missing"}); err != nil {
		t.Fatal(err)
	}
	if err := r.worker.Reviews.AnotherPass(context.Background(), r.card); err != nil {
		t.Fatal(err)
	}
	r.tick()
	if loop := r.loop(t); loop.Pass != 3 || loop.Phase != service.ReviewPhaseReviewing {
		t.Fatalf("loop = %+v", loop)
	}
	spawns := r.worker.Tmux.Spawned()
	brief := briefOf(t, spawns[len(spawns)-1])
	if !strings.Contains(brief, "what changed since pass 1") || !strings.Contains(brief, "/1/7") || strings.Contains(brief, "since pass 2") {
		t.Errorf("pass 3's brief does not compare with pass 1:\n%s", brief)
	}

	onTheFix := service.SubmittedFinding{PR: 7, Severity: "blocking", Title: "The bound is off by one", Body: "x",
		FailureScenario: "y", Place: &service.ReviewPlace{Path: "login.go", Line: 20}}
	result, err := r.submit(t, service.ReviewSubmission{Summary: "s", Findings: []service.SubmittedFinding{onTheFix},
		Rulings: []service.SubmittedRuling{{Finding: finding, Status: "resolved", Reason: "the retries are bounded"}}})
	if err != nil || result.Verdict != service.VerdictChanges {
		t.Fatalf("submit = %+v, %v", result, err)
	}
}

// A pass whose head moved is discarded and pinned again: what changed is
// still measured from the last posted pass, never from the discarded pin.
func TestADiscardedPassDoesNotMoveTheBaseline(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	finding := changesPass(t, r, "The loop has no bound.")
	r.answer(t, finding, service.AnswerFixed, "bounded the retries")
	r.worker.Checkout.Commit(r.branch, "bbb222")
	r.worker.ReviewGitHub.OpenPR(7, r.branch, "bbb222")
	r.tick()
	r.finish(t)
	resolved := []service.SubmittedRuling{{Finding: finding, Status: "resolved", Reason: "the retries are bounded"}}

	// The author pushes again while pass 2 runs at bbb222.
	r.worker.Checkout.Commit(r.branch, "ccc333")
	r.worker.Checkout.ChangeSince("aaa111", "ccc333", "login.go", 18, 32)
	r.worker.Checkout.ChangeSince("bbb222", "ccc333", "login.go", 40, 40)
	r.worker.ReviewGitHub.Push(7, "ccc333")
	if _, err := r.submit(t, service.ReviewSubmission{Summary: "s", Rulings: resolved}); err != nil {
		t.Fatal(err)
	}
	r.tick()
	if loop := r.loop(t); loop.Pass != 2 || loop.Phase != service.ReviewPhaseReviewing || r.worker.Checkout.Pins()["refs/legato/review/"+r.card+"/2/7"] != "ccc333" {
		t.Fatalf("loop = %+v, pins %v", loop, r.worker.Checkout.Pins())
	}

	onTheFix := service.SubmittedFinding{PR: 7, Severity: "blocking", Title: "The bound is off by one", Body: "x",
		FailureScenario: "y", Place: &service.ReviewPlace{Path: "login.go", Line: 30}}
	result, err := r.submit(t, service.ReviewSubmission{Summary: "s", Findings: []service.SubmittedFinding{onTheFix}, Rulings: resolved})
	if err != nil || result.Verdict != service.VerdictChanges {
		t.Fatalf("submit = %+v, %v", result, err)
	}
}

// A later pass's blocking finding whose place is no place at the head (a
// file the head does not have, a span of a trillion lines) is refused for
// that, promptly: its span is never walked line by line under the
// engine's lock, and it is not held to the diff as well.
func TestALaterPassFindingOnAnEnormousSpanOfNoFileIsRefusedPromptly(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	finding := changesPass(t, r, "The loop has no bound.")
	r.answer(t, finding, service.AnswerFixed, "bounded the retries")
	r.worker.Checkout.Commit(r.branch, "bbb222")
	r.worker.Checkout.Change("bbb222", "login.go", 18, 24)
	r.worker.ReviewGitHub.Diff("bbb222", "login.go", 10, 30)
	r.worker.ReviewGitHub.OpenPR(7, r.branch, "bbb222")
	r.tick()
	r.finish(t)

	nowhere := service.SubmittedFinding{PR: 7, Severity: "blocking", Title: "Everything is wrong", Body: "x",
		FailureScenario: "y", Place: &service.ReviewPlace{Path: "missing.go", StartLine: 1, Line: 1_000_000_000_000}}
	type submitted struct{ err error }
	done := make(chan submitted, 1)
	go func() {
		_, err := r.submit(t, service.ReviewSubmission{Summary: "s", Findings: []service.SubmittedFinding{nowhere},
			Rulings: []service.SubmittedRuling{{Finding: finding, Status: "resolved", Reason: "the retries are bounded"}}})
		done <- submitted{err}
	}()
	var err error
	select {
	case got := <-done:
		err = got.err
	case <-time.After(10 * time.Second):
		panic("submitting a finding on an enormous span of a missing file did not return in 10s")
	}
	var refused *service.ReviewSubmissionError
	if !errors.As(err, &refused) || len(refused.Problems) != 1 || !strings.Contains(refused.Problems[0], "does not exist") {
		t.Fatalf("err = %v", err)
	}
}
