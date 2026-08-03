package workflow_test

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

func TestRunTitleRejectsReferencesOutsideDeclaredInputs(t *testing.T) {
	for name, title := range map[string]string{
		"unknown input": "${{ workflow.inputs.missing }}",
		"step output":   "${{ steps.prepare.outputs.number }}",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := workflow.Parse("test.yaml", []byte(`
schema_version: 1
name: test
title: Test
run_title: "`+title+`"
inputs:
  task: {type: task, required: true}
steps:
  - id: prepare
    title: Prepare
    type: agent.run
    with: {prompt: test}
    outputs:
      number: {type: integer}
`))
			if err == nil {
				t.Fatal("Parse succeeded")
			}
			if !strings.Contains(err.Error(), "run_title") {
				t.Fatalf("error = %q", err)
			}
		})
	}
}

func TestResolveRunTitleRejectsEmptyResult(t *testing.T) {
	definition, err := workflow.Parse("test.yaml", []byte(`
schema_version: 1
name: test
title: Test
run_title: "${{ workflow.inputs.label }}"
inputs:
  label: {type: string, required: true}
steps:
  - id: run
    title: Run
    type: agent.run
    with: {prompt: test}
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := definition.ResolveRunTitle(map[string]any{"label": "   "}); err == nil {
		t.Fatal("ResolveRunTitle succeeded")
	}
}

func TestResolveRunTitleFallsBackToDefinitionTitle(t *testing.T) {
	definition := workflow.Definition{Title: "Reusable title"}
	title, err := definition.ResolveRunTitle(nil)
	if err != nil {
		t.Fatal(err)
	}
	if title != definition.Title {
		t.Fatalf("title = %q", title)
	}
}
