package store

import (
	"context"
	"testing"
)

func childRun(id string, attempt int) WorkflowRun {
	return WorkflowRun{ID: id, WorkflowName: "child", WorkflowTitle: "Child", DefinitionSource: "test", DefinitionDigest: "sha", DefinitionJSON: `{}`, InputsJSON: `{}`, Status: "running", CurrentStepID: "s",
		ParentRunID: "wr-1", ParentStepPath: "review", ParentAttempt: attempt}
}

func childSteps(runID string) []WorkflowStep {
	return []WorkflowStep{{RunID: runID, StepID: "s", StepOrder: 0, Title: "S", ExecutorType: "task.note", DefinitionJSON: `{}`, Status: "pending"}}
}

// A workflow.run step's child is unique per attempt: a second create for
// the same attempt (a crash, then the reconcile) finds the first.
func TestAChildRunIsCreatedOncePerAttempt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "wf-task")
	createWorkflowFixture(t, s)

	id, created, err := s.CreateChildWorkflowRun(ctx, childRun("wr-c1", 1), childSteps("wr-c1"))
	if err != nil || !created || id != "wr-c1" {
		t.Fatalf("first create = %q, %v, %v", id, created, err)
	}
	id, created, err = s.CreateChildWorkflowRun(ctx, childRun("wr-c2", 1), childSteps("wr-c2"))
	if err != nil || created || id != "wr-c1" {
		t.Fatalf("second create = %q, %v, %v; want wr-c1 found", id, created, err)
	}
	if _, err := s.GetWorkflowRun(ctx, "wr-c2"); err == nil {
		t.Fatal("the second child was created")
	}
	view, err := s.GetWorkflowRun(ctx, "wr-1")
	if err != nil {
		t.Fatal(err)
	}
	if view.Steps[1].Path != "review" || view.Steps[1].ChildRunID != "wr-c1" {
		t.Fatalf("parent step = %+v", view.Steps[1])
	}
	child, err := s.GetWorkflowRun(ctx, "wr-c1")
	if err != nil || child.Run.ParentRunID != "wr-1" || child.Run.ParentStepPath != "review" || child.Run.ParentAttempt != 1 {
		t.Fatalf("child = %+v, %v", child, err)
	}

	if id, created, err = s.CreateChildWorkflowRun(ctx, childRun("wr-c3", 2), childSteps("wr-c3")); err != nil || !created || id != "wr-c3" {
		t.Fatalf("second attempt's create = %q, %v, %v", id, created, err)
	}
	children, err := s.ListChildWorkflowRuns(ctx, "wr-1")
	if err != nil || len(children) != 2 || children[0].ID != "wr-c1" || children[1].ID != "wr-c3" {
		t.Fatalf("children = %+v, %v", children, err)
	}
}

func TestARunsOutputsAreKeptOnceItSucceeds(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "wf-task")
	createWorkflowFixture(t, s)
	if err := s.SetWorkflowRunOutputs(ctx, "wr-1", `{"top_branch":"b"}`); err == nil {
		t.Fatal("a running run's outputs were set")
	}
	for _, step := range []struct{ path, next string }{{"prepare", "review"}, {"review", ""}} {
		attempt := WorkflowStepAttempt{ID: "wa-" + step.path, RunID: "wr-1", StepID: step.path, AttemptNumber: 1, ExecutorType: "x", TaskID: "wf-task", Status: "running"}
		if err := s.StartWorkflowStepAttempt(ctx, attempt, `{}`); err != nil {
			t.Fatal(err)
		}
		if err := s.CompleteWorkflowStep(ctx, "wr-1", step.path, attempt.ID, `{}`, step.next); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetWorkflowRunOutputs(ctx, "wr-1", `{"top_branch":"b"}`); err != nil {
		t.Fatal(err)
	}
	view, err := s.GetWorkflowRun(ctx, "wr-1")
	if err != nil || view.Run.OutputsJSON != `{"top_branch":"b"}` {
		t.Fatalf("run = %+v, %v", view, err)
	}
}
