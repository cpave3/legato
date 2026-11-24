package handofftest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
)

// openComments are the IDs of a card's comment items that are not closed.
func openComments(t *testing.T, w *Worker, card string) string {
	t.Helper()
	items, err := w.Store.ListHandoffReviewItems(context.Background(), card)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, item := range items {
		if item.Kind == service.ItemComment && !item.Closed {
			ids = append(ids, item.ID)
		}
	}
	return strings.Join(ids, ",")
}

func TestAPollThatGetsA5xxClosesNoItem(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.worker.ReviewGitHub.SetComments(7, ownersReview, botComment)
	r.tick()
	if got := openComments(t, r.worker, r.card); got != "c-r-1,c-i-501" {
		t.Fatalf("open comments %s", got)
	}

	// Both comments are gone on GitHub, but GitHub answers 502.
	r.worker.ReviewGitHub.SetComments(7)
	r.worker.ReviewGitHub.FailPolls("acme/widget", &service.GitHubError{Status: 502, Message: "Bad Gateway"})
	r.worker.Clock.Advance(2 * time.Minute)
	r.tick()
	if got := openComments(t, r.worker, r.card); got != "c-r-1,c-i-501" {
		t.Fatalf("after a 502: open comments %q; a failed poll closes nothing", got)
	}
	if typed := r.worker.typedTo("legato-" + r.card); strings.Contains(typed, "deleted on GitHub") {
		t.Fatalf("the author was told of a deletion GitHub never reported: %q", typed)
	}

	// A read that succeeds and lacks them closes them.
	r.worker.ReviewGitHub.FailPolls("acme/widget", nil)
	r.worker.Clock.Advance(2 * time.Minute)
	r.tick()
	if got := openComments(t, r.worker, r.card); got != "" {
		t.Fatalf("after a successful read: open comments %q", got)
	}
}

func TestAGraphQLRateLimitLeavesItemsOpenAndThePollWaitsForTheReset(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.worker.ReviewGitHub.SetComments(7, ownersReview, botComment)
	r.tick()

	reset := r.worker.Clock.Now().Add(10 * time.Minute)
	r.worker.ReviewGitHub.SetComments(7)
	r.worker.ReviewGitHub.FailPolls("acme/widget", &service.GitHubRateLimit{Installation: "github-reviewer@acme", Status: 200,
		Message: "API rate limit exceeded for installation ID 7.", Reset: reset, RetryAt: reset})
	r.worker.Clock.Advance(2 * time.Minute)
	r.tick()
	if got := openComments(t, r.worker, r.card); got != "c-r-1,c-i-501" {
		t.Fatalf("after a rate limit: open comments %q", got)
	}

	// GitHub would answer now, but the next poll comes after the reset.
	r.worker.ReviewGitHub.FailPolls("acme/widget", nil)
	polls := r.worker.ReviewGitHub.PollsOf("acme/widget")
	r.worker.Clock.Advance(6 * time.Minute)
	r.tick()
	if r.worker.ReviewGitHub.PollsOf("acme/widget") != polls {
		t.Fatal("polled before the reset")
	}
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	if r.worker.ReviewGitHub.PollsOf("acme/widget") != polls+1 {
		t.Fatal("not polled after the reset")
	}
	if got := openComments(t, r.worker, r.card); got != "" {
		t.Fatalf("after the reset: open comments %q", got)
	}
}

func TestALimitOnOneInstallationDoesNotDelayALoopOnAnother(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1", GitHubRepositories: []string{"acme/*", "zeta/*"}, ReviewPasses: &ReviewPassOptions{}})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	cards := map[string]string{}
	for i, repo := range []string{"acme/widget", "zeta/gadget"} {
		taskID, _ := localTask(t, main, "Fix "+repo, "", "")
		away, err := main.Handoffs.HandOff(ctx, service.TaskHandoffRequest{TaskID: taskID, Worker: paired.InstanceID,
			Purpose: service.HandoffMakeChange, Repo: repo, AgentKind: "claude-code"})
		if err != nil {
			t.Fatal(err)
		}
		worker.Settle()
		worker.ReviewGitHub.OpenPR(7+i*14, "legato/"+strings.ToLower(taskID), "aaa111")
		cards[repo] = away.WorkerTaskID
	}
	tick := func() { worker.Reviews.Tick(ctx); worker.Settle() }
	worker.ReviewGitHub.SetComments(7, ownersReview)
	worker.ReviewGitHub.SetComments(21, botComment)
	tick()
	if openComments(t, worker, cards["acme/widget"]) != "c-r-1" || openComments(t, worker, cards["zeta/gadget"]) != "c-i-501" {
		t.Fatalf("open comments: acme %q, zeta %q", openComments(t, worker, cards["acme/widget"]), openComments(t, worker, cards["zeta/gadget"]))
	}

	// zeta's installation is limited for an hour; acme's carries on.
	reset := worker.Clock.Now().Add(time.Hour)
	worker.ReviewGitHub.FailPolls("zeta/gadget", &service.GitHubRateLimit{Installation: "github-reviewer@zeta", Status: 403, Reset: reset, RetryAt: reset})
	members := service.PRComment{Kind: "issue", ID: 2001, Author: "amy", Association: "MEMBER", Body: "And the docs?", URL: "i2001"}
	worker.ReviewGitHub.SetComments(7, ownersReview, strangers, members)
	worker.ReviewGitHub.SetComments(21)
	worker.Clock.Advance(2 * time.Minute)
	tick()
	acmePolls := worker.ReviewGitHub.PollsOf("acme/widget")
	worker.Clock.Advance(2 * time.Minute)
	tick()
	if worker.ReviewGitHub.PollsOf("acme/widget") != acmePolls+1 {
		t.Fatal("acme's loop waited for zeta's limit")
	}
	if got := openComments(t, worker, cards["acme/widget"]); got != "c-r-1,c-c-2001" {
		t.Fatalf("acme's open comments %q; want its new one", got)
	}
	if got := openComments(t, worker, cards["zeta/gadget"]); got != "c-i-501" {
		t.Fatalf("zeta's open comments %q; a limited poll closes nothing", got)
	}
}
