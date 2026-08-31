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

func TestResolveWorkflowTaskIntrinsic(t *testing.T) {
	definition, err := workflow.Parse("intrinsic.yaml", []byte(`
schema_version: 1
name: intrinsic
title: Intrinsic
steps:
  - id: inspect
    title: Inspect
    type: agent.run
    with:
      task: ${{ workflow.task }}
      id: ${{ workflow.task.id }}
      prompt: "Task ${{ workflow.task.title }} at ${{ workflow.task.working_dir }}: ${{ workflow.task.metadata.repository.name }}"
      nested:
        owner: ${{ workflow.task.metadata.owner }}
`))
	if err != nil {
		t.Fatal(err)
	}
	workflowTask := map[string]any{
		"id": "workflow-1", "title": "Intrinsic", "working_dir": "/tmp/workflow",
		"metadata": map[string]any{"repository": map[string]any{"name": "legato"}, "owner": "platform"},
	}
	resolved, err := definition.ResolveStep("inspect", nil, workflowTask, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := resolved.With["task"].(map[string]any)["id"]; got != "workflow-1" {
		t.Fatalf("task id = %#v", got)
	}
	if got := resolved.With["id"]; got != "workflow-1" {
		t.Fatalf("id = %#v", got)
	}
	if got := resolved.With["prompt"]; got != "Task Intrinsic at /tmp/workflow: legato" {
		t.Fatalf("prompt = %#v", got)
	}
	if got := resolved.With["nested"].(map[string]any)["owner"]; got != "platform" {
		t.Fatalf("nested owner = %#v", got)
	}
}

func TestWorkflowTaskIntrinsicValidationRejectsUnsupportedReferences(t *testing.T) {
	for _, expression := range []string{
		"${{ workflow.tasks }}",
		"${{ workflow.task.unknown }}",
		"${{ workflow.task.title.extra }}",
	} {
		t.Run(expression, func(t *testing.T) {
			_, err := workflow.Parse("invalid.yaml", []byte(`
schema_version: 1
name: invalid
title: Invalid
steps:
  - id: inspect
    title: Inspect
    type: agent.run
    with:
      prompt: "`+expression+`"
`))
			if err == nil {
				t.Fatalf("expected %s to fail validation", expression)
			}
		})
	}
}

func TestTaskMoveRequiresTaskAndNonEmptyColumn(t *testing.T) {
	for name, with := range map[string]string{
		"missing task":   "column: Ready",
		"missing column": "task: ${{ workflow.task }}",
		"empty column":   "task: ${{ workflow.task }}\n      column: ' '",
		"non-string":     "task: ${{ workflow.task }}\n      column: 42",
		"literal task":   "task: subject-1\n      column: Ready",
		"field task":     "task: ${{ workflow.task.id }}\n      column: Ready",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := workflow.Parse("move.yaml", []byte(`
schema_version: 1
name: move
title: Move
steps:
  - id: move
    title: Move
    type: task.move
    with:
      `+with+`
`))
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestWorkflowTaskIntrinsicIsUnavailableInRunTitle(t *testing.T) {
	_, err := workflow.Parse("invalid.yaml", []byte(`
schema_version: 1
name: invalid
title: Invalid
run_title: Run ${{ workflow.task.id }}
steps:
  - id: inspect
    title: Inspect
    type: agent.run
    with: {prompt: inspect}
`))
	if err == nil {
		t.Fatal("expected workflow.task in run_title to fail validation")
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
	step, err := definition.ResolveStep("note", map[string]any{"task": map[string]any{"id": "LEG-1"}}, nil, map[string]map[string]any{"triage": {"report": map[string]any{"path": "/managed/triage.md", "artifact_id": "a1"}}})
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
	resolved, err := definition.ResolveStep("inspect", map[string]any{"task": map[string]any{"id": "LEG-42"}}, nil, nil)
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
	_, err = definition.ResolveStep("inspect", map[string]any{"task": map[string]any{"id": "x/../../outside"}}, nil, nil)
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

	resolved, err := definition.ResolveStep("review", inputs, nil, map[string]map[string]any{
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

func TestChimeraSessionRemoveRequiresWholeTaskReference(t *testing.T) {
	for name, task := range map[string]string{
		"valid input":   "${{ workflow.inputs.task }}",
		"valid subject": "${{ workflow.task }}",
		"missing":       "",
		"literal":       "task-1",
		"field":         "${{ workflow.inputs.task.id }}",
	} {
		t.Run(name, func(t *testing.T) {
			with := ""
			if task != "" {
				with = "with:\n      task: " + task
			}
			_, err := workflow.Parse("remove.yaml", []byte(`
schema_version: 1
name: remove
title: Remove
inputs:
  task: {type: task, required: true}
steps:
  - id: remove
    title: Remove
    type: chimera.session.remove
    `+with+`
`))
			wantValid := name == "valid input" || name == "valid subject"
			if wantValid && err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !wantValid && err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
