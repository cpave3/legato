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
		{"a trigger whose kind has dots", checkBase + `on:
  task.entered_column:
    column: ''
steps:
  - id: a
    title: A
    type: agent.run
    with: {prompt: hi}
`, "on.task.entered_column.column", 8, 13, "task.entered_column column is required"},
		{"a pull request trigger", checkBase + `on:
  pr.changes_requested:
    max_runs_per_day: -1
steps:
  - id: a
    title: A
    type: agent.run
    with: {prompt: hi}
`, "on.pr.changes_requested.max_runs_per_day", 8, 23, "max_runs_per_day must be 1 or more"},
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
    outputs: {summary: {type: string}}
`))
	if definition == nil || len(diagnostics) != 0 {
		t.Fatalf("Check = %v, %+v", definition, diagnostics)
	}
	if definition.Source != "check.yaml" || definition.Steps[0].Start != "automatic" {
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

// An unknown with key is a warning (LEG-461): the file still loads.
func TestCheckWarnsOfAnUnknownWithKey(t *testing.T) {
	definition, diagnostics := workflow.Check("check.yaml", []byte(checkBase+`steps:
  - id: a
    title: A
    type: agent.run
    with:
      prompt: hi
      promt: hi
    outputs: {summary: {type: string}}
`))
	if definition == nil {
		t.Fatal("a warning stopped the file loading")
	}
	if len(diagnostics) != 1 || diagnostics[0].Severity != workflow.SeverityWarning || diagnostics[0].Path != "steps[0].with.promt" || diagnostics[0].Line != 12 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
}

// An agent.run step with no outputs loses what its agent found when the
// step completes and the agent is removed (LEG-532): a warning, so the file
// still loads, unless keep_agent keeps the agent.
func TestCheckWarnsOfAnAgentRunStepWithNoOutputs(t *testing.T) {
	cases := map[string]struct {
		step string
		warn bool
	}{
		"no outputs": {step: `
    with:
      prompt: hi
`, warn: true},
		"outputs declared": {step: `
    with:
      prompt: hi
    outputs:
      digest: {type: string}
`},
		"keep_agent": {step: `
    with:
      prompt: hi
      keep_agent: true
`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			definition, diagnostics := workflow.Check("check.yaml", []byte(checkBase+`steps:
  - id: digest
    title: Digest
    type: agent.run`+c.step))
			if definition == nil {
				t.Fatalf("the file did not load: %+v", diagnostics)
			}
			if !c.warn {
				if len(diagnostics) != 0 {
					t.Fatalf("diagnostics = %+v", diagnostics)
				}
				return
			}
			if len(diagnostics) != 1 || diagnostics[0].Severity != workflow.SeverityWarning || diagnostics[0].Path != "steps[0].type" ||
				!strings.Contains(diagnostics[0].Message, "declares no outputs") || !strings.Contains(diagnostics[0].Message, "keep_agent") {
				t.Fatalf("diagnostics = %+v", diagnostics)
			}
		})
	}
}

// scratch: true says the step needs no folder of its own: it cannot also
// name one (LEG-530).
func TestCheckRefusesScratchWithAFolder(t *testing.T) {
	step := func(with string) string {
		return checkBase + `steps:
  - id: digest
    title: Digest
    type: agent.run
    with:
      prompt: hi
` + with + `    outputs: {digest: {type: string}}
`
	}
	if definition, diagnostics := workflow.Check("check.yaml", []byte(step("      scratch: true\n"))); definition == nil || len(diagnostics) != 0 {
		t.Fatalf("scratch alone: %+v", diagnostics)
	}
	for _, with := range []string{"      scratch: true\n      working_dir: /repo\n", "      scratch: true\n      repo: acme/widget\n"} {
		if definition, diagnostics := workflow.Check("check.yaml", []byte(step(with))); definition != nil || len(diagnostics) == 0 || !strings.Contains(diagnostics[0].Message, "scratch") {
			t.Fatalf("%q: %v, %+v", with, definition != nil, diagnostics)
		}
	}
	if definition, diagnostics := workflow.Check("check.yaml", []byte(step("      scratch: yes please\n"))); definition != nil {
		t.Fatalf("a scratch that is not true or false loaded: %+v", diagnostics)
	}
}
