package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

type fakeWorkflowExecutor struct {
	typeName string
	starts   []ResolvedWorkflowStep
	result   WorkflowExecutionResult
}

func (f *fakeWorkflowExecutor) Type() string { return f.typeName }
func (f *fakeWorkflowExecutor) Start(_ context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	f.starts = append(f.starts, step)
	return f.result, nil
}
func (f *fakeWorkflowExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

func TestWorkflowStartPassesDeterministicOutputToAgentStep(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	definition, err := workflowengine.Parse("review.yaml", []byte(`
schema_version: 1
name: review-pr
title: Review PR
inputs:
  repo: {type: string, required: true}
steps:
  - id: prepare
    title: Prepare
    type: github.pr.prepare
    with: {repo: "${{ workflow.inputs.repo }}"}
    outputs:
      worktree_path: {type: string}
  - id: review
    title: Review
    type: agent.run
    with:
      working_dir: "${{ steps.prepare.outputs.worktree_path }}"
      prompt: review it
`))
	if err != nil {
		t.Fatal(err)
	}
	prepare := &fakeWorkflowExecutor{typeName: "github.pr.prepare", result: WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"worktree_path": "/tmp/worktree"}}}
	agent := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent"}}
	svc := NewWorkflowService(s, []WorkflowExecutor{prepare, agent})

	view, err := svc.Start(context.Background(), definition, map[string]string{"repo": "acme/app"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(agent.starts) != 1 || agent.starts[0].With["working_dir"] != "/tmp/worktree" {
		t.Fatalf("agent starts = %+v", agent.starts)
	}
	if view.Run.Status != "running" || view.Run.CurrentStepID != "review" {
		t.Fatalf("run = %+v", view.Run)
	}
}
