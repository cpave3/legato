package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// Migration 153 rebuilds agent_stages so a workflow can reset a place
// (source workflow), keeping every row it had.
func TestAgentStagesKeepTheirRowsAndTakeTheWorkflowSource(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legato.db")
	old := newStoreAtMigration(t, path, "152_workflow_step_effects.sql")
	if _, err := old.SetWorkStage(ctx, WorkStage{WorkStagePlace: WorkStagePlace{TaskID: "LEG-1"}, Stage: WorkStageFinished, Reason: "done", Source: WorkStageSourceAgent}); err != nil {
		t.Fatal(err)
	}
	if _, err := old.SetWorkStage(ctx, WorkStage{WorkStagePlace: WorkStagePlace{TaskID: "LEG-1"}, Stage: WorkStageImplementing, Source: WorkStageSourceWorkflow}); err == nil {
		t.Fatal("before migration 153 the workflow source was accepted")
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	kept, err := s.GetWorkStage(ctx, WorkStagePlace{TaskID: "LEG-1"})
	if err != nil || kept == nil || kept.Stage != WorkStageFinished || kept.Reason != "done" {
		t.Fatalf("kept = %+v, %v", kept, err)
	}
	if _, err := s.SetWorkStage(ctx, WorkStage{WorkStagePlace: WorkStagePlace{TaskID: "LEG-1"}, Stage: WorkStageImplementing, Source: WorkStageSourceWorkflow}); err != nil {
		t.Fatalf("the workflow source: %v", err)
	}
}

func waitFixture(t *testing.T) (*Store, string) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, Task{ID: "wf-task", Title: "Run", Status: "Backlog", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	run := WorkflowRun{ID: "wr-1", WorkflowName: "w", WorkflowTitle: "W", DefinitionJSON: "{}", InputsJSON: "{}", Status: "running", CurrentStepID: "build", TaskID: "wf-task"}
	if err := s.CreateWorkflowRun(ctx, run, []WorkflowStep{{RunID: "wr-1", Path: "build", StepID: "build", ExecutorType: "agent.run", Status: "pending"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.StartWorkflowStepAttempt(ctx, WorkflowStepAttempt{ID: "wa-1", RunID: "wr-1", Path: "build", StepID: "build", AttemptNumber: 1, ExecutorType: "agent.run", TaskID: "wf-task", Status: "running"}, "{}"); err != nil {
		t.Fatal(err)
	}
	return s, "wr-1"
}

// A wait's watermark is the place's newest stage event when the wait
// begins; with reset, the place goes back to Implementing in the same
// write, so only a Finished that comes after counts.
func TestAWaitsWatermarkIsThePlacesNewestEventAndOnlyALaterFinishedCounts(t *testing.T) {
	ctx := context.Background()
	s, runID := waitFixture(t)
	place := WorkStagePlace{TaskID: "LEG-7"}
	if _, err := s.SetWorkStage(ctx, WorkStage{WorkStagePlace: place, Stage: WorkStageFinished, Source: WorkStageSourceAgent}); err != nil {
		t.Fatal(err)
	}
	watermark, err := s.BeginWorkflowWait(ctx, runID, "build", WorkflowWait{Signals: []string{"finished"}, Place: place, Reset: true})
	if err != nil {
		t.Fatal(err)
	}
	if watermark == 0 {
		t.Fatal("watermark = 0; want the finished event's ID")
	}
	stage, _ := s.GetWorkStage(ctx, place)
	if stage.Stage != WorkStageImplementing || stage.Source != WorkStageSourceWorkflow {
		t.Fatalf("stage = %+v; want implementing from the workflow", stage)
	}
	if found, err := s.WorkStageEventAbove(ctx, place, watermark, WorkStageFinished); err != nil || found != nil {
		t.Fatalf("finished above the watermark = %+v, %v; want none yet", found, err)
	}
	if _, err := s.SetWorkStage(ctx, WorkStage{WorkStagePlace: place, Stage: WorkStageFinished, Reason: "again", Source: WorkStageSourceAgent}); err != nil {
		t.Fatal(err)
	}
	found, err := s.WorkStageEventAbove(ctx, place, watermark, WorkStageFinished)
	if err != nil || found == nil || found.Reason != "again" {
		t.Fatalf("finished above the watermark = %+v, %v", found, err)
	}
	view, _ := s.GetWorkflowRun(ctx, runID)
	if view.Steps[0].WaitWatermark != watermark || view.Steps[0].WaitSignals != `["finished"]` || view.Steps[0].WaitTaskID != "LEG-7" || view.Steps[0].WaitSlot != SlotMain {
		t.Fatalf("wait record = %+v", view.Steps[0])
	}
}

func TestAWaitBindsToItsAgentAndIsListedWhileItsStepWaits(t *testing.T) {
	ctx := context.Background()
	s, runID := waitFixture(t)
	if _, err := s.BeginWorkflowWait(ctx, runID, "build", WorkflowWait{Signals: []string{"complete", "ended"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkWorkflowStepWaiting(ctx, runID, "build", "wa-1", &AgentSession{ID: 4, TmuxSession: "legato-wf-task"}, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.BindWorkflowWait(ctx, runID, "build", "wa-1", WorkStagePlace{TaskID: "wf-task", PlaceExecutionID: "ex-1"}, 4); err != nil {
		t.Fatal(err)
	}
	waits, err := s.ListWorkflowAgentWaits(ctx)
	if err != nil || len(waits) != 1 {
		t.Fatalf("waits = %+v, %v", waits, err)
	}
	if w := waits[0]; w.WaitExecutionID != "ex-1" || w.WaitSessionID == nil || *w.WaitSessionID != 4 || w.WaitTaskID != "wf-task" {
		t.Fatalf("wait = %+v", w)
	}
	signals, err := s.WorkflowStepWaitSignals(ctx, runID, "build")
	if err != nil || len(signals) != 2 || signals[1] != "ended" {
		t.Fatalf("signals = %v, %v", signals, err)
	}
}

// A wait bound again to another agent (one resumed, or a new one in its
// place) keeps that agent's folder on the attempt: the session is deleted
// when the agent ends, and the step's outputs read the folder afterwards.
func TestAWaitBoundAgainKeepsItsAgentsFolder(t *testing.T) {
	ctx := context.Background()
	s, runID := waitFixture(t)
	if _, err := s.BeginWorkflowWait(ctx, runID, "build", WorkflowWait{Signals: []string{"ended"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkWorkflowStepWaiting(ctx, runID, "build", "wa-1", &AgentSession{ID: 4, WorkingDir: "/clones/old"}, "", ""); err != nil {
		t.Fatal(err)
	}
	id, err := s.InsertAgentSessionReturningID(ctx, AgentSession{TaskID: "wf-task", TmuxSession: "legato-wf-task", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateExecution(ctx, Execution{ID: "ex-2", Kind: ExecutionInteractive, Status: ExecutionRunning, TaskID: "wf-task", AgentSessionID: &id, WorkingDir: "/clones/new", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.BindWorkflowWait(ctx, runID, "build", "wa-1", WorkStagePlace{TaskID: "wf-task"}, id); err != nil {
		t.Fatal(err)
	}
	view, err := s.GetWorkflowRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if got := view.Attempts[0].WorkingDir; got != "/clones/new" {
		t.Fatalf("attempt's folder = %q; want the bound agent's", got)
	}
}

// A timeout is claimed once: two hosts sweeping the same deadline, one
// takes it.
func TestATimedOutStepIsClaimedOnce(t *testing.T) {
	ctx := context.Background()
	s, runID := waitFixture(t)
	deadline := time.Now().Add(-time.Minute)
	if err := s.SetWorkflowStepDeadline(ctx, runID, "build", deadline); err != nil {
		t.Fatal(err)
	}
	if late, err := s.ListWorkflowStepsPastDeadline(ctx, deadline.Add(-time.Second)); err != nil || len(late) != 0 {
		t.Fatalf("past an earlier time = %+v, %v", late, err)
	}
	late, err := s.ListWorkflowStepsPastDeadline(ctx, time.Now())
	if err != nil || len(late) != 1 || late[0].DeadlineAt == nil {
		t.Fatalf("past now = %+v, %v", late, err)
	}
	until := time.Now().Add(2 * time.Minute)
	first, err := s.ClaimWorkflowStepTimeout(ctx, runID, "build", *late[0].DeadlineAt, until)
	if err != nil || !first {
		t.Fatalf("first claim = %v, %v", first, err)
	}
	if second, err := s.ClaimWorkflowStepTimeout(ctx, runID, "build", *late[0].DeadlineAt, until); err != nil || second {
		t.Fatalf("second claim = %v, %v", second, err)
	}
}

// A claimed timeout goes stale: a host that claimed it and could not fail
// the step (it stopped, or stopping the agent failed) leaves the step past
// its deadline again once the claim is old, for another sweep to take.
func TestAClaimedTimeoutIsTakenAgainOnceItsClaimIsStale(t *testing.T) {
	ctx := context.Background()
	s, runID := waitFixture(t)
	if err := s.SetWorkflowStepDeadline(ctx, runID, "build", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	late, err := s.ListWorkflowStepsPastDeadline(ctx, time.Now())
	if err != nil || len(late) != 1 {
		t.Fatalf("past now = %+v, %v", late, err)
	}
	until := time.Now().Add(2 * time.Minute)
	if claimed, err := s.ClaimWorkflowStepTimeout(ctx, runID, "build", *late[0].DeadlineAt, until); err != nil || !claimed {
		t.Fatalf("claim = %v, %v", claimed, err)
	}
	if held, err := s.ListWorkflowStepsPastDeadline(ctx, time.Now()); err != nil || len(held) != 0 {
		t.Fatalf("while claimed = %+v, %v; want none", held, err)
	}
	stale, err := s.ListWorkflowStepsPastDeadline(ctx, until.Add(time.Second))
	if err != nil || len(stale) != 1 || stale[0].DeadlineAt == nil {
		t.Fatalf("once stale = %+v, %v; want the step again", stale, err)
	}
	if again, err := s.ClaimWorkflowStepTimeout(ctx, runID, "build", *stale[0].DeadlineAt, until.Add(2*time.Minute)); err != nil || !again {
		t.Fatalf("claim again = %v, %v", again, err)
	}
}

// A step recorded from the pane whose run has not moved on yet can be
// failed once: the run goes back to it (on_failure fail), and a second host
// finds nothing to fail.
func TestARecordedStepFailsOnceAndTheRunGoesBackToIt(t *testing.T) {
	for _, tc := range []struct {
		onFailure, mode, runStatus, current string
		due                                 bool
	}{
		{"", FailureFailed, "failed", "build", false},
		{FailurePaused, FailurePaused, "waiting", "build", false},
		{FailureContinued, FailureContinued, "running", "after", true},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			ctx := context.Background()
			s := newTestStore(t)
			if err := s.CreateTask(ctx, Task{ID: "wf-task", Title: "Run", Status: "Backlog", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"}); err != nil {
				t.Fatal(err)
			}
			run := WorkflowRun{ID: "wr-1", WorkflowName: "w", WorkflowTitle: "W", DefinitionJSON: "{}", InputsJSON: "{}", Status: "running", CurrentStepID: "build", TaskID: "wf-task"}
			steps := []WorkflowStep{
				{RunID: "wr-1", Path: "build", StepID: "build", StepOrder: 0, ExecutorType: "agent.run", Status: "pending", DefinitionJSON: `{"on_failure":"` + tc.onFailure + `"}`},
				{RunID: "wr-1", Path: "after", StepID: "after", StepOrder: 1, ExecutorType: "gate", Status: "pending", DefinitionJSON: "{}"},
			}
			if err := s.CreateWorkflowRun(ctx, run, steps); err != nil {
				t.Fatal(err)
			}
			if err := s.StartWorkflowStepAttempt(ctx, WorkflowStepAttempt{ID: "wa-1", RunID: "wr-1", Path: "build", StepID: "build", AttemptNumber: 1, ExecutorType: "agent.run", TaskID: "wf-task", Status: "waiting_agent"}, "{}"); err != nil {
				t.Fatal(err)
			}
			if err := s.RecordWorkflowStepCompletion(ctx, "wr-1", "build", "wa-1", `{"pr_number":12}`, "after"); err != nil {
				t.Fatal(err)
			}
			failure := StepFailure{Message: "github: 502", Code: "outputs_unavailable"}
			mode, err := s.FailRecordedWorkflowStep(ctx, "wr-1", "build", "wa-1", failure)
			if err != nil || mode != tc.mode {
				t.Fatalf("mode = %q, %v; want %q", mode, err, tc.mode)
			}
			view, err := s.GetWorkflowRun(ctx, "wr-1")
			if err != nil {
				t.Fatal(err)
			}
			if view.Run.Status != tc.runStatus || view.Run.CurrentStepID != tc.current {
				t.Fatalf("run = %s at %s; want %s at %s", view.Run.Status, view.Run.CurrentStepID, tc.runStatus, tc.current)
			}
			build := view.Steps[0]
			if build.ErrorCode != "outputs_unavailable" || build.Error != "github: 502" || build.AdvanceDue != tc.due || view.Attempts[0].Status != "failed" {
				t.Fatalf("build = %+v, attempt = %+v", build, view.Attempts[0])
			}
			if _, err := s.FailRecordedWorkflowStep(ctx, "wr-1", "build", "wa-1", failure); !errors.Is(err, ErrWorkflowStateConflict) {
				t.Fatalf("second fail = %v; want a conflict", err)
			}
		})
	}
}
