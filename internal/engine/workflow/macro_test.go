package workflow_test

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

func TestAgentStepsTakeAMacroOrAPromptButNotBoth(t *testing.T) {
	for _, kind := range []string{"agent.run", "agent.dispatch"} {
		// agent.run's task makes it the task's own agent, which takes no
		// working_dir; dispatch's task is the agent it messages.
		task := "      task: ${{ workflow.inputs.task }}\n"
		if kind == "agent.run" {
			task = ""
		}
		base := `
schema_version: 1
name: m
title: M
inputs:
  task: {type: task, required: true}
steps:
  - id: s
    title: S
    type: ` + kind + `
    with:
` + task + `      working_dir: /repo
`
		cases := map[string]struct {
			with string
			ok   bool
		}{
			"macro alone":  {"      macro: Review\n", true},
			"prompt alone": {"      prompt: go\n", true},
			"both":         {"      macro: Review\n      prompt: go\n", false},
			"neither":      {"", false},
			"empty macro":  {"      macro: \"\"\n", false},
		}
		for name, c := range cases {
			t.Run(kind+"/"+name, func(t *testing.T) {
				_, err := workflow.Parse("m.yaml", []byte(base+c.with))
				if c.ok && err != nil {
					t.Fatalf("refused: %v", err)
				}
				if !c.ok && err == nil {
					t.Fatal("accepted")
				}
				if !c.ok && name == "both" && !strings.Contains(err.Error(), "macro") {
					t.Fatalf("error should name macro: %v", err)
				}
			})
		}
	}
}
