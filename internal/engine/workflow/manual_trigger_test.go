package workflow_test

import (
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

func TestManualTriggerAndWorktreeRemoveDefinition(t *testing.T) {
	definition, err := workflow.Parse("cleanup.yaml", []byte(`
schema_version: 1
name: cleanup
title: Cleanup
steps:
  - id: prepare
    title: Prepare
    type: github.pr.prepare
    with: {repo: owner/repo, number: 1, primary_dir: /repo}
    outputs:
      worktree_path: {type: string}
  - id: cleanup
    title: Remove worktree
    type: worktree.remove
    trigger: manual
    with:
      expected_path: ${{ steps.prepare.outputs.worktree_path }}
`))
	if err != nil {
		t.Fatal(err)
	}
	if definition.Steps[0].Trigger != "automatic" || definition.Steps[1].Trigger != "manual" {
		t.Fatalf("triggers = %q, %q", definition.Steps[0].Trigger, definition.Steps[1].Trigger)
	}
}

func TestInvalidStepTriggerRejected(t *testing.T) {
	_, err := workflow.Parse("bad.yaml", []byte("schema_version: 1\nname: bad\ntitle: Bad\nsteps:\n  - id: run\n    title: Run\n    type: agent.run\n    trigger: eventually\n    with: {prompt: run}\n"))
	if err == nil {
		t.Fatal("invalid trigger was accepted")
	}
}

// A step can wait for the workflow task's agent to end, so a person ends a
// conversation by closing the agent.
func TestAgentEndedTriggerIsAccepted(t *testing.T) {
	definition, err := workflow.Parse("discuss.yaml", []byte(`
schema_version: 1
name: discuss
title: Discuss
steps:
  - id: discuss
    title: Discuss
    type: agent.dispatch
    with:
      task: ${{ workflow.task }}
      working_dir: /repo
      prompt: Read the diff.
  - id: end
    title: End discussion
    type: agent.stop
    trigger: agent_ended
    with:
      task: ${{ workflow.task }}
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := definition.Steps[1].Trigger; got != "agent_ended" {
		t.Fatalf("trigger = %q, want agent_ended", got)
	}
	if !definition.Steps[1].WaitsForPerson() || definition.Steps[0].WaitsForPerson() {
		t.Fatal("an agent_ended step waits like a manual one; an automatic step does not")
	}
}

func TestAgentRunKeepAgentMustBeTrueOrFalse(t *testing.T) {
	for _, value := range []string{"true", "false"} {
		if _, err := workflow.Parse("keep.yaml", []byte("schema_version: 1\nname: keep\ntitle: Keep\nsteps:\n  - id: run\n    title: Run\n    type: agent.run\n    with: {prompt: run, keep_agent: "+value+"}\n")); err != nil {
			t.Fatalf("keep_agent: %s rejected: %v", value, err)
		}
	}
	if _, err := workflow.Parse("keep.yaml", []byte("schema_version: 1\nname: keep\ntitle: Keep\nsteps:\n  - id: run\n    title: Run\n    type: agent.run\n    with: {prompt: run, keep_agent: \"yes\"}\n")); err == nil {
		t.Fatal(`keep_agent: "yes" was accepted`)
	}
}
