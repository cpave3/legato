package service

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

type workflowTaskMutationExecutor struct {
	store  *store.Store
	starts []ResolvedWorkflowStep
}

func (e *workflowTaskMutationExecutor) Type() string { return "task.metadata.set" }
func (e *workflowTaskMutationExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error {
	return nil
}
func (e *workflowTaskMutationExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	e.starts = append(e.starts, step)
	task, err := e.store.GetTask(ctx, step.TaskID)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	task.Title = "Updated workflow task"
	if err := e.store.UpdateTask(ctx, *task); err != nil {
		return WorkflowExecutionResult{}, err
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{}}, nil
}

func TestWorkflowTaskIntrinsicResolvesGeneratedTaskSeparatelyFromSubject(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.CreateTask(context.Background(), store.Task{ID: "subject-1", Title: "Subject", Status: "Backlog"}); err != nil {
		t.Fatal(err)
	}
	definition, err := workflowengine.Parse("test.yaml", []byte(`
schema_version: 1
name: identities
title: Identity test
inputs:
  task: {type: task, required: true}
steps:
  - id: mutate
    title: Mutate workflow task
    type: task.metadata.set
    with:
      task: ${{ workflow.task }}
  - id: inspect
    title: Inspect identities
    type: agent.run
    with:
      workflow_task: ${{ workflow.task }}
      workflow_id: ${{ workflow.task.id }}
      workflow_title: ${{ workflow.task.title }}
      subject_id: ${{ workflow.inputs.task.id }}
`))
	if err != nil {
		t.Fatal(err)
	}
	mutate := &workflowTaskMutationExecutor{store: db}
	inspect := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent"}}
	view, err := NewWorkflowService(db, []WorkflowExecutor{mutate, inspect}).Start(context.Background(), definition, map[string]string{"task": "subject-1"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.TaskID == view.Run.RelatedTaskID || view.Run.RelatedTaskID != "subject-1" {
		t.Fatalf("run identities = %+v", view.Run)
	}
	if len(inspect.starts) != 1 {
		t.Fatalf("inspect starts = %d", len(inspect.starts))
	}
	with := inspect.starts[0].With
	if with["workflow_id"] != view.Run.TaskID || with["subject_id"] != "subject-1" {
		t.Fatalf("resolved identities = %#v", with)
	}
	if with["workflow_title"] != "Updated workflow task" {
		t.Fatalf("workflow title = %#v", with["workflow_title"])
	}
	var persisted map[string]any
	if err := json.Unmarshal([]byte(view.Run.InputsJSON), &persisted); err != nil {
		t.Fatal(err)
	}
	if _, exists := persisted["workflow.task"]; exists || len(persisted) != 1 {
		t.Fatalf("persisted inputs = %#v", persisted)
	}
}

type workflowTaskFailingExecutor struct {
	starts []ResolvedWorkflowStep
}

func (e *workflowTaskFailingExecutor) Type() string { return "task.metadata.set" }
func (e *workflowTaskFailingExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error {
	return nil
}
func (e *workflowTaskFailingExecutor) Start(_ context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	e.starts = append(e.starts, step)
	if len(e.starts) == 1 {
		return WorkflowExecutionResult{}, errors.New("retry me")
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{}}, nil
}

func TestWorkflowTaskIntrinsicReloadsForManualAndRetryExecution(t *testing.T) {
	for _, trigger := range []string{"manual", "automatic"} {
		t.Run(trigger, func(t *testing.T) {
			db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			definition, err := workflowengine.Parse("test.yaml", []byte(`
schema_version: 1
name: current
title: Original workflow task
steps:
  - id: inspect
    title: Inspect
    type: task.metadata.set
    trigger: `+trigger+`
    with:
      title: ${{ workflow.task.title }}
`))
			if err != nil {
				t.Fatal(err)
			}
			executor := &workflowTaskFailingExecutor{}
			svc := NewWorkflowService(db, []WorkflowExecutor{executor})
			view, err := svc.Start(context.Background(), definition, nil)
			if err != nil {
				t.Fatal(err)
			}
			if trigger == "manual" {
				updateWorkflowTaskTitle(t, db, view.Run.TaskID, "Manual title")
				view, err = svc.TriggerStep(context.Background(), view.Run.ID, "inspect", false)
				if err != nil {
					t.Fatal(err)
				}
				if executor.starts[0].With["title"] != "Manual title" {
					t.Fatalf("manual title = %#v", executor.starts[0].With["title"])
				}
			}
			updateWorkflowTaskTitle(t, db, view.Run.TaskID, "Retry title")
			if _, err := svc.Retry(context.Background(), view.Run.ID, "inspect", false); err != nil {
				t.Fatal(err)
			}
			if executor.starts[len(executor.starts)-1].With["title"] != "Retry title" {
				t.Fatalf("retry title = %#v", executor.starts[len(executor.starts)-1].With["title"])
			}
		})
	}
}

func TestWorkflowTaskIntrinsicReloadsDuringAgentCompletion(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	definition, err := workflowengine.Parse("test.yaml", []byte(`
schema_version: 1
name: complete
title: Original workflow task
steps:
  - id: agent
    title: Agent
    type: agent.run
    with: {prompt: inspect}
    outputs:
      report: {type: file, path: "${{ workflow.task.title }}.md"}
`))
	if err != nil {
		t.Fatal(err)
	}
	executor := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent"}}
	svc := NewWorkflowService(db, []WorkflowExecutor{executor})
	view, err := svc.Start(context.Background(), definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	updateWorkflowTaskTitle(t, db, view.Run.TaskID, "Changed")
	_, err = svc.Complete(context.Background(), view.Run.ID, "agent", view.Attempts[0].ID, nil, map[string]string{"report": "Original workflow task.md"})
	if err == nil || !strings.Contains(err.Error(), `declared path "Changed.md"`) {
		t.Fatalf("completion error = %v", err)
	}
}

func updateWorkflowTaskTitle(t *testing.T, db *store.Store, taskID, title string) {
	t.Helper()
	task, err := db.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	task.Title = title
	if err := db.UpdateTask(context.Background(), *task); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowTaskInputResolvesCurrentTaskContext(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	provider, remoteID := "jira", "LEG-42"
	metadata := `{"triaged":false,"repository":{"name":"legato"}}`
	if err := db.CreateTask(context.Background(), store.Task{ID: "LEG-42", Title: "Investigate timeout", Description: "Check retries", DescriptionMD: "Check **retries**", RemoteDescriptionMD: "Connection **times out**", Status: "Backlog", Priority: "High", Provider: &provider, RemoteID: &remoteID, Metadata: &metadata}); err != nil {
		t.Fatal(err)
	}
	definition, err := workflowengine.Parse("test.yaml", []byte(`
schema_version: 1
name: triage
title: Triage task
inputs:
  task:
    type: task
    required: true
steps:
  - id: inspect
    title: Inspect task
    type: agent.run
    with:
      task: ${{ workflow.inputs.task }}
      prompt: "Triage ${{ workflow.inputs.task.title }} from ${{ workflow.inputs.task.metadata.repository.name }}: ${{ workflow.inputs.task.description }}"
`))
	if err != nil {
		t.Fatal(err)
	}
	executor := &fakeWorkflowExecutor{typeName: "agent.run"}
	svc := NewWorkflowService(db, []WorkflowExecutor{executor})
	view, err := svc.Start(context.Background(), definition, map[string]string{"task": "LEG-42"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.RelatedTaskID != "LEG-42" {
		t.Fatalf("related task = %q", view.Run.RelatedTaskID)
	}
	if len(executor.starts) != 1 {
		t.Fatalf("starts = %d", len(executor.starts))
	}
	got := executor.starts[0].With["task"].(map[string]any)
	if got["id"] != "LEG-42" || got["provider"] != "jira" || got["description"] != "Connection **times out**\n\n## Legato details\n\nCheck **retries**" {
		t.Fatalf("task context = %#v", got)
	}
	if executor.starts[0].With["prompt"] != "Triage Investigate timeout from legato: Connection **times out**\n\n## Legato details\n\nCheck **retries**" {
		t.Fatalf("prompt = %q", executor.starts[0].With["prompt"])
	}
}
