package service

import (
	"context"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// prStageFixture is a card task LEG-1 on acme/widget's branch feature/x,
// its agent running as ex-1 and its stage started, with PR tracking and the
// stage milestones on one bus.
type prStageFixture struct {
	gh     *fakeGitHub
	store  *store.Store
	prs    *prTrackingService
	stages *WorkStageService
	place  WorkStagePlace
}

func newPRStageFixture(t *testing.T, prs ...fakePR) prStageFixture {
	t.Helper()
	gh := newFakeGitHub(t)
	gh.set("acme/widget", prs...)
	s, svc, bus := newBranchTracking(t, gh)
	createPRTask(t, s, "LEG-1")
	setRepoContext(t, s, "LEG-1", "acme/widget", "feature/x")
	pushingAgent(t, s, "LEG-1", "ex-1")
	stages := startMilestones(t, s, bus)
	place := TaskPlace("LEG-1", "")
	if err := stages.Start(context.Background(), place, "ex-1"); err != nil {
		t.Fatal(err)
	}
	return prStageFixture{gh: gh, store: s, prs: svc, stages: stages, place: place}
}

// noMilestone checks no milestone has moved the place's stage.
func (f prStageFixture) noMilestone(t *testing.T) {
	t.Helper()
	events, err := f.stages.History(context.Background(), f.place, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.Source == store.WorkStageSourceMilestone {
			t.Fatalf("a milestone moved the stage: %+v", e)
		}
	}
}

// LEG-519: an agent starts in a folder whose branch already has an open
// pull request. Discovery links it, but it opened before the agent ran, so
// the agent stays Exploring.
func TestAPullRequestAlreadyOpenWhenTrackingBeginsMovesNoStage(t *testing.T) {
	f := newPRStageFixture(t, fakePR{Number: 1965, Head: "feature/x", Created: "2026-01-01T00:00:00Z"})
	if err := f.prs.DiscoverPRs(context.Background(), "LEG-1"); err != nil {
		t.Fatal(err)
	}
	if got := prList(t, f.prs, "LEG-1"); len(got) != 1 || got[0] != "feature/x:#1965" {
		t.Fatalf("tracked = %v, want the card's PR linked", got)
	}
	stageStays(t, f.stages, f.place, store.WorkStageExploring)
	f.noMilestone(t)
}

// A pull request opened while the agent runs is the Reviewing edge.
func TestAPullRequestOpenedAfterTheAgentStartedIsReviewing(t *testing.T) {
	f := newPRStageFixture(t)
	f.gh.set("acme/widget", fakePR{Number: 20, Head: "feature/x", Created: time.Now().UTC().Add(time.Minute).Format(time.RFC3339)})
	if err := f.prs.DiscoverPRs(context.Background(), "LEG-1"); err != nil {
		t.Fatal(err)
	}
	if got := stageSoon(t, f.stages, f.place, store.WorkStageReviewing); got.Reason != "PR #20 open" {
		t.Fatalf("stage = %+v", got)
	}
}

// A draft that was open before the agent started and is marked ready while
// it runs is the Reviewing edge.
func TestADraftMarkedReadyWhileTheAgentRunsIsReviewing(t *testing.T) {
	f := newPRStageFixture(t, fakePR{Number: 21, Head: "feature/x", Draft: true})
	ctx := context.Background()
	if err := f.prs.DiscoverPRs(ctx, "LEG-1"); err != nil {
		t.Fatal(err)
	}
	stageStays(t, f.stages, f.place, store.WorkStageExploring)
	f.gh.set("acme/widget", fakePR{Number: 21, Head: "feature/x"})
	if err := f.prs.PollAll(ctx); err != nil {
		t.Fatal(err)
	}
	if got := stageSoon(t, f.stages, f.place, store.WorkStageReviewing); got.Reason != "PR #21 ready for review" {
		t.Fatalf("stage = %+v", got)
	}
}

// A push by the agent to the branch of a pull request that was already open
// is the Reviewing edge, though linking that pull request was not.
func TestAPushToAPullRequestAlreadyOpenIsReviewing(t *testing.T) {
	f := newPRStageFixture(t, fakePR{Number: 22, Head: "feature/x"})
	ctx := context.Background()
	if err := f.prs.DiscoverPRs(ctx, "LEG-1"); err != nil {
		t.Fatal(err)
	}
	stageStays(t, f.stages, f.place, store.WorkStageExploring)
	f.stages.Pushed(ctx, GitPush{TaskID: "LEG-1", ExecutionID: "ex-1", Repo: "acme/widget", Refs: []string{"refs/heads/feature/x"}})
	if got := stageSoon(t, f.stages, f.place, store.WorkStageReviewing); got.Reason != "pushed to PR #22" {
		t.Fatalf("stage = %+v", got)
	}
}

// Nor is a pull request already merged when tracking begins: the agent
// does not start Finished.
func TestAPullRequestAlreadyMergedWhenTrackingBeginsMovesNoStage(t *testing.T) {
	f := newPRStageFixture(t, fakePR{Number: 23, Head: "feature/x", State: "closed", Merged: true})
	if err := f.prs.DiscoverPRs(context.Background(), "LEG-1"); err != nil {
		t.Fatal(err)
	}
	if got := prList(t, f.prs, "LEG-1"); len(got) != 1 || got[0] != "feature/x:#23" {
		t.Fatalf("tracked = %v", got)
	}
	stageStays(t, f.stages, f.place, store.WorkStageExploring)
	f.noMilestone(t)
}

// LEG-519: an ephemeral task (an agent with no card) links no pull
// requests: not its working folder's branch's, found when its agent
// starts, and not one its agent opens or pushes through the broker. Polls
// never refresh it, so the link would go stale.
func TestAnEphemeralTaskLinksNoPullRequests(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.set("acme/widget", fakePR{Number: 1965, Head: "feature/x"}, fakePR{Number: 30, Head: "legato/walkthrough"})
	s, svc, _ := newBranchTracking(t, gh)
	ctx := context.Background()
	taskID, err := s.CreateEphemeralTask(ctx, "full walkthrough")
	if err != nil {
		t.Fatal(err)
	}
	setRepoContext(t, s, taskID, "acme/widget", "feature/x")

	if err := svc.DiscoverPRs(ctx, taskID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RediscoverPR(ctx, taskID); err != nil {
		t.Fatal(err)
	}
	branches := NewAgentBranches(s, svc)
	branches.Track(AgentBranch{TaskID: taskID, ExecutionID: "ex-1", Repo: "acme/widget", Branch: "legato/walkthrough", Opened: true})
	branches.Track(AgentBranch{TaskID: taskID, ExecutionID: "ex-1", Repo: "acme/widget", Branch: "legato/walkthrough"})
	branches.Wait()

	if got := prList(t, svc, taskID); len(got) != 0 {
		t.Fatalf("tracked = %v, want nothing", got)
	}
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.PRMeta != nil {
		t.Fatalf("pr_meta = %s, want none", *task.PRMeta)
	}
}
