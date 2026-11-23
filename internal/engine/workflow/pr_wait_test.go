package workflow_test

import (
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

func prWaitStep(with string) string {
	return `schema_version: 1
name: wait
title: Wait
inputs:
  pr: {type: integer, required: true}
steps:
  - id: merged
    title: Wait for the merge
    type: github.pr.wait
    with:
` + with
}

// github.pr.wait waits until a pull request merges or closes (LEG-540).
func TestAPRWaitStepNamesItsPullRequestAndWhatItWaitsFor(t *testing.T) {
	definition, diagnostics := workflow.Check("wait.yaml", []byte(prWaitStep("      repo: acme/widget\n      pr_number: ${{ workflow.inputs.pr }}\n")))
	if definition == nil || len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	for _, until := range []string{"merged", "closed"} {
		if _, err := workflow.Parse("wait.yaml", []byte(prWaitStep("      repo: acme/widget\n      pr_number: 3\n      until: "+until+"\n"))); err != nil {
			t.Fatalf("until %s: %v", until, err)
		}
	}
	for name, with := range map[string]string{
		"no pull request": "      repo: acme/widget\n",
		"no repository":   "      pr_number: 3\n",
		"until open":      "      repo: acme/widget\n      pr_number: 3\n      until: open\n",
	} {
		if _, err := workflow.Parse("wait.yaml", []byte(prWaitStep(with))); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	spec, ok := workflow.LookupExecutor("github.pr.wait")
	if !ok {
		t.Fatal("no catalog entry")
	}
	outputs := map[string]string{}
	for _, output := range spec.Outputs {
		outputs[output.Name] = output.Type
	}
	if outputs["state"] != "string" || outputs["merged"] != "boolean" || outputs["merged_at"] != "string" {
		t.Fatalf("outputs = %v", outputs)
	}
	graph := workflow.BuildGraph(definition)
	for _, node := range graph.Nodes {
		if node.ID == "step:merged" && (node.Step.Key.Field != "pr" || node.Step.Key.Value != "acme/widget#${{ workflow.inputs.pr }}") {
			t.Fatalf("key = %+v", node.Step.Key)
		}
	}
}
