package workflow_test

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

const checkBase = `schema_version: 1
name: check
title: Check
inputs:
  task: {type: task, required: true}
`

func TestCheckAnchorsEachProblem(t *testing.T) {
	cases := []struct {
		name, yaml, path string
		line, column     int
		message          string
	}{
		{"bad step type", checkBase + `steps:
  - id: a
    title: A
    type: agent.nope
`, "steps[0].type", 9, 11, `step "a" has unsupported type "agent.nope"`},
		{"missing required with key", checkBase + `steps:
  - id: a
    title: A
    type: task.move
    with:
      task: ${{ workflow.inputs.task }}
`, "steps[0].with", 10, 5, `step "a": with requires column`},
		{"undeclared input", checkBase + `steps:
  - id: a
    title: A
    type: agent.run
    with:
      prompt: Look at ${{ workflow.inputs.nope }}
`, "steps[0].with.prompt", 11, 15, `step "a" field "prompt": input "nope" is not declared`},
		{"forward output reference", checkBase + `steps:
  - id: a
    title: A
    type: agent.run
    with:
      prompt: ${{ steps.b.outputs.x }}
  - id: b
    title: B
    type: agent.run
    with: {prompt: hi}
    outputs: {x: {type: string}}
`, "steps[0].with.prompt", 11, 15, `step "a" field "prompt": step "b" is not an earlier step`},
		{"bad cron", checkBase + `on:
  schedule:
    cron: "61 * * * *"
steps:
  - id: a
    title: A
    type: agent.run
    with: {prompt: hi}
`, "on.schedule.cron", 8, 11, "schedule: "},
		{"duplicate step ID", checkBase + `steps:
  - id: a
    title: A
    type: agent.run
    with: {prompt: hi}
  - id: a
    title: A again
    type: agent.run
    with: {prompt: hi}
`, "steps[1].id", 11, 9, "step IDs must be non-empty and unique"},
		{"unknown top-level key", checkBase + `colour: blue
steps:
  - id: a
    title: A
    type: agent.run
    with: {prompt: hi}
`, "colour", 6, 1, "field colour not found"},
		// yaml.v3 reports a syntax error on the line its context starts.
		{"syntax error", "schema_version: 1\nname: [oops\n", "", 1, 1, "did not find expected"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			definition, diagnostics := workflow.Check("check.yaml", []byte(tc.yaml))
			if definition != nil {
				t.Fatal("Check returned a definition for a file with an error")
			}
			if len(diagnostics) != 1 {
				t.Fatalf("diagnostics = %+v, want one", diagnostics)
			}
			d := diagnostics[0]
			if d.Severity != workflow.SeverityError || d.Path != tc.path || d.Line != tc.line || d.Column != tc.column || !strings.Contains(d.Message, tc.message) {
				t.Fatalf("diagnostic = %+v, want path %q at %d:%d with %q", d, tc.path, tc.line, tc.column, tc.message)
			}
			if d.EndLine < d.Line || (d.EndLine == d.Line && d.EndColumn <= d.Column) {
				t.Fatalf("diagnostic ends at %d:%d, before it starts", d.EndLine, d.EndColumn)
			}
		})
	}
}

func TestCheckReportsEachStepsProblem(t *testing.T) {
	_, diagnostics := workflow.Check("check.yaml", []byte(checkBase+`steps:
  - id: a
    title: A
    type: agent.nope
  - id: b
    title: B
    type: task.move
    with: {task: "${{ workflow.inputs.task }}"}
`))
	if len(diagnostics) != 2 {
		t.Fatalf("diagnostics = %+v, want two", diagnostics)
	}
	if diagnostics[0].Path != "steps[0].type" || diagnostics[1].Path != "steps[1].with" {
		t.Fatalf("paths = %q, %q", diagnostics[0].Path, diagnostics[1].Path)
	}
}

func TestCheckSpansAScalarValue(t *testing.T) {
	_, diagnostics := workflow.Check("check.yaml", []byte(checkBase+`steps:
  - id: a
    title: A
    type: agent.nope
`))
	d := diagnostics[0]
	if d.EndLine != 9 || d.EndColumn != 11+len("agent.nope") {
		t.Fatalf("span = %d:%d-%d:%d, want the value", d.Line, d.Column, d.EndLine, d.EndColumn)
	}
}

func TestCheckAcceptsAValidFile(t *testing.T) {
	definition, diagnostics := workflow.Check("check.yaml", []byte(checkBase+`steps:
  - id: a
    title: A
    type: agent.run
    with: {prompt: hi}
`))
	if definition == nil || len(diagnostics) != 0 {
		t.Fatalf("Check = %v, %+v", definition, diagnostics)
	}
	if definition.Source != "check.yaml" || definition.Steps[0].Trigger != "automatic" {
		t.Fatalf("definition = %+v", definition)
	}
}

func TestCheckAnEmptyFile(t *testing.T) {
	_, diagnostics := workflow.Check("check.yaml", nil)
	if len(diagnostics) != 1 || diagnostics[0].Line != 1 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
}

// Parse keeps its first error, word for word, with the path beside it.
func TestParseKeepsItsFirstError(t *testing.T) {
	_, err := workflow.Parse("check.yaml", []byte(checkBase+`steps:
  - id: a
    title: A
    type: agent.nope
  - id: b
    title: B
    type: task.move
    with: {task: "${{ workflow.inputs.task }}"}
`))
	if err == nil || err.Error() != `validate workflow check.yaml: step "a" has unsupported type "agent.nope"` {
		t.Fatalf("err = %v", err)
	}
	if path := workflow.ErrorPath(err); path != "steps[0].type" {
		t.Fatalf("ErrorPath = %q", path)
	}
}
