package store

import (
	"context"
	"errors"
	"testing"
)

func TestWorkflowStepAttemptAndCompletionAreCompareAndSet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "wf-task")
	createWorkflowFixture(t, s)

	attempt := WorkflowStepAttempt{ID: "wa-1", RunID: "wr-1", StepID: "prepare", AttemptNumber: 1, ExecutorType: "github.pr.prepare", TaskID: "wf-task", Status: "running"}
	if err := s.StartWorkflowStepAttempt(ctx, attempt, `{"repo":"acme/app"}`); err != nil {
		t.Fatalf("StartWorkflowStepAttempt: %v", err)
	}
	if err := s.CompleteWorkflowStep(ctx, "wr-1", "prepare", "wa-1", `{"worktree_path":"/tmp/wt"}`, "review"); err != nil {
		t.Fatalf("CompleteWorkflowStep: %v", err)
	}
	if err := s.CompleteWorkflowStep(ctx, "wr-1", "prepare", "wa-1", `{}`, "review"); !errors.Is(err, ErrWorkflowStateConflict) {
		t.Fatalf("second completion error = %v, want state conflict", err)
	}
	view, err := s.GetWorkflowRun(ctx, "wr-1")
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.CurrentStepID != "review" || view.Steps[0].Status != "succeeded" || view.Attempts[0].Status != "succeeded" {
		t.Fatalf("view after completion = %+v", view)
	}
}

func createWorkflowFixture(t *testing.T, s *Store) {
	t.Helper()
	run := WorkflowRun{ID: "wr-1", WorkflowName: "review-pr", WorkflowTitle: "Review PR", DefinitionSource: "test", DefinitionDigest: "sha", DefinitionJSON: `{}`, InputsJSON: `{}`, Status: "running", CurrentStepID: "prepare", TaskID: "wf-task"}
	steps := []WorkflowStepRun{
		{RunID: run.ID, StepID: "prepare", StepOrder: 0, Title: "Prepare", ExecutorType: "github.pr.prepare", DefinitionJSON: `{}`, Status: "pending"},
		{RunID: run.ID, StepID: "review", StepOrder: 1, Title: "Review", ExecutorType: "agent.run", DefinitionJSON: `{}`, Status: "pending"},
	}
	if err := s.CreateWorkflowRun(context.Background(), run, steps); err != nil {
		t.Fatal(err)
	}
}
