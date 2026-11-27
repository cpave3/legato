package workflow_test

import (
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

func TestAnAgentRunStepNamesItsAccountAsTemplatedText(t *testing.T) {
	definition, err := workflow.Parse("account.yaml", []byte(`
schema_version: 1
name: account
title: Account
inputs:
  who: {type: string, required: true}
steps:
  - id: work
    title: Work
    type: agent.run
    with: {prompt: work, account: "${{ workflow.inputs.who }}"}
`))
	if err != nil {
		t.Fatal(err)
	}
	step, err := definition.ResolveStep("work", map[string]any{"who": "work"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if step.With["account"] != "work" {
		t.Fatalf("account = %#v", step.With["account"])
	}
}

func TestAnAgentRunStepsAccountMustBeText(t *testing.T) {
	_, err := workflow.Parse("account.yaml", []byte(`
schema_version: 1
name: account
title: Account
steps:
  - id: work
    title: Work
    type: agent.run
    with: {prompt: work, account: [work]}
`))
	if err == nil {
		t.Fatal("a list was taken as an account")
	}
}
