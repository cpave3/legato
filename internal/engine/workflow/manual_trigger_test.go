package workflow_test

import (
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

func TestManualTriggerAndWorktreeRemoveDefinition(t *testing.T) {
	definition, err := workflow.Parse("cleanup.yaml", []byte(`
schema_version: 1
name: cleanup
title: Cleanup
steps:
  - id: prepare
    title: Prepare
    type: github.pr.prepare
    with: {repo: owner/repo, number: 1, primary_dir: /repo}
    outputs:
      worktree_path: {type: string}
  - id: cleanup
    title: Remove worktree
    type: worktree.remove
    trigger: manual
    with:
      expected_path: ${{ steps.prepare.outputs.worktree_path }}
`))
	if err != nil {
		t.Fatal(err)
	}
	if definition.Steps[0].Trigger != "automatic" || definition.Steps[1].Trigger != "manual" {
		t.Fatalf("triggers = %q, %q", definition.Steps[0].Trigger, definition.Steps[1].Trigger)
	}
}

func TestInvalidStepTriggerRejected(t *testing.T) {
	_, err := workflow.Parse("bad.yaml", []byte("schema_version: 1\nname: bad\ntitle: Bad\nsteps:\n  - id: run\n    title: Run\n    type: agent.run\n    trigger: eventually\n    with: {prompt: run}\n"))
	if err == nil {
		t.Fatal("invalid trigger was accepted")
	}
}
