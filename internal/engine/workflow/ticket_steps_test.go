package workflow_test

import (
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

// ticket.create and ticket.comment write to Jira or Burnlite with Legato's
// own settings (LEG-640).
func TestTicketStepsNameWhatTheyWrite(t *testing.T) {
	step := func(stepType, with string) string {
		return checkBase + `steps:
  - id: raise
    title: Raise a ticket
    type: ` + stepType + `
    with:
` + with
	}
	accepted := map[string]string{
		"a Jira bug":      "ticket.create|      provider: jira\n      project: AG\n      summary: Login fails\n      description: |\n        Seen today\n      labels: [prod, slack]\n      priority: High\n      fields: {customfield_10020: 7}\n",
		"a Burnlite task": "ticket.create|      provider: burnlite\n      project: LEG\n      summary: Fix login\n      size: Medium\n      uncertainty: Low\n      kind: work\n",
		"templates":       "ticket.create|      provider: ${{ workflow.inputs.task.metadata.provider }}\n      project: AG\n      summary: ${{ workflow.inputs.task.title }}\n",
		"a comment":       "ticket.comment|      key: AG-12\n      body: Seen again in prod today\n",
	}
	refused := map[string]string{
		"no project":    "ticket.create|      summary: S\n",
		"no summary":    "ticket.create|      project: AG\n",
		"a provider":    "ticket.create|      provider: github\n      project: AG\n      summary: S\n",
		"a size":        "ticket.create|      project: LEG\n      summary: S\n      size: Huge\n",
		"no key":        "ticket.comment|      body: x\n",
		"no body":       "ticket.comment|      key: AG-1\n",
		"comment to gh": "ticket.comment|      provider: github\n      key: AG-1\n      body: x\n",
	}
	split := func(s string) (string, string) {
		for i := range s {
			if s[i] == '|' {
				return s[:i], s[i+1:]
			}
		}
		return s, ""
	}
	for name, c := range accepted {
		stepType, with := split(c)
		if definition, diagnostics := workflow.Check("ticket.yaml", []byte(step(stepType, with))); definition == nil || len(diagnostics) != 0 {
			t.Fatalf("%s: diagnostics = %+v", name, diagnostics)
		}
	}
	for name, c := range refused {
		stepType, with := split(c)
		if _, err := workflow.Parse("ticket.yaml", []byte(step(stepType, with))); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	for stepType, outputs := range map[string][]string{
		"ticket.create":  {"key", "url", "id", "provider", "created"},
		"ticket.comment": {"url", "id", "key"},
	} {
		spec, ok := workflow.LookupExecutor(stepType)
		if !ok || spec.Group != "Board" {
			t.Fatalf("%s spec = %+v", stepType, spec)
		}
		var names []string
		for _, o := range spec.Outputs {
			names = append(names, o.Name)
		}
		if len(names) != len(outputs) {
			t.Fatalf("%s outputs = %v, want %v", stepType, names, outputs)
		}
		for i := range names {
			if names[i] != outputs[i] {
				t.Fatalf("%s outputs = %v, want %v", stepType, names, outputs)
			}
		}
	}
}
