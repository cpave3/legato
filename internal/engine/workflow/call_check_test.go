package workflow_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

func callingFile(name, calls string) string {
	return fmt.Sprintf(`schema_version: 2
name: %s
title: %s
steps:
  - id: call
    type: workflow.run
    with: {workflow: %s}
`, name, name, calls)
}

func mustParse(t *testing.T, text string) *workflow.Definition {
	t.Helper()
	definition, err := workflow.Parse("x.yaml", []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

func diagnosticsNaming(diagnostics []workflow.Diagnostic, text string) []workflow.Diagnostic {
	var found []workflow.Diagnostic
	for _, d := range diagnostics {
		if strings.Contains(d.Message, text) {
			found = append(found, d)
		}
	}
	return found
}

// Recursion is found across the discovered files: a calls b, b calls a.
func TestACallThatLeadsBackIsAnError(t *testing.T) {
	a := callingFile("a", "b")
	others := []*workflow.Definition{mustParse(t, callingFile("b", "a")), mustParse(t, a)}
	diagnostics := workflow.CallDiagnostics([]byte(a), mustParse(t, a), others)
	found := diagnosticsNaming(diagnostics, "a → b → a")
	if len(found) != 1 || found[0].Severity != workflow.SeverityError || found[0].Path != "steps[0].with.workflow" || found[0].Line != 7 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	self := callingFile("s", "s")
	if found := diagnosticsNaming(workflow.CallDiagnostics([]byte(self), mustParse(t, self), nil), "s → s"); len(found) != 1 {
		t.Fatal("a workflow that calls itself is not refused")
	}
}

func TestCallsNestingDeeperThanFiveAreAnError(t *testing.T) {
	var others []*workflow.Definition
	names := []string{"w0", "w1", "w2", "w3", "w4", "w5", "w6"}
	for i := 1; i < len(names)-1; i++ {
		others = append(others, mustParse(t, callingFile(names[i], names[i+1])))
	}
	others = append(others, mustParse(t, "schema_version: 2\nname: w6\ntitle: W6\nsteps:\n  - id: s\n    type: task.context\n    with: {task: \"${{ workflow.task }}\"}\n"))
	top := callingFile("w0", "w1")
	if found := diagnosticsNaming(workflow.CallDiagnostics([]byte(top), mustParse(t, top), others), "deeper than 5"); len(found) != 1 || found[0].Severity != workflow.SeverityError {
		t.Fatal("a chain of six calls is not refused")
	}
	five := callingFile("w1", "w2")
	if found := diagnosticsNaming(workflow.CallDiagnostics([]byte(five), mustParse(t, five), others), "deeper"); len(found) != 0 {
		t.Fatalf("a chain of five calls is refused: %+v", found)
	}
}

// What the caller passes and reads is checked against the child's file,
// as warnings: the child may change.
func TestACallIsCheckedAgainstTheChildsInputsAndOutputs(t *testing.T) {
	child := mustParse(t, `schema_version: 2
name: child
title: Child
inputs:
  milestone: {type: string, required: true}
  base: {type: string}
outputs:
  top_branch: {type: string, value: "${{ steps.s.outputs.content }}"}
steps:
  - id: s
    type: task.context
    with: {task: "${{ workflow.task }}"}
`)
	caller := `schema_version: 2
name: caller
title: Caller
inputs:
  pick: {type: string, choices: [child, gone]}
outputs:
  bottom: {type: string, value: "${{ steps.call.outputs.bottom_branch }}"}
steps:
  - id: call
    type: workflow.run
    on_failure: continue
    with:
      workflow: "${{ workflow.inputs.pick }}"
      inputs: {nope: x, base: y}
  - id: note
    type: task.note
    with: {task: "${{ workflow.task }}", note: "${{ steps.call.outputs.top_branch }} ${{ steps.call.outputs.run_id }} ${{ steps.call.outputs.error_code }} ${{ steps.call.outputs.prs }}"}
`
	diagnostics := workflow.CallDiagnostics([]byte(caller), mustParse(t, caller), []*workflow.Definition{child})
	for _, want := range []string{`"gone"`, `input "nope"`, `"milestone"`, `"prs"`, `"bottom_branch"`} {
		found := diagnosticsNaming(diagnostics, want)
		if len(found) != 1 || found[0].Severity != workflow.SeverityWarning {
			t.Errorf("no single warning naming %s in %+v", want, diagnostics)
		}
	}
	for _, unwanted := range []string{`"top_branch"`, `"run_id"`, `"error_code"`, `"base"`} {
		if found := diagnosticsNaming(diagnostics, unwanted); len(found) != 0 {
			t.Errorf("warned of %s: %+v", unwanted, found)
		}
	}
}
