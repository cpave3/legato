package workflow_test

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

const changesRequested = `
schema_version: 1
name: address
title: Address feedback
inputs:
  task: {type: task, required: true}
  repo: {type: string}
  pr_number: {type: integer}
on:
  pr.changes_requested:
TRIGGER
steps:
  - id: note
    title: Note
    type: task.note
    with:
      task: ${{ workflow.inputs.task }}
      body: hi
`

func TestAChangesRequestedTrigger(t *testing.T) {
	definition, err := workflow.Parse("a.yaml", []byte(strings.Replace(changesRequested, "TRIGGER\n", "    enabled: false\n    inputs:\n      task: ${{ event.task }}\n      repo: ${{ event.repo }}\n      pr_number: ${{ event.pr_number }}\n", 1)))
	if err != nil {
		t.Fatal(err)
	}
	list := definition.On.List()
	if len(list) != 1 || list[0].Kind != workflow.TriggerPRChangesRequested || list[0].EnabledByDefault {
		t.Fatalf("triggers = %+v", list)
	}
	if _, err := workflow.Parse("a.yaml", []byte(strings.Replace(changesRequested, "TRIGGER\n", "    inputs: {nope: x}\n", 1))); err == nil {
		t.Fatal("accepted an undeclared input")
	}
	if _, err := workflow.Parse("a.yaml", []byte(strings.Replace(changesRequested, "TRIGGER\n", "    max_runs_per_day: -2\n", 1))); err == nil {
		t.Fatal("accepted max_runs_per_day: -2")
	}
}
