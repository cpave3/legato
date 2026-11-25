package workflow_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

// The review-loop shape of spec LEG-559: if: on step outputs and inputs.
const controlFile = `schema_version: 2
name: control
title: Control
inputs:
  ready: {type: boolean}
  base: {type: string}
  limit: {type: integer}
steps:
  - id: pass
    type: task.context
    with: {task: "${{ workflow.task }}"}
    on_failure: continue
    outputs:
      verdict: {type: string}
      found: {type: json}
  - id: confirm
    if: {field: steps.pass.outputs.verdict, equals: clean}
    type: task.context
    with: {task: "${{ workflow.task }}"}
  - id: woke
    if: {field: steps.pass.outputs.verdict, in: [pushed, disputed]}
    type: task.context
    with: {task: "${{ workflow.task }}"}
  - id: ready
    if: {all: [{field: workflow.inputs.ready, equals: true}, {field: steps.pass.outputs.found.last.state, equals: approved}]}
    type: task.context
    with: {task: "${{ workflow.task }}"}
  - id: failed
    if: {field: steps.pass.outputs.status, equals: failed}
    type: task.context
    with: {task: "${{ workflow.task }}"}
  - id: many
    if: {field: "steps.pass.outputs.found.items[-1].n", gt: 2}
    type: task.context
    with:
      task: "${{ workflow.task }}"
      base: "${{ workflow.inputs.base ?? steps.pass.outputs.verdict }}"
      limit: "${{ workflow.inputs.limit }}"
      note: "Base ${{ workflow.inputs.base ?? 'main' }}, first ${{ steps.pass.outputs.found.items[0].n }}"
`

func parseControl(t *testing.T) *workflow.Definition {
	t.Helper()
	definition, err := workflow.Parse("control.yaml", []byte(controlFile))
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

func decodeJSON(t *testing.T, text string) map[string]any {
	t.Helper()
	var out map[string]any
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	if err := decoder.Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestIfReadsStepOutputsAndInputs(t *testing.T) {
	definition := parseControl(t)
	pass := decodeJSON(t, `{"verdict": "pushed", "found": {"last": {"state": "approved"}, "items": [{"n": 1}, {"n": 3.0}]}}`)
	outputs := map[string]map[string]any{"pass": pass}
	cases := []struct {
		step   string
		inputs map[string]any
		want   bool
	}{
		{"confirm", nil, false},
		// in: on a step output.
		{"woke", nil, true},
		// all: mixing workflow.inputs.* with step outputs, and equals:
		// true on a boolean input.
		{"ready", map[string]any{"ready": true}, true},
		{"ready", map[string]any{"ready": false}, false},
		{"ready", nil, false},
		// status is missing while the step succeeds.
		{"failed", nil, false},
		// numbers compare by value: 3.0 after JSON is above 2.
		{"many", nil, true},
	}
	for _, tc := range cases {
		got, err := definition.StepRuns(tc.step, tc.inputs, nil, outputs)
		if err != nil || got != tc.want {
			t.Errorf("%s with %v: runs = %v, %v; want %v", tc.step, tc.inputs, got, err, tc.want)
		}
	}
}

func TestIfOnASkippedStepsOutputsIsFalse(t *testing.T) {
	definition := parseControl(t)
	// pass was skipped: its outputs are missing.
	for _, step := range []string{"confirm", "woke", "ready", "failed", "many"} {
		got, err := definition.StepRuns(step, map[string]any{"ready": true}, nil, map[string]map[string]any{})
		if err != nil || got {
			t.Errorf("%s: runs = %v, %v; want false", step, got, err)
		}
	}
	failed := map[string]map[string]any{"pass": {"status": "failed", "error": "boom", "error_code": "review_loop_active"}}
	if got, err := definition.StepRuns("failed", nil, nil, failed); err != nil || !got {
		t.Errorf("failed after a continued failure: runs = %v, %v", got, err)
	}
}

func TestFallbacksAndIndexingResolve(t *testing.T) {
	definition := parseControl(t)
	outputs := map[string]map[string]any{"pass": decodeJSON(t, `{"verdict": "clean", "found": {"items": [{"n": 1}, {"n": 3}]}}`)}
	resolved, err := definition.ResolveStep("many", nil, map[string]any{"id": "t"}, outputs)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.With["base"] != "clean" {
		t.Errorf("base = %v, want the step's verdict", resolved.With["base"])
	}
	if _, ok := resolved.With["limit"]; ok {
		t.Errorf("limit = %v: a whole value that is missing leaves the field out", resolved.With["limit"])
	}
	if resolved.With["note"] != "Base main, first 1" {
		t.Errorf("note = %q", resolved.With["note"])
	}
	// The empty string counts as a value; null does not.
	resolved, err = definition.ResolveStep("many", map[string]any{"base": "", "limit": int64(4)}, map[string]any{"id": "t"}, outputs)
	if err != nil || resolved.With["base"] != "" || resolved.With["limit"] != int64(4) {
		t.Fatalf("with = %v, %v", resolved.With, err)
	}
	resolved, err = definition.ResolveStep("many", map[string]any{"base": nil}, map[string]any{"id": "t"}, outputs)
	if err != nil || resolved.With["base"] != "clean" {
		t.Fatalf("base = %v, %v: null falls back", resolved.With["base"], err)
	}
}

func TestMissingValueWithinTextFailsTheStep(t *testing.T) {
	definition := parseControl(t)
	_, err := definition.ResolveStep("many", nil, map[string]any{"id": "t"}, map[string]map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "??") {
		t.Fatalf("err = %v, want one that suggests ??", err)
	}
}

func TestConditionOperators(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name      string
		condition workflow.Condition
		task      map[string]any
		want      bool
	}{
		{"gt missing", workflow.Condition{Field: "task.metadata.n", GT: 1}, map[string]any{}, false},
		{"lt missing", workflow.Condition{Field: "task.metadata.n", LT: 1}, map[string]any{}, false},
		{"empty missing", workflow.Condition{Field: "task.metadata.n", Empty: &yes}, map[string]any{}, true},
		{"exists missing", workflow.Condition{Field: "task.metadata.n", Exists: &yes}, map[string]any{}, false},
		{"gt by value", workflow.Condition{Field: "task.metadata.n", GT: 2}, map[string]any{"metadata": map[string]any{"n": json.Number("2.5")}}, true},
		{"lt by value", workflow.Condition{Field: "task.metadata.n", LT: 3}, map[string]any{"metadata": map[string]any{"n": int64(3)}}, false},
		{"equals 3 and 3.0", workflow.Condition{Field: "task.metadata.n", Equals: 3}, map[string]any{"metadata": map[string]any{"n": float64(3)}}, true},
		{"in by value", workflow.Condition{Field: "task.metadata.n", In: []any{1, 2}}, map[string]any{"metadata": map[string]any{"n": json.Number("2.0")}}, true},
		{"gt on text", workflow.Condition{Field: "task.metadata.at", GT: "2026-01-01"}, map[string]any{"metadata": map[string]any{"at": "2026-02-01"}}, true},
		{"gt number against text", workflow.Condition{Field: "task.metadata.n", GT: 1}, map[string]any{"metadata": map[string]any{"n": "5"}}, false},
		{"empty list", workflow.Condition{Field: "task.metadata.tags", Empty: &yes}, map[string]any{"metadata": map[string]any{"tags": []any{}}}, true},
		{"empty text", workflow.Condition{Field: "task.metadata.tags", Empty: &yes}, map[string]any{"metadata": map[string]any{"tags": ""}}, true},
		{"not empty", workflow.Condition{Field: "task.metadata.tags", Empty: &no}, map[string]any{"metadata": map[string]any{"tags": []any{"a"}}}, true},
	}
	for _, tc := range cases {
		trigger := workflow.TaskEnteredColumnTrigger{Column: "Doing", Conditions: &tc.condition}
		if err := trigger.Validate(nil); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		got, err := trigger.Matches(tc.task)
		if err != nil || got != tc.want {
			t.Errorf("%s: matches = %v, %v; want %v", tc.name, got, err, tc.want)
		}
	}
}

func TestControlKeysAreChecked(t *testing.T) {
	cases := []struct {
		name, file, want string
	}{
		{"if in a v1 file", "schema_version: 1\nname: c\ntitle: C\nsteps:\n  - {id: a, type: task.context, with: {task: \"${{ workflow.task }}\"}, if: {field: workflow.inputs.x, exists: true}}\n", "if is a schema_version 2 key"},
		{"on_failure in a v1 file", "schema_version: 1\nname: c\ntitle: C\nsteps:\n  - {id: a, type: task.context, with: {task: \"${{ workflow.task }}\"}, on_failure: continue}\n", "on_failure is a schema_version 2 key"},
		{"unknown on_failure", "schema_version: 2\nname: c\ntitle: C\nsteps:\n  - {id: a, type: task.context, with: {task: \"${{ workflow.task }}\"}, on_failure: retry}\n", "unsupported on_failure"},
		{"if on a later step", "schema_version: 2\nname: c\ntitle: C\nsteps:\n  - {id: a, type: task.context, with: {task: \"${{ workflow.task }}\"}, if: {field: steps.b.outputs.content, exists: true}}\n  - {id: b, type: task.context, with: {task: \"${{ workflow.task }}\"}}\n", "not an earlier step"},
		{"if with two operators", "schema_version: 2\nname: c\ntitle: C\ninputs: {n: {type: integer}}\nsteps:\n  - {id: a, type: task.context, with: {task: \"${{ workflow.task }}\"}, if: {field: workflow.inputs.n, gt: 1, lt: 3}}\n", "exactly one operator"},
		{"gt a list", "schema_version: 2\nname: c\ntitle: C\ninputs: {n: {type: integer}}\nsteps:\n  - {id: a, type: task.context, with: {task: \"${{ workflow.task }}\"}, if: {field: workflow.inputs.n, gt: [1]}}\n", "gt takes a number or text"},
		{"?? of two types", "schema_version: 2\nname: c\ntitle: C\ninputs: {n: {type: integer}}\nsteps:\n  - {id: a, type: task.note, with: {task: \"${{ workflow.task }}\", note: \"${{ workflow.inputs.n ?? 'none' }}\"}}\n", "same type"},
		{"gate options", "schema_version: 2\nname: c\ntitle: C\nsteps:\n  - {id: a, type: gate, with: {title: Proceed, options: [Yes, yes]}}\n", "different, non-empty names"},
		{"gate body twice", "schema_version: 2\nname: c\ntitle: C\nsteps:\n  - {id: a, type: gate, with: {title: Proceed, body: a, body_path: /b}}\n", "not both"},
	}
	for _, tc := range cases {
		_, err := workflow.Parse(tc.name, []byte(tc.file))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
	if _, err := workflow.Parse("gate", []byte("schema_version: 2\nname: c\ntitle: C\nsteps:\n  - {id: a, type: gate, with: {title: Proceed}, on_failure: pause}\n  - {id: b, if: {field: steps.a.outputs.choice, equals: Continue}, type: task.context, with: {task: \"${{ workflow.task }}\"}}\n")); err != nil {
		t.Fatalf("a gate and an if on its choice: %v", err)
	}
}
