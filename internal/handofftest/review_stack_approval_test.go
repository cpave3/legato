package handofftest

import (
	"context"
	"fmt"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// threeLayers makes the work a stack of three pull requests, #137 to #139,
// with HEAD at the top.
func (r reviewed) threeLayers(t *testing.T) []string {
	t.Helper()
	r.worker.ReviewGitHub.ClosePR(7)
	branches := []string{r.branch, r.branch + "-b", r.branch + "-c"}
	base := "main"
	for i, branch := range branches {
		r.worker.ReviewGitHub.OpenStackedPR(137+i, branch, base, fmt.Sprintf("sha%d", 137+i))
		r.worker.Checkout.Commit(branch, fmt.Sprintf("sha%d", 137+i))
		base = branch
	}
	r.worker.Checkout.File("login.go", 40)
	return branches
}

func postedEvents(posted []service.ReviewPost) []string {
	var out []string
	for _, p := range posted {
		out = append(out, fmt.Sprintf("#%d %s", p.Number, p.Event))
	}
	return out
}

func TestEachLayerOfAStackIsApprovedOnItsOwn(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	branches := r.threeLayers(t)
	r.finish(t)

	result, err := r.submit(t, service.ReviewSubmission{Summary: "The top layer has a bug.", Findings: []service.SubmittedFinding{
		{PR: 139, Severity: "blocking", Title: "Retries never stop", Body: "No bound.", FailureScenario: "A login while the server is down never returns.", Place: &service.ReviewPlace{Path: "login.go", Line: 20}},
	}})
	if err != nil || result.Verdict != service.VerdictChanges {
		t.Fatalf("submit = %+v, %v", result, err)
	}
	if got := fmt.Sprint(postedEvents(r.worker.ReviewGitHub.Posted())); got != "[#137 APPROVE #138 APPROVE #139 COMMENT]" {
		t.Fatalf("posted %s", got)
	}
	if r.loop(t).Phase != service.ReviewPhaseWaitingForAuthor || r.column(t) != "Doing" {
		t.Fatalf("phase %q, column %q", r.loop(t).Phase, r.column(t))
	}

	// The author fixes the top layer only, and is done again.
	r.tick()
	r.answer(t, result.Findings[0], service.AnswerFixed, "bounded")
	r.worker.Checkout.Commit(branches[2], "sha139b")
	r.worker.ReviewGitHub.OpenStackedPR(139, branches[2], branches[1], "sha139b")
	r.tick()
	r.finish(t)
	if loop := r.loop(t); loop.Pass != 2 || loop.Phase != service.ReviewPhaseReviewing {
		t.Fatalf("loop %+v", loop)
	}
	result, err = r.submit(t, service.ReviewSubmission{Summary: "Fixed.", Rulings: []service.SubmittedRuling{{Finding: result.Findings[0], Status: "resolved", Reason: "fixed"}}})
	if err != nil || result.Verdict != service.VerdictClean {
		t.Fatalf("pass 2 = %+v, %v", result, err)
	}
	// Only the top layer is approved again; the lower two keep theirs.
	if got := fmt.Sprint(postedEvents(r.worker.ReviewGitHub.Posted())); got != "[#137 APPROVE #138 APPROVE #139 COMMENT #139 APPROVE]" {
		t.Fatalf("posted %s", got)
	}
	if r.loop(t).Phase != service.ReviewPhaseClean || r.column(t) != "Done" {
		t.Fatalf("phase %q, column %q", r.loop(t).Phase, r.column(t))
	}
	block := r.worker.Reviews.ReviewBlock(context.Background(), r.card)
	if len(block.Links) != 3 || block.Links[0].PR != 137 || block.Links[0].URL != "https://github.com/review/1" || block.Links[2].URL != "https://github.com/review/4" {
		t.Fatalf("links %+v", block.Links)
	}
}

func TestALayerWithAFailingCheckIsNotApproved(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.threeLayers(t)
	r.worker.ReviewGitHub.SetChecks("sha138", service.CheckResult{Name: "go", State: service.CheckFailing, Summary: "tests failed"})
	r.finish(t)

	if _, err := r.submit(t, service.ReviewSubmission{Summary: "Looks right."}); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(postedEvents(r.worker.ReviewGitHub.Posted())); got != "[#137 APPROVE #138 COMMENT #139 APPROVE]" {
		t.Fatalf("posted %s", got)
	}
}
