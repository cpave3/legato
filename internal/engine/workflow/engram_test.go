package workflow_test

import (
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

func TestAnAgentRunStepNamesItsEngramAsTemplatedText(t *testing.T) {
	definition, err := workflow.Parse("engram.yaml", []byte(`
schema_version: 1
name: engram
title: Engram
inputs:
  who: {type: string, required: true}
steps:
  - id: work
    title: Work
    type: agent.run
    with: {prompt: work, engram: "${{ workflow.inputs.who }}"}
`))
	if err != nil {
		t.Fatal(err)
	}
	step, err := definition.ResolveStep("work", map[string]any{"who": "mneme"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if step.With["engram"] != "mneme" {
		t.Fatalf("engram = %#v", step.With["engram"])
	}
}

func TestAnAgentRunStepsEngramMustBeText(t *testing.T) {
	_, err := workflow.Parse("engram.yaml", []byte(`
schema_version: 1
name: engram
title: Engram
steps:
  - id: work
    title: Work
    type: agent.run
    with: {prompt: work, engram: [mneme]}
`))
	if err == nil {
		t.Fatal("a list was taken as an engram")
	}
}
