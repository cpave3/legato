package handofftest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
)

func TestAStuckReviewRaisesAnItemWhoseAnotherPassRunsIt(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	attention := attentionOf(t, r.main)
	r.main.StartConnections()
	ctx := context.Background()
	r.finish(t)
	r.worker.Clock.Advance(46 * time.Minute)
	r.tick()

	item := attention.eventuallyItem(t, service.AttentionSignalHandoffReviewStuck)
	if !strings.Contains(item.Reason, "ran past") || item.TaskID != r.taskID {
		t.Fatalf("item %+v", item)
	}
	for _, id := range []string{"return", "another_pass", "message", "recall"} {
		action(t, item, id)
	}
	if !action(t, item, "message").RequiresText {
		t.Fatal("Message the author asks for no text")
	}

	if err := attention.Execute(ctx, item.ID, service.AttentionActionRequest{ActionID: "another_pass", ExpectedRevision: item.SourceRevision}); err != nil {
		t.Fatal(err)
	}
	r.tick()
	if loop := r.loop(t); loop.Phase != service.ReviewPhaseReviewing || loop.Pass != 2 {
		t.Fatalf("after Run another pass: %+v", loop)
	}
	if _, err := r.submit(t, service.ReviewSubmission{Summary: "Good."}); err != nil {
		t.Fatal(err)
	}
	if r.handoffState(t) != "done" {
		t.Fatalf("handoff %s", r.handoffState(t))
	}
	eventually(t, "the item goes", func() bool {
		for _, i := range attention.handoffItems(t) {
			if i.SignalKind == service.AttentionSignalHandoffReviewStuck {
				return false
			}
		}
		return true
	})
}

func TestTheMainCanMessageTheAuthor(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.main.StartConnections()
	r.finish(t)
	eventually(t, "in review", func() bool { return r.away(t).Review != nil })

	if err := r.main.Handoffs.MessageAuthor(context.Background(), r.taskID, "/clear the cache, then rebase on main"); err != nil {
		t.Fatal(err)
	}
	if typed := r.worker.typedTo("legato-" + r.card); !strings.Contains(typed, "[legato main] /clear the cache, then rebase on main") {
		t.Fatalf("typed to the author: %q", typed)
	}
}

func TestTheHandOffOverrideTurnsReviewsOffOrIsRefused(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1", GitHubRepositories: []string{"acme/*"}, ReviewPasses: &ReviewPassOptions{}})
	main := NewMain(t, MainOptions{Name: "workstation", Clock: worker.Clock})
	paired := Pair(t, main, worker)

	advert, err := main.Handoffs.WorkerReviewPasses(ctx, paired.InstanceID)
	if err != nil || advert == nil || advert.Defaults.Agent != "codex" || advert.Skills[0] != "3-tier-review" {
		t.Fatalf("advert %+v, %v", advert, err)
	}

	// A skill the worker does not have is refused before anything is made.
	taskID, _ := localTask(t, main, "Refused", "", "")
	_, err = main.Handoffs.HandOff(ctx, service.TaskHandoffRequest{TaskID: taskID, Worker: paired.InstanceID, Purpose: service.HandoffMakeChange,
		Repo: "acme/widget", AgentKind: "claude-code", Review: &service.ReviewPassOverride{Skill: "nitpick"}})
	if !errors.Is(err, service.ErrWorkerRefused) || !strings.Contains(err.Error(), `no skill "nitpick"`) {
		t.Fatalf("missing skill: %v", err)
	}

	// Off for this handoff: Done is done, with no pass.
	off := false
	taskID, _ = localTask(t, main, "No review", "", "")
	away, err := main.Handoffs.HandOff(ctx, service.TaskHandoffRequest{TaskID: taskID, Worker: paired.InstanceID, Purpose: service.HandoffMakeChange,
		Repo: "acme/widget", AgentKind: "claude-code", Review: &service.ReviewPassOverride{Enabled: &off}})
	if err != nil {
		t.Fatal(err)
	}
	worker.Settle()
	worker.Checkout.Set(worker.Tmux.Spawned()[0].WorkDir, "legato/"+strings.ToLower(taskID), "aaa111")
	worker.ReviewGitHub.OpenPR(7, "legato/"+strings.ToLower(taskID), "aaa111")
	if err := worker.Board.MoveCard(ctx, away.WorkerTaskID, "Done"); err != nil {
		t.Fatal(err)
	}
	worker.Reviews.Tick(ctx)
	worker.Settle()
	if err := worker.Handoffs.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	h, _ := worker.Store.OpenHandoffForTask(ctx, away.WorkerTaskID)
	if h.State != "done" || len(worker.ReviewGitHub.Posted()) != 0 {
		t.Fatalf("handoff %s, posted %d", h.State, len(worker.ReviewGitHub.Posted()))
	}
	if _, err := worker.Store.GetAgentSessionInSlot(ctx, away.WorkerTaskID, "reviewer"); err == nil {
		t.Fatal("a reviewer ran with review passes off")
	}
}

func TestAWorkerWithoutReviewPassesTakesNoOverride(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1", GitHubRepositories: []string{"acme/*"}})
	main := NewMain(t, MainOptions{Name: "workstation", Clock: worker.Clock})
	paired := Pair(t, main, worker)

	if advert, err := main.Handoffs.WorkerReviewPasses(ctx, paired.InstanceID); err != nil || advert != nil {
		t.Fatalf("advert %+v, %v; want none", advert, err)
	}
	taskID, _ := localTask(t, main, "Fix it", "", "")
	_, err := main.Handoffs.HandOff(ctx, service.TaskHandoffRequest{TaskID: taskID, Worker: paired.InstanceID, Purpose: service.HandoffMakeChange,
		Repo: "acme/widget", AgentKind: "claude-code", Review: &service.ReviewPassOverride{MaxPasses: 2}})
	if !errors.Is(err, service.ErrWorkerRefused) || !strings.Contains(err.Error(), "does not run review passes") {
		t.Fatalf("override to an old worker: %v", err)
	}

	// Turning them off asks for nothing it lacks: the handoff goes, with
	// no override.
	off := false
	if _, err := main.Handoffs.HandOff(ctx, service.TaskHandoffRequest{TaskID: taskID, Worker: paired.InstanceID, Purpose: service.HandoffMakeChange,
		Repo: "acme/widget", AgentKind: "claude-code", Review: &service.ReviewPassOverride{Enabled: &off}}); err != nil {
		t.Fatalf("review passes off to a worker without them: %v", err)
	}
}
