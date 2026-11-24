package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func TestWorkflowStartResolvesRunTitleFromExpandedInputs(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	metadata := `{"pull_request":{"number":42}}`
	if err := db.CreateTask(context.Background(), store.Task{ID: "task-42", Title: "Review checkout", Status: "Backlog", Metadata: &metadata}); err != nil {
		t.Fatal(err)
	}
	definition, err := workflowengine.Parse("review.yaml", []byte(`
schema_version: 1
name: review-pr
title: Review pull request
run_title: "Review PR #${{ workflow.inputs.card.metadata.pull_request.number }}: ${{ workflow.inputs.card.title }}"
inputs:
  card: {type: task, required: true}
steps:
  - id: review
    title: Review
    type: agent.run
    with: {prompt: review}
`))
	if err != nil {
		t.Fatal(err)
	}
	executor := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent"}}

	view, err := NewWorkflowService(db, []WorkflowExecutor{executor}).Start(context.Background(), definition, map[string]string{"card": "task-42"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	const want = "Review PR #42: Review checkout"
	if view.Run.WorkflowTitle != want {
		t.Fatalf("workflow title = %q, want %q", view.Run.WorkflowTitle, want)
	}
	generated, err := db.GetTask(context.Background(), view.Run.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if generated.Title != want {
		t.Fatalf("execution task title = %q, want %q", generated.Title, want)
	}
}

// startNamedAgentRun starts a run whose agent.run step names its agent, on
// card LEG-9 (Burnlite's LEG-9 when remote).
func startNamedAgentRun(t *testing.T, runTitle string, remote bool) (AgentService, *store.Store, *store.WorkflowRunView) {
	t.Helper()
	agents, s, _ := newTestAgentService(t)
	ctx := context.Background()
	card := store.Task{ID: "LEG-9", Title: "Fix login", Status: "Doing"}
	if remote {
		provider, remoteID := "burnlite", "LEG-9"
		card.ID, card.Provider, card.RemoteID = "c9x2k1m4", &provider, &remoteID
	}
	if err := s.CreateTask(ctx, card); err != nil {
		t.Fatal(err)
	}
	yaml := `
schema_version: 1
name: one-off
title: One-off task
` + runTitle + `
inputs:
  task: {type: task, required: true}
steps:
  - id: triage
    title: Triage
    type: agent.run
    with: {name: "Triage - ${{ workflow.inputs.task.title }}", prompt: Triage it, agent: claude-code}
`
	def, err := workflowengine.Parse("one-off.yaml", []byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	workflows := NewWorkflowService(s, []WorkflowExecutor{NewAgentRunExecutor(NewWorkflowAgentRunner(agents, s))})
	view, err := workflows.Start(ctx, def, map[string]string{"task": card.ID})
	if err != nil {
		t.Fatal(err)
	}
	return agents, s, view
}

func TestANamedAgentRunStepNamesItsAgentAndLeavesTheRunTitle(t *testing.T) {
	agents, s, view := startNamedAgentRun(t, `run_title: "Ship ${{ workflow.inputs.task.title }}"`, false)
	ctx := context.Background()
	const want = "Ship Fix login"
	task, err := s.GetTask(ctx, view.Run.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Title != want {
		t.Fatalf("the run's task is titled %q, want the run title %q", task.Title, want)
	}
	if run, err := s.GetWorkflowRun(ctx, view.Run.ID); err != nil || run.Run.WorkflowTitle != want {
		t.Fatalf("run title = %+v, %v", run, err)
	}
	listed, err := agents.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range listed {
		if a.TaskID == view.Run.TaskID {
			if a.Title != "Triage - Fix login" {
				t.Fatalf("the agent is titled %q, want its step's name", a.Title)
			}
			if a.RunCard == nil || a.RunCard.TaskID != "LEG-9" || a.RunCard.Key != "LEG-9" || a.RunCard.Title != "Fix login" {
				t.Fatalf("the agent's run card = %+v, want LEG-9", a.RunCard)
			}
			return
		}
	}
	t.Fatalf("the step's agent is not listed: %+v", listed)
}

func TestARunWithNoRunTitleIsNamedAfterItsWorkflowAndCard(t *testing.T) {
	for _, remote := range []bool{false, true} {
		_, s, view := startNamedAgentRun(t, "", remote)
		task, err := s.GetTask(context.Background(), view.Run.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		if task.Title != "One-off task · LEG-9" || view.Run.WorkflowTitle != "One-off task · LEG-9" {
			t.Fatalf("remote %v: task %q, run %q, want \"One-off task · LEG-9\"", remote, task.Title, view.Run.WorkflowTitle)
		}
	}
}
