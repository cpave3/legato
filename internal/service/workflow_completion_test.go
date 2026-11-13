package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func TestWorkflowCompleteAdvancesAgentOutputIntoNextStep(t *testing.T) {
	db, _ := store.New(filepath.Join(t.TempDir(), "test.db"))
	defer db.Close()
	workDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(workDir, "review.md"), []byte("# Review"), 0o600)
	definition, err := workflowengine.Parse("test.yaml", []byte(`
schema_version: 1
name: chain
title: Chain
steps:
  - id: review
    title: Review
    type: agent.run
    with: {prompt: review}
    outputs:
      review: {type: file, path: review.md, media_type: text/markdown}
      findings: {type: json}
  - id: summarize
    title: Summarize
    type: agent.run
    with:
      prompt: "Summarize ${{ steps.review.outputs.findings }}"
`))
	if err != nil {
		t.Fatal(err)
	}
	first := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent", WorkingDir: workDir}}
	svc := NewWorkflowServiceWithOutputs(db, []WorkflowExecutor{first}, t.TempDir())
	view, err := svc.Start(context.Background(), definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	attempt := view.Attempts[0]
	view, err = svc.Complete(context.Background(), view.Run.ID, "review", attempt.ID, map[string]string{"findings": `[{"severity":"high"}]`}, map[string]string{"review": "review.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.starts) != 2 || first.starts[1].With["prompt"] != `Summarize [{"severity":"high"}]` || view.Run.CurrentStepID != "summarize" {
		t.Fatalf("starts=%+v run=%+v", first.starts, view.Run)
	}
	view, err = svc.Complete(context.Background(), view.Run.ID, "summarize", view.Attempts[1].ID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	task, err := db.GetTask(context.Background(), view.Run.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "succeeded" || task.Status != "Done" {
		t.Fatalf("run=%+v task=%+v", view.Run, task)
	}
}

// waitThenFailExecutor waits for the agent on its first step and cannot
// start the next one, as the learning curator could not start in a job
// clone's sandbox (LEG-156).
type waitThenFailExecutor struct{ starts int }

func (e *waitThenFailExecutor) Type() string { return "agent.run" }
func (e *waitThenFailExecutor) Start(context.Context, ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	e.starts++
	if e.starts > 1 {
		return WorkflowExecutionResult{}, errors.New("Legato's sandbox: refused")
	}
	return WorkflowExecutionResult{Status: "waiting_agent"}, nil
}
func (e *waitThenFailExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

// A completion whose step is recorded but whose next step cannot start says
// so: the caller must not take it for a refused completion and retry.
func TestWorkflowCompleteSaysTheStepWasRecordedWhenTheNextStepFails(t *testing.T) {
	db, _ := store.New(filepath.Join(t.TempDir(), "test.db"))
	defer db.Close()
	definition, err := workflowengine.Parse("test.yaml", []byte(`
schema_version: 1
name: chain
title: Chain
steps:
  - id: interview
    title: Interview
    type: agent.run
    with: {prompt: reflect}
  - id: curator
    title: Curate task learning
    type: agent.run
    with: {prompt: curate}
`))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewWorkflowService(db, []WorkflowExecutor{&waitThenFailExecutor{}})
	ctx := context.Background()
	view, err := svc.Start(ctx, definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	attempt := view.Attempts[0]
	_, err = svc.Complete(ctx, view.Run.ID, "interview", attempt.ID, nil, nil)
	if !errors.Is(err, ErrWorkflowStepRecorded) {
		t.Fatalf("err = %v, want ErrWorkflowStepRecorded", err)
	}
	for _, want := range []string{`"interview"`, "recorded", `"curator"`, "Legato's sandbox: refused"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %s", err, want)
		}
	}
	view, err = db.GetWorkflowRun(ctx, view.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range view.Steps {
		if step.StepID == "interview" && step.Status != "succeeded" {
			t.Errorf("interview step %s, want succeeded", step.Status)
		}
	}
}
