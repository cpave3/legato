package service

import (
	"context"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// relatedFixture is a real store with an agent service over a mock tmux, so
// the agents a loop relates are started and stopped as Legato does.
type relatedFixture struct {
	restartFixture
	related *RelatedAgentsService
}

func newRelatedFixture(t *testing.T) relatedFixture {
	t.Helper()
	f := newSlotFixture(t)
	return relatedFixture{restartFixture: f, related: NewRelatedAgentsService(f.store)}
}

func (f relatedFixture) spawn(t *testing.T, taskID, slot string) {
	t.Helper()
	if err := f.svc.SpawnAgent(context.Background(), taskID, 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: t.TempDir(), Slot: slot}); err != nil {
		t.Fatalf("starting %s in slot %q: %v", taskID, slot, err)
	}
}

func (f relatedFixture) relatedTo(t *testing.T, agentID string) []RelatedAgent {
	t.Helper()
	rows, err := f.related.RelatedAgents(context.Background(), agentID)
	if err != nil {
		t.Fatalf("related agents of %s: %v", agentID, err)
	}
	return rows
}

// handoffLoop makes a handed-off card with a review loop at pass.
func (f relatedFixture) handoffLoop(t *testing.T, taskID string, pass int, phase string) {
	t.Helper()
	ctx := context.Background()
	if err := f.store.CreateHandoffCard(ctx, store.Task{ID: taskID, Title: "Handed-off " + taskID, Status: "Doing"}, nil, store.Handoff{
		MainInstanceID: "srv-main", HandoffID: "ho-" + taskID, MainName: "erebus", MainTaskID: "M1", TaskID: taskID,
		Purpose: HandoffMakeChange, Kind: HandoffNewBranch, Repo: "acme/widget", AgentKind: "claude-code", State: store.HandoffInProgress,
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveHandoffReview(ctx, store.HandoffReview{TaskID: taskID, Phase: phase, Pass: pass}); err != nil {
		t.Fatal(err)
	}
}

// runLoop makes a review-pr-passes run whose review step waits on its loop,
// keyed on the run's task, and returns the run's ID.
func (f relatedFixture) runLoop(t *testing.T, runTask string, run reviewRun, pass int) string {
	t.Helper()
	ctx := context.Background()
	createTask(t, f.store, runTask)
	run.RunID, run.StepID, run.AttemptID = "wr-"+runTask, "review", "att-"+runTask
	if err := openReviewRun(ctx, f.store, runTask, run, ReviewPassSettings{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.store.CreateWorkflowRun(ctx, store.WorkflowRun{ID: run.RunID, WorkflowName: "review-pr-passes", WorkflowTitle: "Review loop",
		DefinitionJSON: "{}", InputsJSON: "{}", Status: "running", CurrentStepID: "review", TaskID: runTask},
		[]store.WorkflowStep{{RunID: run.RunID, StepID: "review", Title: "Review", ExecutorType: ReviewPassesStepType, DefinitionJSON: "{}", Status: "pending"}}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.StartWorkflowStepAttempt(ctx, store.WorkflowStepAttempt{ID: run.AttemptID, RunID: run.RunID, StepID: "review", AttemptNumber: 1,
		ExecutorType: ReviewPassesStepType, TaskID: runTask, Status: "running"}, "{}"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.WaitWorkflowStepOnService(ctx, run.RunID, "review", run.AttemptID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveHandoffReview(ctx, store.HandoffReview{TaskID: runTask, Phase: ReviewPhaseReviewing, Pass: pass}); err != nil {
		t.Fatal(err)
	}
	return run.RunID
}

func onlyRow(t *testing.T, rows []RelatedAgent) RelatedAgent {
	t.Helper()
	if len(rows) != 1 {
		t.Fatalf("related rows = %+v; want one", rows)
	}
	return rows[0]
}

func TestAHandoffLoopsAuthorAndReviewerListEachOther(t *testing.T) {
	f := newRelatedFixture(t)
	f.handoffLoop(t, "card-1", 2, ReviewPhaseReviewing)
	f.spawn(t, "card-1", "")
	f.spawn(t, "card-1", SlotReviewer)

	reviewer := onlyRow(t, f.relatedTo(t, "card-1"))
	if reviewer.AgentID != "card-1_reviewer" || reviewer.Role != RelatedRoleReviewer || reviewer.Pass != 2 || reviewer.Status != RelatedAgentRunning {
		t.Fatalf("the author's row = %+v; want the running reviewer at pass 2", reviewer)
	}
	if reviewer.Loop.Kind != RelatedLoopHandoff || reviewer.Loop.Phase != ReviewPhaseReviewing {
		t.Fatalf("the reviewer's loop = %+v", reviewer.Loop)
	}

	author := onlyRow(t, f.relatedTo(t, "card-1_reviewer"))
	if author.AgentID != "card-1" || author.Role != RelatedRoleAuthor || author.Status != RelatedAgentRunning || author.Title != "Handed-off card-1" {
		t.Fatalf("the reviewer's row = %+v; want the running author", author)
	}
}

func TestAnEndedReviewerStaysWhileItsLoopIsOpenAndGoesWhenTheLoopEnds(t *testing.T) {
	f := newRelatedFixture(t)
	ctx := context.Background()
	f.handoffLoop(t, "card-1", 1, ReviewPhaseWaitingForAuthor)
	f.spawn(t, "card-1", "")
	f.spawn(t, "card-1", SlotReviewer)
	if err := f.svc.(SlotAgents).KillAgentInSlot(ctx, "card-1", SlotReviewer); err != nil {
		t.Fatal(err)
	}

	if row := onlyRow(t, f.relatedTo(t, "card-1")); row.Status != RelatedAgentEnded || row.AgentID != "card-1_reviewer" {
		t.Fatalf("the author's row = %+v; want the reviewer marked ended", row)
	}
	if err := f.store.SaveHandoffReview(ctx, store.HandoffReview{TaskID: "card-1", Phase: ReviewPhaseEnded, Pass: 1}); err != nil {
		t.Fatal(err)
	}
	if rows := f.relatedTo(t, "card-1"); len(rows) != 0 {
		t.Fatalf("after the loop ended, the author's rows = %+v; want none", rows)
	}
	if rows := f.relatedTo(t, "card-1_reviewer"); len(rows) != 0 {
		t.Fatalf("after the loop ended, the reviewer's rows = %+v; want none", rows)
	}
}

func TestAHandoffWithNoPassYetListsNoReviewer(t *testing.T) {
	f := newRelatedFixture(t)
	f.handoffLoop(t, "card-1", 0, ReviewPhaseAuthor)
	f.spawn(t, "card-1", "")
	if rows := f.relatedTo(t, "card-1"); len(rows) != 0 {
		t.Fatalf("rows = %+v; want none before a first pass", rows)
	}
}

func TestAnAgentAuthoredWorkflowLoopsAuthorAndReviewerListEachOther(t *testing.T) {
	f := newRelatedFixture(t)
	createTask(t, f.store, "LEG-423")
	f.spawn(t, "LEG-423", "")
	runID := f.runLoop(t, "o7ooq1l4", reviewRun{Repo: "cpave3/legato", PR: 210, LinkedTask: "LEG-423", AuthorTask: "LEG-423"}, 3)
	f.spawn(t, "o7ooq1l4", SlotReviewer)

	reviewer := onlyRow(t, f.relatedTo(t, "LEG-423"))
	if reviewer.AgentID != "o7ooq1l4_reviewer" || reviewer.Role != RelatedRoleReviewer || reviewer.Pass != 3 {
		t.Fatalf("the author's row = %+v; want the run's reviewer at pass 3", reviewer)
	}
	if reviewer.Loop.Kind != RelatedLoopWorkflow || reviewer.Loop.RunID != runID || reviewer.Loop.Link != "/workflows/runs/"+runID {
		t.Fatalf("the reviewer's loop = %+v", reviewer.Loop)
	}

	author := onlyRow(t, f.relatedTo(t, "o7ooq1l4_reviewer"))
	if author.AgentID != "LEG-423" || author.Role != RelatedRoleAuthor || author.Status != RelatedAgentRunning {
		t.Fatalf("the reviewer's row = %+v; want LEG-423's main slot", author)
	}
}

func TestAWorkflowLoopWithoutAnAuthorListsTheLinkedTaskOnly(t *testing.T) {
	f := newRelatedFixture(t)
	createTask(t, f.store, "LEG-423")
	f.spawn(t, "LEG-423", "")
	f.runLoop(t, "o7ooq1l4", reviewRun{Repo: "cpave3/legato", PR: 210, LinkedTask: "LEG-423"}, 1)
	f.spawn(t, "o7ooq1l4", SlotReviewer)

	linked := onlyRow(t, f.relatedTo(t, "o7ooq1l4_reviewer"))
	if linked.Role != RelatedRoleLinkedTask || linked.TaskID != "LEG-423" || linked.AgentID != "" || linked.Status != RelatedAgentNone {
		t.Fatalf("the reviewer's row = %+v; want the linked task, with no agent", linked)
	}
	// The board task's agent still sees the reviewer of a loop on its PR.
	if row := onlyRow(t, f.relatedTo(t, "LEG-423")); row.AgentID != "o7ooq1l4_reviewer" {
		t.Fatalf("the linked task's row = %+v; want the reviewer", row)
	}
}

func TestAWorkflowLoopWithNoLinkedTaskListsNothing(t *testing.T) {
	f := newRelatedFixture(t)
	f.runLoop(t, "o7ooq1l4", reviewRun{Repo: "cpave3/legato", PR: 210}, 1)
	f.spawn(t, "o7ooq1l4", SlotReviewer)
	if rows := f.relatedTo(t, "o7ooq1l4_reviewer"); len(rows) != 0 {
		t.Fatalf("rows = %+v; want none", rows)
	}
}

func TestAFinishedWorkflowLoopDropsOut(t *testing.T) {
	f := newRelatedFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "LEG-423")
	f.spawn(t, "LEG-423", "")
	runID := f.runLoop(t, "o7ooq1l4", reviewRun{Repo: "cpave3/legato", PR: 210, LinkedTask: "LEG-423", AuthorTask: "LEG-423"}, 1)
	f.spawn(t, "o7ooq1l4", SlotReviewer)
	if err := f.store.CompleteWorkflowStep(ctx, runID, "review", "att-o7ooq1l4", `{"outcome":"approved"}`, ""); err != nil {
		t.Fatal(err)
	}
	if rows := f.relatedTo(t, "LEG-423"); len(rows) != 0 {
		t.Fatalf("the author's rows after the run's loop finished = %+v; want none", rows)
	}
	if rows := f.relatedTo(t, "o7ooq1l4_reviewer"); len(rows) != 0 {
		t.Fatalf("the reviewer's rows after the run's loop finished = %+v; want none", rows)
	}
}
