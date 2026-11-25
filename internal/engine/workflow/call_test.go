package workflow_test

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

// LEG-480's example C, in part: a workflow.run step, and later steps that
// read the child's outputs, status and run_id.
const callerFile = `schema_version: 2
name: proposal-project
title: Build every milestone
inputs:
  milestone: {type: string, required: true}
  repo: {type: repository, required: true}
  base: {type: string}
  builder: {type: string, default: build-milestone, choices: [build-milestone, build-milestone-by-ticket]}
outputs:
  top: {type: string, value: "${{ steps.build.outputs.top_branch }}"}
steps:
  - id: build
    type: workflow.run
    on_failure: continue
    with:
      workflow: "${{ workflow.inputs.builder }}"
      inputs:
        milestone: "${{ workflow.inputs.milestone }}"
        repo: "${{ workflow.inputs.repo }}"
        base: "${{ workflow.inputs.base }}"
  - id: again
    type: workflow.run
    if: {field: steps.build.outputs.error_code, equals: review_loop_active}
    with:
      workflow: build-milestone
      inputs: {milestone: "${{ steps.build.outputs.run_id }}", base: "${{ steps.build.outputs.top_branch ?? 'main' }}"}
  - id: note
    type: task.note
    with: {task: "${{ workflow.task }}", note: "${{ steps.again.outputs.status }} ${{ steps.again.outputs.prs[0].url }}"}
`

func TestAWorkflowRunStepsOutputsAreTheChildsAndItsStatus(t *testing.T) {
	definition, err := workflow.Parse("c.yaml", []byte(callerFile))
	if err != nil {
		t.Fatal(err)
	}
	spec, ok := workflow.LookupExecutor(workflow.WorkflowRunStepType)
	if !ok {
		t.Fatal("workflow.run is not in the catalog")
	}
	for _, name := range []string{"status", "run_id"} {
		if _, ok := spec.Output(name); !ok {
			t.Fatalf("workflow.run has no %s output", name)
		}
	}
	if definition.Steps[0].Type != "workflow.run" {
		t.Fatalf("steps = %+v", definition.Steps)
	}
}

func TestTheWorkflowAStepRunsIsALiteralOrAnInputWithChoices(t *testing.T) {
	cases := map[string]struct {
		workflow string
		want     string
	}{
		"a literal":                {`build-milestone`, ""},
		"an input with choices":    {`"${{ workflow.inputs.builder }}"`, ""},
		"an input without choices": {`"${{ workflow.inputs.free }}"`, "choices"},
		"a step's output":          {`"${{ steps.first.outputs.content }}"`, "choices"},
		"within text":              {`"build-${{ workflow.inputs.builder }}"`, "choices"},
		"a fallback":               {`"${{ workflow.inputs.builder ?? 'x' }}"`, "choices"},
		"empty":                    {`""`, "workflow"},
		"not text":                 {`[a]`, "workflow"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			file := `schema_version: 2
name: caller
title: Caller
inputs:
  builder: {type: string, choices: [a, b]}
  free: {type: string}
steps:
  - id: first
    type: task.context
    with: {task: "${{ workflow.task }}"}
  - id: call
    type: workflow.run
    with:
      workflow: ` + tc.workflow + `
`
			_, err := workflow.Parse("caller.yaml", []byte(file))
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one naming %q", err, tc.want)
			}
			if path := workflow.ErrorPath(err); path != "steps[1].with.workflow" {
				t.Fatalf("path = %q", path)
			}
		})
	}
}

func TestAWorkflowRunStepsInputsAreAMap(t *testing.T) {
	file := `schema_version: 2
name: caller
title: Caller
steps:
  - id: call
    type: workflow.run
    with:
      workflow: child
      inputs: "${{ workflow.task }}"
`
	_, err := workflow.Parse("caller.yaml", []byte(file))
	if err == nil || workflow.ErrorPath(err) != "steps[0].with.inputs" {
		t.Fatalf("err = %v at %q", err, workflow.ErrorPath(err))
	}
}

func TestStepsMayNotReadAWorkflowRunStepsOutputsBeforeItRuns(t *testing.T) {
	file := `schema_version: 2
name: caller
title: Caller
steps:
  - id: note
    type: task.note
    with: {task: "${{ workflow.task }}", note: "${{ steps.call.outputs.top_branch }}"}
  - id: call
    type: workflow.run
    with: {workflow: child}
`
	if _, err := workflow.Parse("caller.yaml", []byte(file)); err == nil || !strings.Contains(err.Error(), "earlier step") {
		t.Fatalf("err = %v", err)
	}
}

const childInputsFile = `schema_version: 2
name: child
title: Child
inputs:
  milestone: {type: string, required: true}
  repo: {type: repository, required: true}
  base: {type: string, default: main}
  passes: {type: integer}
  draft: {type: boolean}
  extra: {type: json}
  card: {type: task}
steps:
  - id: s
    type: task.context
    with: {task: "${{ workflow.task }}"}
`

// A caller passes typed values, never text: a repository as the object it
// resolved to, numbers and booleans as themselves.
func TestTypedInputsAreCheckedAgainstTheChildsInputs(t *testing.T) {
	definition, err := workflow.Parse("child.yaml", []byte(childInputsFile))
	if err != nil {
		t.Fatal(err)
	}
	repo := map[string]any{"name": "", "repo": "o/custom", "primary_dir": "/src/custom", "base_branch": "trunk"}
	got, err := definition.NormalizeValues(map[string]any{"milestone": "M-1", "repo": repo, "passes": float64(3), "draft": true, "extra": []any{1.0}, "card": map[string]any{"id": "t1", "title": "T"}})
	if err != nil {
		t.Fatal(err)
	}
	if got["repo"].(map[string]any)["repo"] != "o/custom" || got["base"] != "main" || got["passes"] != int64(3) || got["draft"] != true || got["card"] != "t1" {
		t.Fatalf("inputs = %#v", got)
	}
	for name, raw := range map[string]map[string]any{
		"missing required": {"repo": "preset"},
		"unknown":          {"milestone": "M", "repo": "preset", "nope": 1},
		"wrong type":       {"milestone": 3, "repo": "preset"},
		"not an integer":   {"milestone": "M", "repo": "preset", "passes": 2.5},
		"bad repository":   {"milestone": "M", "repo": map[string]any{"repo": "o/r"}},
	} {
		if _, err := definition.NormalizeValues(raw); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	// A repository named by its preset is resolved by the run, as a form's.
	if got, err := definition.NormalizeValues(map[string]any{"milestone": "M", "repo": "preset"}); err != nil || got["repo"] != "preset" {
		t.Fatalf("preset: %#v, %v", got, err)
	}
}
