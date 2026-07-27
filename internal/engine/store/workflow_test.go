package store

import (
	"context"
	"testing"
)

func TestCreateWorkflowRunPersistsOrderedSteps(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "wf-task")
	run := WorkflowRun{
		ID: "wr-1", WorkflowName: "review-pr", WorkflowTitle: "Review PR",
		DefinitionSource: "/workflows/review.yaml", DefinitionDigest: "sha256:test",
		DefinitionJSON: `{}`, InputsJSON: `{"pr_number":42}`, Status: "running", CurrentStepID: "prepare", TaskID: "wf-task",
	}
	steps := []WorkflowStepRun{
		{RunID: run.ID, StepID: "prepare", StepOrder: 0, Title: "Prepare", ExecutorType: "github.pr.prepare", DefinitionJSON: `{}`, Status: "pending"},
		{RunID: run.ID, StepID: "review", StepOrder: 1, Title: "Review", ExecutorType: "agent.run", DefinitionJSON: `{}`, Status: "pending"},
	}
	if err := s.CreateWorkflowRun(ctx, run, steps); err != nil {
		t.Fatalf("CreateWorkflowRun: %v", err)
	}
	view, err := s.GetWorkflowRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetWorkflowRun: %v", err)
	}
	if view.Run.WorkflowName != "review-pr" || len(view.Steps) != 2 || view.Steps[1].StepID != "review" {
		t.Fatalf("view = %+v", view)
	}
}
