package workflow_test

import (
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

func TestValidateAgentInterviewStep(t *testing.T) {
	_, err := workflow.Parse("interview.yaml", []byte(`
schema_version: 1
name: interview
title: Interview
inputs:
  task: {type: task, required: true}
steps:
  - id: reflect
    title: Reflect
    type: agent.interview
    with:
      task: ${{ workflow.inputs.task }}
    outputs:
      available: {type: boolean}
      reason: {type: string}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
}

func TestResolveManagedArtifactPathForLaterStep(t *testing.T) {
	definition, err := workflow.Parse("task.yaml", []byte(`
schema_version: 1
name: triage
title: Triage
inputs:
  task: {type: task, required: true}
steps:
  - id: triage
    title: Triage
    type: agent.run
    with: {prompt: inspect}
    outputs:
      report: {type: file, path: triage.md}
  - id: note
    title: Note
    type: task.note
    with:
      task: ${{ workflow.inputs.task }}
      body_path: ${{ steps.triage.outputs.report.path }}
`))
	if err != nil {
		t.Fatal(err)
	}
	step, err := definition.ResolveStep("note", map[string]any{"task": map[string]any{"id": "LEG-1"}}, map[string]map[string]any{"triage": {"report": map[string]any{"path": "/managed/triage.md", "artifact_id": "a1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if step.With["body_path"] != "/managed/triage.md" {
		t.Fatalf("body_path = %#v", step.With["body_path"])
	}
}

func TestResolveStepResolvesFileOutputPathFromTaskInput(t *testing.T) {
	definition, err := workflow.Parse("triage.yaml", []byte(`
schema_version: 1
name: triage
title: Triage
inputs:
  task: {type: task, required: true}
steps:
  - id: inspect
    title: Inspect
    type: agent.run
    with:
      prompt: Write report to ${{ outputs.report.path }}
    outputs:
      report: {type: file, path: "task-triage-${{ workflow.inputs.task.id }}.md"}
`))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := definition.ResolveStep("inspect", map[string]any{"task": map[string]any{"id": "LEG-42"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := resolved.Outputs["report"].Path; got != "task-triage-LEG-42.md" {
		t.Fatalf("output path = %q", got)
	}
	if got := resolved.With["prompt"]; got != "Write report to task-triage-LEG-42.md" {
		t.Fatalf("prompt = %#v", got)
	}
}

func TestResolveStepRejectsUnsafeResolvedFileOutputPath(t *testing.T) {
	definition, err := workflow.Parse("triage.yaml", []byte(`
schema_version: 1
name: triage
title: Triage
inputs:
  task: {type: task, required: true}
steps:
  - id: inspect
    title: Inspect
    type: agent.run
    with: {prompt: inspect}
    outputs:
      report: {type: file, path: "task-triage-${{ workflow.inputs.task.id }}.md"}
`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = definition.ResolveStep("inspect", map[string]any{"task": map[string]any{"id": "x/../../outside"}}, nil)
	if err == nil {
		t.Fatal("expected unsafe resolved path to fail")
	}
}

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
