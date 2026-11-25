package store

import (
	"context"
	"errors"
	"testing"
)

// loopRun is a run at a loop step whose pass 0 has two steps, the first
// running. policy is the loop's on_failure, stepPolicy the first step's.
func loopRun(t *testing.T, policy, stepPolicy string) *Store {
	t.Helper()
	ctx := context.Background()
	s := newTestStore(t)
	loopDefinition := `{"id":"each","loop":{"repeat":true}}`
	if policy != "" {
		loopDefinition = `{"id":"each","loop":{"repeat":true,"on_failure":"` + policy + `"}}`
	}
	stepDefinition, testDefinition := `{"id":"build","type":"task.note"}`, `{"id":"test","type":"task.note"}`
	if stepPolicy != "" {
		stepDefinition = `{"id":"build","type":"task.note","on_failure":"` + stepPolicy + `"}`
		testDefinition = `{"id":"test","type":"task.note","on_failure":"` + stepPolicy + `"}`
	}
	run := WorkflowRun{ID: "wr-loop", WorkflowName: "loop", WorkflowTitle: "Loop", DefinitionSource: "t", DefinitionDigest: "d", DefinitionJSON: "{}", InputsJSON: "{}", Status: "running", CurrentStepID: "each", SchemaVersion: 2}
	if err := s.CreateWorkflowRun(ctx, run, []WorkflowStep{
		{RunID: "wr-loop", Path: "each", StepID: "each", StepOrder: 0, Title: "Each", DefinitionJSON: loopDefinition, Status: "pending"},
		{RunID: "wr-loop", Path: "after", StepID: "after", StepOrder: 1, Title: "After", ExecutorType: "task.note", Status: "pending"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.StartWorkflowStepAttempt(ctx, WorkflowStepAttempt{ID: "wa-loop", RunID: "wr-loop", Path: "each", StepID: "each", AttemptNumber: 1, ExecutorType: "loop", Status: "running"}, "{}"); err != nil {
		t.Fatal(err)
	}
	pass := 0
	if err := s.AdvanceWorkflowLoop(ctx, "wr-loop", "each", "{}", `{"passes":[{"index":0,"body":true,"open":true}]}`, []WorkflowStep{
		{RunID: "wr-loop", Path: "each/0/build", ParentPath: "each/0", StepID: "build", Pass: &pass, StepOrder: 0, ExecutorType: "task.note", DefinitionJSON: stepDefinition, Status: "pending"},
		{RunID: "wr-loop", Path: "each/0/test", ParentPath: "each/0", StepID: "test", Pass: &pass, StepOrder: 1, ExecutorType: "task.note", DefinitionJSON: testDefinition, Status: "pending"},
	}, "each/0/build"); err != nil {
		t.Fatal(err)
	}
	if err := s.StartWorkflowStepAttempt(ctx, WorkflowStepAttempt{ID: "wa-build", RunID: "wr-loop", Path: "each/0/build", StepID: "build", AttemptNumber: 1, ExecutorType: "task.note", Status: "running"}, "{}"); err != nil {
		t.Fatal(err)
	}
	return s
}

func loopView(t *testing.T, s *Store) (*WorkflowRunView, map[string]WorkflowStep) {
	t.Helper()
	view, err := s.GetWorkflowRun(context.Background(), "wr-loop")
	if err != nil {
		t.Fatal(err)
	}
	steps := map[string]WorkflowStep{}
	for _, step := range view.Steps {
		steps[step.Path] = step
	}
	return view, steps
}

func TestALoopMakesItsPassAndMovesTheRunIntoIt(t *testing.T) {
	s := loopRun(t, "", "")
	view, steps := loopView(t, s)
	if view.Run.CurrentStepID != "each/0/build" || steps["each/0/test"].Status != "pending" || steps["each"].LoopStateJSON == "" || steps["each"].Status != "running" {
		t.Fatalf("run at %s, steps %+v", view.Run.CurrentStepID, steps)
	}
	// A second host that read the old state is refused.
	err := s.AdvanceWorkflowLoop(context.Background(), "wr-loop", "each", "", `{"passes":[]}`, nil, "each/0/build")
	if !errors.Is(err, ErrWorkflowStateConflict) {
		t.Fatalf("a stale advance = %v, want a conflict", err)
	}
}

// A pass's step that fails with no on_failure of its own does what the
// loop's on_failure says: pause (the default) holds the run at it.
func TestAPassFailurePausesByDefault(t *testing.T) {
	s := loopRun(t, "", "")
	mode, err := s.FailWorkflowStepWith(context.Background(), "wr-loop", "each/0/build", "wa-build", StepFailure{Message: "boom"})
	if err != nil || mode != FailurePaused {
		t.Fatalf("mode = %s, %v", mode, err)
	}
	view, steps := loopView(t, s)
	if view.Run.Status != "waiting" || view.Run.CurrentStepID != "each/0/build" || steps["each/0/build"].Status != WorkflowStepPaused {
		t.Fatalf("run %s at %s, step %s", view.Run.Status, view.Run.CurrentStepID, steps["each/0/build"].Status)
	}
}

// Under continue or fail the pass ends: the run goes back to the loop,
// which decides, and a host is due to advance it.
func TestAPassFailureUnderContinueOrFailEndsThePass(t *testing.T) {
	for _, policy := range []string{"continue", "fail"} {
		s := loopRun(t, policy, "")
		mode, err := s.FailWorkflowStepWith(context.Background(), "wr-loop", "each/0/build", "wa-build", StepFailure{Message: "boom"})
		if err != nil || mode != FailurePassEnded {
			t.Fatalf("%s: mode = %s, %v", policy, mode, err)
		}
		view, steps := loopView(t, s)
		if view.Run.Status != "running" || view.Run.CurrentStepID != "each" || steps["each/0/build"].Status != "failed" || !steps["each/0/build"].AdvanceDue || steps["each/0/test"].Status != "pending" {
			t.Fatalf("%s: run %s at %s, steps %+v", policy, view.Run.Status, view.Run.CurrentStepID, steps)
		}
	}
}

// A step's own on_failure wins over the pass's: continue goes on to the
// pass's next step.
func TestAPassStepsOwnContinueGoesOnInThePass(t *testing.T) {
	s := loopRun(t, "fail", "continue")
	mode, err := s.FailWorkflowStepWith(context.Background(), "wr-loop", "each/0/build", "wa-build", StepFailure{Message: "boom", InProcess: true})
	if err != nil || mode != FailureContinued {
		t.Fatalf("mode = %s, %v", mode, err)
	}
	if view, _ := loopView(t, s); view.Run.CurrentStepID != "each/0/test" {
		t.Fatalf("run at %s, want each/0/test", view.Run.CurrentStepID)
	}
}

// After a pass's last step the run goes back to its loop, never to its end.
func TestThePassesLastStepLeadsBackToTheLoop(t *testing.T) {
	s := loopRun(t, "", "continue")
	ctx := context.Background()
	if err := s.SkipWorkflowStep(ctx, "wr-loop", "each/0/build", "each/0/test"); err == nil {
		t.Fatal("a running step was skipped")
	}
	if _, err := s.FailWorkflowStepWith(ctx, "wr-loop", "each/0/build", "wa-build", StepFailure{Message: "boom", InProcess: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.StartWorkflowStepAttempt(ctx, WorkflowStepAttempt{ID: "wa-test", RunID: "wr-loop", Path: "each/0/test", StepID: "test", AttemptNumber: 1, ExecutorType: "task.note", Status: "running"}, "{}"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FailWorkflowStepWith(ctx, "wr-loop", "each/0/test", "wa-test", StepFailure{Message: "again", InProcess: true}); err != nil {
		t.Fatal(err)
	}
	if view, _ := loopView(t, s); view.Run.Status != "running" || view.Run.CurrentStepID != "each" {
		t.Fatalf("run %s at %s, want running at each", view.Run.Status, view.Run.CurrentStepID)
	}
}

func TestALoopEndsWithItsOutputs(t *testing.T) {
	s := loopRun(t, "", "")
	ctx := context.Background()
	if err := s.CompleteWorkflowStep(ctx, "wr-loop", "each/0/build", "wa-build", "{}", "each/0/test"); err != nil {
		t.Fatal(err)
	}
	_, steps := loopView(t, s)
	state := steps["each"].LoopStateJSON
	if err := s.EndWorkflowLoop(ctx, "wr-loop", "each", "wa-loop", "stale", `{"ended_by":"until"}`, `{"count":1}`, "after"); !errors.Is(err, ErrWorkflowStateConflict) {
		t.Fatalf("an end from a stale state = %v", err)
	}
	// The run is at the pass's step, not the loop: the loop cannot end.
	if err := s.EndWorkflowLoop(ctx, "wr-loop", "each", "wa-loop", state, `{"ended_by":"until"}`, `{"count":1}`, "after"); !errors.Is(err, ErrWorkflowStateConflict) {
		t.Fatalf("an end while the run is in a pass = %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE workflow_runs SET current_step_id = 'each' WHERE id = 'wr-loop'`); err != nil {
		t.Fatal(err)
	}
	if err := s.EndWorkflowLoop(ctx, "wr-loop", "each", "wa-loop", state, `{"ended_by":"until"}`, `{"count":1}`, "after"); err != nil {
		t.Fatal(err)
	}
	view, steps := loopView(t, s)
	if view.Run.CurrentStepID != "after" || steps["each"].Status != "succeeded" || steps["each"].OutputsJSON != `{"count":1}` || steps["each"].LoopStateJSON != `{"ended_by":"until"}` {
		t.Fatalf("run at %s, loop %+v", view.Run.CurrentStepID, steps["each"])
	}
}

func TestALoopsStateIsWrittenOnlyFromTheStateRead(t *testing.T) {
	s := loopRun(t, "", "")
	ctx := context.Background()
	_, steps := loopView(t, s)
	if err := s.SetWorkflowLoopState(ctx, "wr-loop", "each", "stale", `{}`); !errors.Is(err, ErrWorkflowStateConflict) {
		t.Fatalf("a stale write = %v", err)
	}
	if err := s.SetWorkflowLoopState(ctx, "wr-loop", "each", steps["each"].LoopStateJSON, `{"ended_by":"failure"}`); err != nil {
		t.Fatal(err)
	}
}
