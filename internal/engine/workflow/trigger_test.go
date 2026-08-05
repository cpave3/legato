package workflow_test

import (
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

func TestColumnTriggerSupportsConfirmationAndOpenAgentCondition(t *testing.T) {
	definition, err := workflow.Parse("close-agent.yaml", []byte(`
schema_version: 1
name: close-agent
title: Close agent
inputs:
  task: {type: task, required: true}
on:
  task.entered_column:
    column: Done
    confirm: true
    inputs:
      task: ${{ event.task }}
    conditions:
      all:
        - field: task.has_active_agent
          equals: true
steps:
  - id: close
    title: Close agent
    type: agent.stop
    with: {task: "${{ workflow.inputs.task }}"}
`))
	if err != nil {
		t.Fatal(err)
	}
	trigger := definition.On.TaskEnteredColumn
	if trigger == nil || !trigger.Confirm {
		t.Fatalf("trigger = %#v", trigger)
	}
	matched, err := trigger.Matches(map[string]any{"has_active_agent": true})
	if err != nil || !matched {
		t.Fatalf("matched = %v, err = %v", matched, err)
	}
}

func TestColumnTriggerConditionEvaluatesTaskMetadata(t *testing.T) {
	definition, err := workflow.Parse("triage.yaml", []byte(`
schema_version: 1
name: triage
title: Triage
inputs:
  task: {type: task, required: true}
on:
  task.entered_column:
    column: Triaged
    inputs:
      task: ${{ event.task }}
    conditions:
      all:
        - field: task.metadata.triaged
          not_equals: true
steps:
  - id: run
    title: Run
    type: agent.run
    with: {prompt: triage}
`))
	if err != nil {
		t.Fatal(err)
	}
	trigger := definition.On.TaskEnteredColumn
	if trigger == nil || trigger.Column != "Triaged" {
		t.Fatalf("trigger = %#v", trigger)
	}
	matched, err := trigger.Matches(map[string]any{"metadata": map[string]any{"triaged": false}})
	if err != nil || !matched {
		t.Fatalf("matched = %v, err = %v", matched, err)
	}
	matched, err = trigger.Matches(map[string]any{"metadata": map[string]any{"triaged": true}})
	if err != nil || matched {
		t.Fatalf("matched = %v, err = %v", matched, err)
	}
}
