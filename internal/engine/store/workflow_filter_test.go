package store

import (
	"context"
	"testing"
)

func TestListWorkflowRunsFiltersWorkflowAndNormalizedInputs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, run := range []WorkflowRun{
		{ID: "matching-new", WorkflowName: "review-pr", WorkflowTitle: "Review", DefinitionSource: "test", DefinitionDigest: "1", DefinitionJSON: `{}`, InputsJSON: `{"repo":"cpave3/legato","pr_number":42,"draft":false}`, Status: "running", CurrentStepID: "review"},
		{ID: "matching-old", WorkflowName: "review-pr", WorkflowTitle: "Review", DefinitionSource: "test", DefinitionDigest: "2", DefinitionJSON: `{}`, InputsJSON: `{"repo":"cpave3/legato","pr_number":"42","draft":"false"}`, Status: "succeeded", CurrentStepID: "cleanup"},
		{ID: "wrong-pr", WorkflowName: "review-pr", WorkflowTitle: "Review", DefinitionSource: "test", DefinitionDigest: "3", DefinitionJSON: `{}`, InputsJSON: `{"repo":"cpave3/legato","pr_number":43,"draft":false}`, Status: "succeeded", CurrentStepID: "cleanup"},
		{ID: "wrong-workflow", WorkflowName: "other", WorkflowTitle: "Other", DefinitionSource: "test", DefinitionDigest: "4", DefinitionJSON: `{}`, InputsJSON: `{"repo":"cpave3/legato","pr_number":42,"draft":false}`, Status: "succeeded", CurrentStepID: "done"},
	} {
		if err := s.CreateWorkflowRun(ctx, run, nil); err != nil {
			t.Fatalf("CreateWorkflowRun(%s): %v", run.ID, err)
		}
	}

	runs, err := s.ListWorkflowRuns(ctx, WorkflowRunFilter{
		WorkflowName: "review-pr",
		Inputs:       map[string]string{"repo": "cpave3/legato", "pr_number": "42", "draft": "false"},
		Limit:        10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].ID != "matching-old" || runs[1].ID != "matching-new" {
		t.Fatalf("runs = %#v", runs)
	}
}

func TestListWorkflowRunsCombinesStatusAndLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"one", "two"} {
		if err := s.CreateWorkflowRun(ctx, WorkflowRun{ID: id, WorkflowName: "review-pr", WorkflowTitle: "Review", DefinitionSource: "test", DefinitionDigest: id, DefinitionJSON: `{}`, InputsJSON: `{}`, Status: "running", CurrentStepID: "review"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	runs, err := s.ListWorkflowRuns(ctx, WorkflowRunFilter{Status: "running", Limit: 1})
	if err != nil || len(runs) != 1 || runs[0].ID != "two" {
		t.Fatalf("runs=%#v err=%v", runs, err)
	}
}
