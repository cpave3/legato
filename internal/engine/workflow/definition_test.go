package workflow_test

import (
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

func TestParseValidateAndRenderWorkflow(t *testing.T) {
	raw := []byte(`
schema_version: 1
name: review-pr
title: Review pull request
inputs:
  repo:
    type: string
    required: true
  pr_number:
    type: integer
    required: true
steps:
  - id: prepare
    title: Prepare PR
    type: github.pr.prepare
    with:
      repo: ${{ workflow.inputs.repo }}
      number: ${{ workflow.inputs.pr_number }}
      primary_dir: /projects/repo
      base_branch: main
    outputs:
      worktree_path: {type: string}
      metadata: {type: json}
  - id: review
    title: Review code
    type: agent.run
    with:
      working_dir: ${{ steps.prepare.outputs.worktree_path }}
      prompt: |
        Review PR ${{ workflow.inputs.pr_number }} in ${{ workflow.inputs.repo }}.
        Metadata: ${{ steps.prepare.outputs.metadata }}
      agent: chimera
    outputs:
      review:
        type: file
        path: review.md
        media_type: text/markdown
`)

	definition, err := workflow.Parse("review-pr.yaml", raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	inputs, err := definition.NormalizeInputs(map[string]string{"repo": "acme/app", "pr_number": "42"})
	if err != nil {
		t.Fatalf("NormalizeInputs: %v", err)
	}
	if got := inputs["pr_number"]; got != int64(42) {
		t.Fatalf("pr_number = %#v, want int64(42)", got)
	}

	resolved, err := definition.ResolveStep("review", inputs, map[string]map[string]any{
		"prepare": {
			"worktree_path": "/tmp/app-pr-42",
			"metadata":      map[string]any{"head_sha": "abc123"},
		},
	})
	if err != nil {
		t.Fatalf("ResolveStep: %v", err)
	}
	if got := resolved.With["working_dir"]; got != "/tmp/app-pr-42" {
		t.Fatalf("working_dir = %#v", got)
	}
	wantPrompt := "Review PR 42 in acme/app.\nMetadata: {\"head_sha\":\"abc123\"}\n"
	if got := resolved.With["prompt"]; got != wantPrompt {
		t.Fatalf("prompt = %q, want %q", got, wantPrompt)
	}
}
