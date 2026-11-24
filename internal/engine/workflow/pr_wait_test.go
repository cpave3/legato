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

// The wait names the card whose merge it is, and makes a needs-you item
// unless notify is false (LEG-614).
func TestAPRWaitStepNamesItsCardAndCanStayQuiet(t *testing.T) {
	for name, with := range map[string]string{
		"a card":       "      repo: acme/widget\n      pr_number: 3\n      task: abc12345\n",
		"notify false": "      repo: acme/widget\n      pr_number: 3\n      notify: false\n",
		"notify true":  "      repo: acme/widget\n      pr_number: 3\n      notify: true\n",
	} {
		if _, err := workflow.Parse("wait.yaml", []byte(prWaitStep(with))); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := workflow.Parse("wait.yaml", []byte(prWaitStep("      repo: acme/widget\n      pr_number: 3\n      notify: sometimes\n"))); err == nil {
		t.Fatal("notify: sometimes accepted")
	}
	spec, _ := workflow.LookupExecutor("github.pr.wait")
	fields := map[string]workflow.Field{}
	for _, field := range spec.With {
		fields[field.Name] = field
	}
	if fields["task"].Kind != workflow.KindTaskOrID || fields["notify"].Kind != workflow.KindBoolean || fields["notify"].Default != true {
		t.Fatalf("task = %+v, notify = %+v", fields["task"], fields["notify"])
	}
}
