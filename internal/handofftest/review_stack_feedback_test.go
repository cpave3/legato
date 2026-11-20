package handofftest

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

func TestFeedbackOnAStackListsItsPullRequestsAndHowToFixThem(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	branches := r.threeLayers(t)
	r.finish(t)
	if _, err := r.submit(t, service.ReviewSubmission{Summary: "A bug in the middle.", Findings: []service.SubmittedFinding{
		{PR: 138, Severity: "blocking", Title: "Retries never stop", Body: "No bound.", FailureScenario: "A login while the server is down never returns."},
	}}); err != nil {
		t.Fatal(err)
	}
	r.tick()
	ctx := context.Background()

	view, err := service.ReviewFeedback(ctx, r.worker.Store, r.card)
	if err != nil || len(view.PRs) != 3 {
		t.Fatalf("view %+v, %v", view, err)
	}
	for i, pr := range view.PRs {
		if pr.Number != 137+i || pr.Branch != branches[i] {
			t.Fatalf("pull request %d: %+v", i, pr)
		}
	}
	if view.PRs[1].Base != branches[0] || view.Items[0].PR != 138 {
		t.Fatalf("view %+v", view)
	}
	for _, want := range []string{"own pull request's branch", "bottom up", "git rebase --update-refs", "--force-with-lease", "move the task to Done"} {
		if !strings.Contains(view.How, want) {
			t.Fatalf("how does not say %q: %s", want, view.How)
		}
	}
	if strings.Contains(view.How, service.StackFeedbackSkill) {
		t.Fatal("the skill is named though the author does not have it")
	}

	view, _ = service.ReviewFeedback(ctx, r.worker.Store, r.card, service.ReviewFeedbackOptions{HasSkill: func(name string) bool { return name == service.StackFeedbackSkill }})
	if !strings.Contains(view.How, "The address-stack-feedback skill") {
		t.Fatalf("how: %s", view.How)
	}
	// The line typed to the author carries nothing of the finding.
	if typed := r.worker.typedTo("legato-" + r.card); strings.Contains(typed, "Retries never stop") || strings.Contains(typed, "No bound") {
		t.Fatalf("typed to the author: %q", typed)
	}
}

func TestFeedbackOnOnePullRequestIsUnchanged(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	changesPass(t, r, "The loop has no bound.")

	view, err := service.ReviewFeedback(context.Background(), r.worker.Store, r.card, service.ReviewFeedbackOptions{HasSkill: func(string) bool { return true }})
	if err != nil || view.PRs != nil || strings.Contains(view.How, "stack") {
		t.Fatalf("view %+v, %v", view, err)
	}
}
