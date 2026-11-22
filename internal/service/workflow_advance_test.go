package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// oneAgentPerTask is an agent.run executor that refuses a second agent on
// a task while the first runs, as the agent service's SpawnAgent does:
// only Finish lets the next one start.
type oneAgentPerTask struct {
	running  map[string]bool
	starts   []ResolvedWorkflowStep
	finished []store.WorkflowStepAttempt
}

func (e *oneAgentPerTask) Type() string { return "agent.run" }
func (e *oneAgentPerTask) Start(_ context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	if e.running[step.TaskID] {
		return WorkflowExecutionResult{}, errors.New("agent already running for task " + step.TaskID)
	}
	e.running[step.TaskID] = true
	e.starts = append(e.starts, step)
	return WorkflowExecutionResult{Status: "waiting_agent"}, nil
}
func (e *oneAgentPerTask) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }
func (e *oneAgentPerTask) Finish(_ context.Context, attempt store.WorkflowStepAttempt) error {
	e.running[attempt.TaskID] = false
	e.finished = append(e.finished, attempt)
	return nil
}

const twoAgentSteps = "schema_version: 1\nname: chain\ntitle: Chain\nsteps:\n" +
	"  - {id: one, title: One, type: agent.run, with: {prompt: one}}\n" +
	"  - {id: two, title: Two, type: agent.run, with: {prompt: two}}\n"

func advanceFixture(t *testing.T, yaml string) (*store.Store, *oneAgentPerTask, *workflowengine.Definition) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	definition, err := workflowengine.Parse("chain.yaml", []byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return db, &oneAgentPerTask{running: map[string]bool{}}, definition
}

// Two agent.run steps in a row on one run (LEG-452): the first agent is
// finished before the second step starts on the same task.
func TestTwoAgentStepsInARowWork(t *testing.T) {
	ctx := context.Background()
	db, agents, definition := advanceFixture(t, twoAgentSteps)
	svc := NewWorkflowServiceWithOutputs(db, []WorkflowExecutor{agents}, t.TempDir())
	view, err := svc.Start(ctx, definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	view, err = svc.Complete(ctx, view.Run.ID, "one", view.Attempts[0].ID, nil, nil)
	if err != nil {
		t.Fatalf("complete the first step: %v", err)
	}
	if len(agents.starts) != 2 || len(agents.finished) != 1 || view.Run.CurrentStepID != "two" || view.Run.Status != "running" {
		t.Fatalf("starts=%d finished=%d run=%+v", len(agents.starts), len(agents.finished), view.Run)
	}
	for _, step := range view.Steps {
		if step.Path == "two" && step.Status != "waiting_agent" {
			t.Fatalf("second step = %+v", step)
		}
		if step.AdvanceDue {
			t.Fatalf("an advance done is not due: %+v", step)
		}
	}
}

// A completion from the agent's pane only records the step; a host then
// finishes the agent and advances the run (spec LEG-482 §5.4).
func TestACompletionFromThePaneOnlyRecordsAndAHostAdvances(t *testing.T) {
	ctx := context.Background()
	db, agents, definition := advanceFixture(t, twoAgentSteps)
	host := NewWorkflowServiceWithOutputs(db, []WorkflowExecutor{agents}, t.TempDir())
	view, err := host.Start(ctx, definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	pane := NewWorkflowServiceWithOutputs(db, []WorkflowExecutor{agents}, t.TempDir())
	pane.SetRecordOnly()
	view, err = pane.Complete(ctx, view.Run.ID, "one", view.Attempts[0].ID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents.finished) != 0 || len(agents.starts) != 1 {
		t.Fatalf("the pane finished %d agents and started %d steps", len(agents.finished), len(agents.starts))
	}
	if view.Steps[0].Status != "succeeded" || !view.Steps[0].AdvanceDue || view.Run.CurrentStepID != "two" {
		t.Fatalf("recorded: run at %q, first step %+v", view.Run.CurrentStepID, view.Steps[0])
	}
	// Completing again is a conflict: the step is recorded.
	if _, err := pane.Complete(ctx, view.Run.ID, "one", view.Attempts[0].ID, nil, nil); !errors.Is(err, store.ErrWorkflowStateConflict) {
		t.Fatalf("second completion = %v", err)
	}

	moved, err := host.AdvanceDue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 1 || len(agents.finished) != 1 || agents.finished[0].Path != "one" || len(agents.starts) != 2 {
		t.Fatalf("moved=%v finished=%v starts=%d", moved, agents.finished, len(agents.starts))
	}
	if again, err := host.AdvanceDue(ctx); err != nil || len(again) != 0 {
		t.Fatalf("advanced twice: %v, %v", again, err)
	}
}

func TestAHostKeepsAKeptAgentAndEndsTheRunAfterTheLastStep(t *testing.T) {
	ctx := context.Background()
	db, agents, definition := advanceFixture(t, "schema_version: 2\nname: keep\ntitle: Keep\nsteps:\n  - {id: one, title: One, type: agent.run, with: {prompt: one, keep_agent: true}}\n")
	svc := NewWorkflowServiceWithOutputs(db, []WorkflowExecutor{agents}, t.TempDir())
	view, err := svc.Start(ctx, definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.SchemaVersion != 2 {
		t.Fatalf("the run keeps its definition's schema version: %d", view.Run.SchemaVersion)
	}
	view, err = svc.Complete(ctx, view.Run.ID, "one", view.Attempts[0].ID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents.finished) != 0 || view.Run.Status != "succeeded" || view.Steps[0].AdvanceDue {
		t.Fatalf("finished=%d run=%+v step=%+v", len(agents.finished), view.Run, view.Steps[0])
	}
}

// The advance loop moves on what was recorded while no host ran, and what
// a pane records later.
func TestTheAdvanceLoopMovesRecordedSteps(t *testing.T) {
	ctx := context.Background()
	db, agents, definition := advanceFixture(t, "schema_version: 1\nname: chain\ntitle: Chain\nsteps:\n"+
		"  - {id: one, title: One, type: agent.run, with: {prompt: one}}\n"+
		"  - {id: two, title: Two, type: agent.run, with: {prompt: two}}\n"+
		"  - {id: three, title: Three, type: agent.run, with: {prompt: three}}\n")
	host := NewWorkflowServiceWithOutputs(db, []WorkflowExecutor{agents}, t.TempDir())
	pane := NewWorkflowServiceWithOutputs(db, []WorkflowExecutor{agents}, t.TempDir())
	pane.SetRecordOnly()
	view, err := host.Start(ctx, definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pane.Complete(ctx, view.Run.ID, "one", view.Attempts[0].ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	bus := events.New()
	stop := host.StartAdvanceLoop(ctx, bus)
	defer stop()
	if len(agents.starts) != 2 {
		t.Fatalf("the loop's first pass started %d steps, want 2", len(agents.starts))
	}
	view, err = db.GetWorkflowRun(ctx, view.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pane.Complete(ctx, view.Run.ID, "two", view.Attempts[1].ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	bus.Publish(events.Event{Type: events.EventWorkflowChanged, At: time.Now(), Payload: events.WorkflowChangedPayload{RunID: view.Run.ID, StepID: "two", Kind: "completed_step"}})
	deadline := time.Now().Add(5 * time.Second)
	for {
		view, err = db.GetWorkflowRun(ctx, view.Run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if view.Run.CurrentStepID == "three" && len(view.Attempts) == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the loop did not advance: %+v", view.Run)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
