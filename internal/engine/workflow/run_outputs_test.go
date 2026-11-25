package workflow_test

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

// A part of LEG-480's example A: a workflow's outputs read its steps'.
const outputsFile = `schema_version: 2
name: build-milestone
title: Build a milestone
inputs:
  milestone: {type: string, required: true}
outputs:
  top_branch: {type: string, value: "${{ steps.push.outputs.top_branch }}", description: The top of the stack}
  prs:        {type: json,   value: "${{ steps.push.outputs.prs }}"}
  review:     {type: string, value: "${{ steps.review.outputs.outcome ?? 'not reviewed' }}"}
  count:      {type: integer, value: "${{ steps.push.outputs.count }}"}
  label:      {type: string, value: "Built ${{ workflow.inputs.milestone }}"}
steps:
  - id: push
    type: agent.run
    with: {prompt: Push it}
    outputs:
      top_branch: {type: string}
      prs: {type: json}
      count: {type: integer}
  - id: review
    type: github.pr.review_passes
    with: {repo: o/r, pr_number: 1}
`

func TestAWorkflowsOutputsResolveFromItsSteps(t *testing.T) {
	definition, err := workflow.Parse("a.yaml", []byte(outputsFile))
	if err != nil {
		t.Fatal(err)
	}
	if got := definition.Outputs["top_branch"]; got.Type != "string" || got.Description != "The top of the stack" {
		t.Fatalf("top_branch = %+v", got)
	}
	steps := map[string]map[string]any{"push": {"top_branch": "legato/m-3", "prs": []any{map[string]any{"number": 7}}, "count": int64(3)}}
	outputs, warnings := definition.ResolveOutputs(map[string]any{"milestone": "M-1"}, nil, steps)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if outputs["top_branch"] != "legato/m-3" || outputs["review"] != "not reviewed" || outputs["label"] != "Built M-1" || outputs["count"] != int64(3) {
		t.Fatalf("outputs = %#v", outputs)
	}
	if prs, ok := outputs["prs"].([]any); !ok || len(prs) != 1 {
		t.Fatalf("prs = %#v", outputs["prs"])
	}
}

// An output that can't be resolved is left out, and named in a warning.
func TestAnOutputThatCannotBeResolvedIsAWarning(t *testing.T) {
	definition, err := workflow.Parse("a.yaml", []byte(outputsFile))
	if err != nil {
		t.Fatal(err)
	}
	// push was skipped: its outputs are missing; count came back as text.
	outputs, warnings := definition.ResolveOutputs(map[string]any{"milestone": "M-1"}, nil, map[string]map[string]any{"push": {"count": "three"}})
	if _, ok := outputs["top_branch"]; ok {
		t.Fatalf("top_branch resolved: %#v", outputs)
	}
	if _, ok := outputs["count"]; ok {
		t.Fatalf("count resolved: %#v", outputs)
	}
	joined := strings.Join(warnings, "\n")
	for _, want := range []string{"top_branch", "prs", "count"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("warnings %q do not name %s", joined, want)
		}
	}
	if strings.Contains(joined, "review") || outputs["review"] != "not reviewed" {
		t.Fatalf("review: %#v, warnings %q", outputs["review"], joined)
	}
}

func TestWorkflowOutputsAreChecked(t *testing.T) {
	cases := map[string]struct{ outputs, want string }{
		"no value":         {`x: {type: string}`, "value"},
		"bad type":         {`x: {type: file, value: "${{ steps.push.outputs.top_branch }}"}`, "type"},
		"unknown step":     {`x: {type: string, value: "${{ steps.nope.outputs.top_branch }}"}`, "nope"},
		"unknown output":   {`x: {type: string, value: "${{ steps.push.outputs.nope }}"}`, "nope"},
		"undeclared input": {`x: {type: string, value: "${{ workflow.inputs.nope }}"}`, "nope"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			file := "schema_version: 2\nname: a\ntitle: A\noutputs:\n  " + tc.outputs + "\nsteps:\n  - id: push\n    type: agent.run\n    with: {prompt: p}\n    outputs: {top_branch: {type: string}}\n"
			_, err := workflow.Parse("a.yaml", []byte(file))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one naming %q", err, tc.want)
			}
			if path := workflow.ErrorPath(err); !strings.HasPrefix(path, "outputs.x") {
				t.Fatalf("path = %q", path)
			}
		})
	}
}

func TestOutputsAreASchemaVersion2Key(t *testing.T) {
	file := "schema_version: 1\nname: a\ntitle: A\noutputs:\n  x: {type: string, value: hi}\nsteps:\n  - id: s\n    type: task.context\n    with: {task: \"${{ workflow.task }}\"}\n"
	if _, err := workflow.Parse("a.yaml", []byte(file)); err == nil || !strings.Contains(err.Error(), "schema_version 2") {
		t.Fatalf("err = %v", err)
	}
}
