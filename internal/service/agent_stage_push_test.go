package service

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/store"
)

// pushingAgent records a running main-slot agent of taskID with its
// execution.
func pushingAgent(t *testing.T, s *store.Store, taskID, executionID string) {
	t.Helper()
	ctx := context.Background()
	id, err := s.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: taskID, TmuxSession: "legato-" + taskID, AgentKind: "claude-code", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateExecution(ctx, store.Execution{ID: executionID, Kind: store.ExecutionInteractive, Status: store.ExecutionRunning,
		TaskID: taskID, ExecutionTaskID: taskID, TaskTitle: "T", AgentSessionID: &id, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
}

// The credential channels pass each push on with the session that made it
// and the refs it carried, to every listener.
func TestTheCredentialChannelsPassPushesOnWithTheirExecution(t *testing.T) {
	var cache []string
	var pushes []GitPush
	c := &CredentialChannels{}
	c.AddPushListener(func(p GitPush) { cache = append(cache, p.Repo) })
	c.AddPushListener(func(p GitPush) { pushes = append(pushes, p) })
	c.Pushed(context.Background(), credential.Execution{TaskID: "LEG-1", ExecutionID: "ex-1"}, "github", "acme/widget", []string{"refs/heads/legato/leg-1"})

	want := GitPush{TaskID: "LEG-1", ExecutionID: "ex-1", Repo: "acme/widget", Refs: []string{"refs/heads/legato/leg-1"}}
	if len(cache) != 1 || cache[0] != "acme/widget" || len(pushes) != 1 || !reflect.DeepEqual(pushes[0], want) {
		t.Fatalf("heard %v and %+v", cache, pushes)
	}
}

// A push is evidence (Implementing) for the execution that made it; a push
// to a branch whose pull request is open puts the work back in review.
func TestAPushMovesItsAgentsStage(t *testing.T) {
	f := newStageFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "LEG-1")
	pushingAgent(t, f.store, "LEG-1", "ex-1")
	place := TaskPlace("LEG-1", "")
	_, _ = f.stages.Report(ctx, place, "ex-1", store.WorkStageFinished, "answered")

	f.stages.Pushed(ctx, GitPush{TaskID: "LEG-1", ExecutionID: "ex-1", Repo: "acme/widget", Refs: []string{"refs/heads/legato/leg-1"}})
	if got := f.stage(t, place); got.Stage != store.WorkStageImplementing || got.Source != store.WorkStageSourceEvidence || got.Reason != "pushed legato/leg-1" {
		t.Fatalf("after a push stage = %+v", got)
	}

	if _, err := f.store.TrackTaskBranches(ctx, "LEG-1", []store.BranchRef{{Repo: "acme/widget", Branch: "legato/leg-1"}}, BranchSourceAgent); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetTaskBranchPR(ctx, "LEG-1", "acme/widget", "legato/leg-1", &store.PRMeta{PRNumber: 12, State: "OPEN"}); err != nil {
		t.Fatal(err)
	}
	f.stages.Pushed(ctx, GitPush{TaskID: "LEG-1", ExecutionID: "ex-1", Repo: "acme/widget", Refs: []string{"refs/heads/legato/leg-1"}})
	if got := f.stage(t, place); got.Stage != store.WorkStageReviewing || got.Reason != "pushed to PR #12" {
		t.Fatalf("after a push to an open PR stage = %+v", got)
	}

	// A push from an execution that is not the session's moves nothing.
	_, _ = f.stages.Report(ctx, place, "ex-1", store.WorkStageFinished, "done")
	f.stages.Pushed(ctx, GitPush{TaskID: "LEG-1", ExecutionID: "ex-other", Repo: "acme/widget", Refs: []string{"refs/heads/legato/leg-1"}})
	if got := f.stage(t, place); got.Stage != store.WorkStageFinished {
		t.Fatalf("a stranger's push moved the stage to %+v", got)
	}
}

// A stack pushed before its pull requests open is found whole by a later
// push, even when the first look saw only some of them open.
func TestALaterPushFindsTheRestOfAStack(t *testing.T) {
	gh := newFakeGitHub(t)
	s, svc, _ := newBranchTracking(t, gh)
	createPRTask(t, s, "LEG-1")
	branches := NewAgentBranches(s, svc)
	stack := []string{"legato/leg-1-a", "legato/leg-1-b", "legato/leg-1-c", "legato/leg-1-d"}
	gh.set("acme/widget", fakePR{Number: 11, Head: stack[0]}, fakePR{Number: 12, Head: stack[1], Base: stack[0]})
	for _, b := range stack {
		branches.Track(AgentBranch{TaskID: "LEG-1", ExecutionID: "ex-1", Repo: "acme/widget", Branch: b})
	}
	branches.Wait()
	if got := prList(t, svc, "LEG-1"); !reflect.DeepEqual(got, []string{"legato/leg-1-a:#11", "legato/leg-1-b:#12", "legato/leg-1-c", "legato/leg-1-d"}) {
		t.Fatalf("after the first look = %v", got)
	}

	// The other two open on GitHub; the agent pushes the top again.
	gh.set("acme/widget", fakePR{Number: 11, Head: stack[0]}, fakePR{Number: 12, Head: stack[1], Base: stack[0]},
		fakePR{Number: 13, Head: stack[2], Base: stack[1]}, fakePR{Number: 14, Head: stack[3], Base: stack[2]})
	branches.Track(AgentBranch{TaskID: "LEG-1", ExecutionID: "ex-1", Repo: "acme/widget", Branch: stack[3]})
	branches.Wait()
	want := []string{"legato/leg-1-a:#11", "legato/leg-1-b:#12", "legato/leg-1-c:#13", "legato/leg-1-d:#14"}
	if got := prList(t, svc, "LEG-1"); !reflect.DeepEqual(got, want) {
		t.Fatalf("after a later push = %v, want %v", got, want)
	}
}

// A task whose pull request merged gets a new one: its new branch and pull
// request are tracked beside the old, and the work is in review again.
func TestANewPullRequestOnATaskWhosePullRequestMergedIsTracked(t *testing.T) {
	gh := newFakeGitHub(t)
	s, svc, bus := newBranchTracking(t, gh)
	createPRTask(t, s, "LEG-1")
	stages := startMilestones(t, s, bus)
	place := TaskPlace("LEG-1", "")
	branches := NewAgentBranches(s, svc)
	gh.set("acme/widget", fakePR{Number: 11, Head: "legato/leg-1", State: "closed", Merged: true})
	branches.Track(AgentBranch{TaskID: "LEG-1", ExecutionID: "ex-1", Repo: "acme/widget", Branch: "legato/leg-1"})
	branches.Wait()
	stageSoon(t, stages, place, store.WorkStageFinished)

	gh.set("acme/widget", fakePR{Number: 11, Head: "legato/leg-1", State: "closed", Merged: true}, fakePR{Number: 15, Head: "legato/leg-1-more"})
	branches.Track(AgentBranch{TaskID: "LEG-1", ExecutionID: "ex-1", Repo: "acme/widget", Branch: "legato/leg-1-more"})
	branches.Wait()
	if got := prList(t, svc, "LEG-1"); !reflect.DeepEqual(got, []string{"legato/leg-1:#11", "legato/leg-1-more:#15"}) {
		t.Fatalf("tracked = %v", got)
	}
	stageSoon(t, stages, place, store.WorkStageReviewing)
}

// Starting the agent again in the same checkout keeps the stack its task
// tracks: the code context is captured again, and nothing found is lost.
func TestCapturingTheSameCheckoutAgainKeepsTheStack(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.set("acme/widget", fakePR{Number: 11, Head: "leg-1-a"}, fakePR{Number: 12, Head: "leg-1-b", Base: "leg-1-a"})
	s, svc, _ := newBranchTracking(t, gh)
	ctx := context.Background()
	createPRTask(t, s, "LEG-1")
	inspector := &fakeCheckoutInspector{context: &store.CodeContext{Repo: "acme/widget", Branch: "leg-1-b", BaseBranch: "main"}}
	agent := &agentService{store: s, checkoutInspector: inspector, prSvc: svc}
	agent.captureAgentCodeContext("LEG-1", "/w")
	// The ticket's key finds both pull requests of the stack.
	if _, err := s.TrackTaskBranches(ctx, "LEG-1", []store.BranchRef{{Repo: "acme/widget", Branch: "leg-1-a"}}, BranchSourceDiscovered); err != nil {
		t.Fatal(err)
	}
	if err := svc.PollAll(ctx); err != nil {
		t.Fatal(err)
	}
	before := prList(t, svc, "LEG-1")
	if len(before) != 2 {
		t.Fatalf("tracked = %v", before)
	}

	agent.captureAgentCodeContext("LEG-1", "/w")
	if got := prList(t, svc, "LEG-1"); !reflect.DeepEqual(got, before) {
		t.Fatalf("after capturing again = %v, want %v", got, before)
	}
}
