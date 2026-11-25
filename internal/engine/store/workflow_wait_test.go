package store

import (
	"context"
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
	first, err := s.ClaimWorkflowStepTimeout(ctx, runID, "build", *late[0].DeadlineAt)
	if err != nil || !first {
		t.Fatalf("first claim = %v, %v", first, err)
	}
	if second, err := s.ClaimWorkflowStepTimeout(ctx, runID, "build", *late[0].DeadlineAt); err != nil || second {
		t.Fatalf("second claim = %v, %v", second, err)
	}
}
