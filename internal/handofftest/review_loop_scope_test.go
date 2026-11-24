package handofftest

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// another is a second make-a-change handoff to r's worker, of repo, whose
// author has opened pull request number from its branch at head, in a
// checkout of its own.
func (r reviewed) another(t *testing.T, repo string, number int, head string) reviewed {
	t.Helper()
	ctx := context.Background()
	first, err := r.main.Handoffs.Away(ctx, r.taskID)
	if err != nil || first == nil {
		t.Fatalf("the first handoff: %+v, %v", first, err)
	}
	taskID, _ := localTask(t, r.main, "Fix the gadget", "", "")
	away, err := r.main.Handoffs.HandOff(ctx, service.TaskHandoffRequest{TaskID: taskID, Worker: first.WorkerInstanceID,
		Purpose: service.HandoffMakeChange, Repo: repo, AgentKind: "claude-code"})
	if err != nil {
		t.Fatal(err)
	}
	r.worker.Settle()
	spawned := r.worker.Tmux.Spawned()
	branch := "legato/" + strings.ToLower(taskID)
	r.worker.Checkout.SetFor(away.WorkerTaskID, spawned[len(spawned)-1].WorkDir, branch, head)
	r.worker.ReviewGitHub.OpenPR(number, branch, head)
	return reviewed{worker: r.worker, main: r.main, taskID: taskID, card: away.WorkerTaskID, branch: branch}
}

// The same pull request number and check name in two repositories: each
// loop sees its own failing check, and neither is approved and done with
// the other's.
func TestEachLoopSeesItsOwnCheckWhenTwoRepositoriesShareAPRNumber(t *testing.T) {
	widget := newReviewed(t, ReviewPassOptions{})
	widget.worker.ReviewGitHub.SetChecks("aaa111", buildFails)
	widget.finish(t)
	if _, err := widget.submit(t, service.ReviewSubmission{Summary: "Good."}); err != nil {
		t.Fatal(err)
	}
	widget.tick()
	if checks := widget.checkItems(t); len(checks) != 1 || checks[0].URL != buildFails.URL {
		t.Fatalf("widget's check items %+v", checks)
	}

	gadgetFails := buildFails
	gadgetFails.URL, gadgetFails.Summary = "https://github.com/acme/gadget/runs/99", "the gadget build broke"
	gadget := widget.another(t, "acme/gadget", 7, "ccc333")
	widget.worker.ReviewGitHub.SetChecks("ccc333", gadgetFails)
	gadget.finish(t)
	if _, err := gadget.submit(t, service.ReviewSubmission{Summary: "Good."}); err != nil {
		t.Fatal(err)
	}
	gadget.tick()

	if state := gadget.handoffState(t); state == "done" {
		t.Fatal("the gadget handoff is done with its check failing")
	}
	checks := gadget.checkItems(t)
	if len(checks) != 1 || checks[0].URL != gadgetFails.URL || checks[0].Repo != "acme/gadget" {
		t.Fatalf("gadget's check items %+v; want its own failing build", checks)
	}
	checks = widget.checkItems(t)
	if len(checks) != 1 || checks[0].URL != buildFails.URL || checks[0].Repo != "acme/widget" {
		t.Fatalf("widget's check items %+v; want its own failing build", checks)
	}
}
