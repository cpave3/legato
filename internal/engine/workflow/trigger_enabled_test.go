package workflow_test

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

const columnTriggered = `
schema_version: 1
name: triage
title: Triage
inputs:
  task: {type: task, required: true}
on:
  task.entered_column:
    column: Triage
    inputs:
      task: ${{ event.task }}
ENABLED
steps:
  - id: note
    title: Note
    type: task.note
    with:
      task: ${{ workflow.inputs.task }}
      body: hi
`

func TestATriggerIsOnUnlessItSaysEnabledFalse(t *testing.T) {
	for value, want := range map[string]bool{"": true, "    enabled: true\n": true, "    enabled: false\n": false} {
		definition, err := workflow.Parse("t.yaml", []byte(strings.Replace(columnTriggered, "ENABLED\n", value, 1)))
		if err != nil {
			t.Fatalf("%q: %v", value, err)
		}
		triggers := definition.On.List()
		if len(triggers) != 1 || triggers[0].Kind != workflow.TriggerTaskEnteredColumn || triggers[0].EnabledByDefault != want {
			t.Fatalf("%q: triggers = %+v", value, triggers)
		}
	}
	if _, err := workflow.Parse("t.yaml", []byte(strings.Replace(columnTriggered, "ENABLED\n", "    enabled: sometimes\n", 1))); err == nil {
		t.Fatal("accepted enabled: sometimes")
	}
}

func TestADefinitionWithoutTriggersListsNone(t *testing.T) {
	definition, err := workflow.Parse("t.yaml", []byte(strings.Replace(strings.Replace(columnTriggered, "ENABLED\n", "", 1), "on:\n  task.entered_column:\n    column: Triage\n    inputs:\n      task: ${{ event.task }}\n", "", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if got := definition.On.List(); len(got) != 0 {
		t.Fatalf("triggers = %+v", got)
	}
}
