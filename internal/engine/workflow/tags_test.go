package workflow

import (
	"slices"
	"strings"
	"testing"
)

func taggedWorkflow(tags string) []byte {
	return []byte(`schema_version: 2
name: prod-health-check
title: Prod health check
` + tags + `
steps:
  - id: note
    title: Note
    type: task.note
    with:
      task: abc
      body: hello
`)
}

func TestAWorkflowFileCarriesItsTags(t *testing.T) {
	definition, err := Parse("prod-health-check.workflow.yaml", taggedWorkflow("tags: [autopilot, ops]"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(definition.Tags, []string{"autopilot", "ops"}) {
		t.Fatalf("tags = %v, want [autopilot ops]", definition.Tags)
	}
}

func TestAWorkflowWithoutTagsHasNone(t *testing.T) {
	definition, err := Parse("prod-health-check.workflow.yaml", taggedWorkflow(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(definition.Tags) != 0 {
		t.Fatalf("tags = %v, want none", definition.Tags)
	}
}

func TestTagsAreRefusedUnlessTheyAreShortLowerCaseWords(t *testing.T) {
	for name, tags := range map[string]string{
		"upper case":   "tags: [Autopilot]",
		"a space":      "tags: [\"auto pilot\"]",
		"empty":        "tags: [\"\"]",
		"leading -":    "tags: [-ops]",
		"too long":     "tags: [" + strings.Repeat("a", 33) + "]",
		"duplicate":    "tags: [ops, ops]",
		"nine of them": "tags: [a, b, c, d, e, f, g, h, i]",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse("x.workflow.yaml", taggedWorkflow(tags))
			if err == nil || !strings.Contains(err.Error(), "tags") {
				t.Fatalf("error = %v, want a tags error", err)
			}
		})
	}
}

func TestTheBuiltInReviewLoopIsTaggedReviews(t *testing.T) {
	if !slices.Equal(BuiltInReviewLoop().Tags, []string{"reviews"}) {
		t.Fatalf("review-loop tags = %v, want [reviews]", BuiltInReviewLoop().Tags)
	}
}

func callerOf(name, target string) string {
	return `schema_version: 2
name: ` + name + `
title: ` + name + `
steps:
  - id: call
    title: Call
    type: workflow.run
    with:
      workflow: ` + target + `
`
}

func TestCallersAreTheWorkflowsWhoseStepsRunIt(t *testing.T) {
	inner := mustParse(t, string(taggedWorkflow("")))
	callers := Callers([]*Definition{
		mustParse(t, callerOf("ci-health", "prod-health-check")),
		mustParse(t, callerOf("nightly", "prod-health-check")),
		inner,
	})
	if !slices.Equal(callers["prod-health-check"], []string{"ci-health", "nightly"}) {
		t.Fatalf("callers = %v", callers)
	}
	if len(callers["ci-health"]) != 0 {
		t.Fatalf("ci-health has callers %v, want none", callers["ci-health"])
	}
}

const commentedWorkflow = `# Probes production.
schema_version: 2
name: prod-health-check
title: Prod health check # shown in the list
description: >-
  Probes the endpoints.
steps:
  - id: note # the only step
    title: Note
    type: task.note
    with:
      task: abc
      body: hello
`

func TestSetTagsAddsTheFieldAfterTheTitleAndKeepsTheRest(t *testing.T) {
	got, err := SetTags(commentedWorkflow, []string{"autopilot", "ops"})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(commentedWorkflow, "title: Prod health check # shown in the list\n", "title: Prod health check # shown in the list\ntags: [autopilot, ops]\n", 1)
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestSetTagsReplacesTagsAFileHas(t *testing.T) {
	for name, tags := range map[string]string{
		"flow":  "tags: [old] # grouping\n",
		"block": "tags:\n  - old\n  - older\n",
	} {
		t.Run(name, func(t *testing.T) {
			text := strings.Replace(commentedWorkflow, "description:", tags+"description:", 1)
			got, err := SetTags(text, []string{"ops"})
			if err != nil {
				t.Fatal(err)
			}
			if want := strings.Replace(commentedWorkflow, "description:", "tags: [ops]\ndescription:", 1); got != want {
				t.Fatalf("got\n%s\nwant\n%s", got, want)
			}
		})
	}
}

func TestSetTagsWithNoTagsRemovesTheField(t *testing.T) {
	text := strings.Replace(commentedWorkflow, "description:", "tags: [old]\ndescription:", 1)
	got, err := SetTags(text, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != commentedWorkflow {
		t.Fatalf("got\n%s", got)
	}
}

func TestSetTagsQuotesATagYAMLWouldReadAsSomethingElse(t *testing.T) {
	got, err := SetTags(commentedWorkflow, []string{"true", "2026"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(mustParse(t, got).Tags, []string{"true", "2026"}) {
		t.Fatalf("tags read back as %v from\n%s", mustParse(t, got).Tags, got)
	}
}

func TestSetTagsRefusesTagsTheFileCannotHold(t *testing.T) {
	if _, err := SetTags(commentedWorkflow, []string{"Not A Tag"}); err == nil {
		t.Fatal("want an error")
	}
}
