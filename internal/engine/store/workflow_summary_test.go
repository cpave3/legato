package store

import (
	"context"
	"reflect"
	"testing"
)

// The runs list says where each run is without a second request: its steps
// in order with their state, and the title of the task it is for.
func TestListWorkflowRunsSummarisesStepsAndTheRelatedTask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, Task{ID: "gpri1ycr", Title: "Board search ignores workspace filter", Status: "Backlog"}); err != nil {
		t.Fatal(err)
	}
	steps := []WorkflowStepRun{
		{RunID: "triage", StepID: "context", StepOrder: 0, Title: "Read the task", ExecutorType: "task.context", DefinitionJSON: `{"trigger":"automatic"}`, Status: "succeeded"},
		{RunID: "triage", StepID: "approve", StepOrder: 1, Title: "Move to Ready", ExecutorType: "task.move", DefinitionJSON: `{"trigger":"manual"}`, Status: "waiting_manual"},
	}
	if err := s.CreateWorkflowRun(ctx, WorkflowRun{ID: "triage", WorkflowName: "triage-task", WorkflowTitle: "Triage gpri1ycr", DefinitionSource: "test", DefinitionDigest: "1", DefinitionJSON: `{}`, InputsJSON: `{}`, Status: "waiting", CurrentStepID: "approve", RelatedTaskID: "gpri1ycr"}, steps); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateWorkflowRun(ctx, WorkflowRun{ID: "job", WorkflowName: "legato-job", WorkflowTitle: "Agent job", DefinitionSource: "test", DefinitionDigest: "2", DefinitionJSON: `{}`, InputsJSON: `{}`, Status: "running", CurrentStepID: "agent"}, nil); err != nil {
		t.Fatal(err)
	}

	runs, err := s.ListWorkflowRuns(ctx, WorkflowRunFilter{WithSteps: true})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]WorkflowRun{}
	for _, run := range runs {
		byID[run.ID] = run
	}

	triage := byID["triage"]
	if triage.RelatedTaskTitle != "Board search ignores workspace filter" {
		t.Errorf("RelatedTaskTitle = %q", triage.RelatedTaskTitle)
	}
	want := []WorkflowStepSummary{
		{StepID: "context", Title: "Read the task", Status: "succeeded", Trigger: "automatic"},
		{StepID: "approve", Title: "Move to Ready", Status: "waiting_manual", Trigger: "manual"},
	}
	if !reflect.DeepEqual(triage.Steps, want) {
		t.Errorf("Steps = %#v, want %#v", triage.Steps, want)
	}

	job := byID["job"]
	if job.RelatedTaskTitle != "" || len(job.Steps) != 0 {
		t.Errorf("a run with no task and no steps: title %q, steps %#v", job.RelatedTaskTitle, job.Steps)
	}
}

// Callers that only count runs, such as the navigation badge, ask for every
// run and do not get the step summaries.
func TestListWorkflowRunsLeavesOutStepsUnlessAsked(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	steps := []WorkflowStepRun{{RunID: "r", StepID: "a", Title: "A", ExecutorType: "task.note", DefinitionJSON: `{}`, Status: "pending"}}
	if err := s.CreateWorkflowRun(ctx, WorkflowRun{ID: "r", WorkflowName: "w", WorkflowTitle: "W", DefinitionSource: "test", DefinitionDigest: "1", DefinitionJSON: `{}`, InputsJSON: `{}`, Status: "running", CurrentStepID: "a"}, steps); err != nil {
		t.Fatal(err)
	}
	runs, err := s.ListWorkflowRuns(ctx, WorkflowRunFilter{})
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs=%#v err=%v", runs, err)
	}
	if runs[0].Steps != nil {
		t.Errorf("Steps = %#v, want none", runs[0].Steps)
	}
}

// A run's page names its task too.
func TestGetWorkflowRunNamesTheRelatedTask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, Task{ID: "xk2pb7do", Title: "Add keyboard help to the plans page", Status: "Backlog"}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateWorkflowRun(ctx, WorkflowRun{ID: "plan", WorkflowName: "plan-task", WorkflowTitle: "Plan xk2pb7do", DefinitionSource: "test", DefinitionDigest: "1", DefinitionJSON: `{}`, InputsJSON: `{}`, Status: "running", CurrentStepID: "plan", RelatedTaskID: "xk2pb7do"}, nil); err != nil {
		t.Fatal(err)
	}
	view, err := s.GetWorkflowRun(ctx, "plan")
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.RelatedTaskTitle != "Add keyboard help to the plans page" {
		t.Errorf("RelatedTaskTitle = %q", view.Run.RelatedTaskTitle)
	}
}
