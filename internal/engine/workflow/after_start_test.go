package workflow_test

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

func TestDefinitionValidatesAfterStart(t *testing.T) {
	valid := `
schema_version: 1
name: plan
title: Plan
after_start: agent
inputs:
  task: {type: task, required: true}
steps:
  - id: dispatch
    title: Dispatch
    type: agent.dispatch
    with:
      task: ${{ workflow.inputs.task }}
      prompt: plan
      working_dir: /repo
`
	if _, err := workflow.Parse("valid.yaml", []byte(valid)); err != nil {
		t.Fatalf("valid after_start: %v", err)
	}
	for name, definition := range map[string]string{
		"unknown destination": strings.Replace(valid, "after_start: agent", "after_start: nowhere", 1),
		"missing task":        strings.Replace(valid, "  task: {type: task, required: true}\n", "", 1),
		"optional task":       strings.Replace(valid, "task: {type: task, required: true}", "task: {type: task}", 1),
		"ambiguous task":      strings.Replace(valid, "  task: {type: task, required: true}\n", "  task: {type: task, required: true}\n  other: {type: task}\n", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := workflow.Parse(name+".yaml", []byte(definition)); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
