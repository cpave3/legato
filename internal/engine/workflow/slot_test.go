package workflow_test

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

func parseAgentRunWith(with string) error {
	_, err := workflow.Parse("s.yaml", []byte("schema_version: 1\nname: s\ntitle: S\ninputs:\n  task: {type: task, required: true}\nsteps:\n  - id: s\n    title: S\n    type: agent.run\n    with: "+with+"\n"))
	return err
}

// An agent.run step on a card can name the card's agent slot it runs in.
func TestAnAgentRunStepCanNameACardSlot(t *testing.T) {
	for _, slot := range []string{"triage", "main", "plan-2", strings.Repeat("a", 32)} {
		if err := parseAgentRunWith(`{prompt: hi, task: "${{ workflow.inputs.task }}", repo: a/b, slot: ` + slot + `}`); err != nil {
			t.Errorf("slot %q: %v", slot, err)
		}
	}
}

// A slot name is checked when the file loads: its form, the slots other
// executors keep, and that it is a card's slot.
func TestAnAgentRunSlotIsCheckedWhenTheFileLoads(t *testing.T) {
	const invalid = `step "s": with slot must be main or a slot name: lowercase letters, digits and dashes, starting with a letter, at most 32; reviewer is the review loop's`
	cases := []struct{ name, with, want string }{
		{"capitals", `{prompt: hi, task: "${{ workflow.inputs.task }}", slot: Triage}`, invalid},
		{"leading digit", `{prompt: hi, task: "${{ workflow.inputs.task }}", slot: "1x"}`, invalid},
		{"underscore", `{prompt: hi, task: "${{ workflow.inputs.task }}", slot: a_b}`, invalid},
		{"too long", `{prompt: hi, task: "${{ workflow.inputs.task }}", slot: ` + strings.Repeat("a", 33) + `}`, invalid},
		{"empty", `{prompt: hi, task: "${{ workflow.inputs.task }}", slot: ""}`, invalid},
		{"not text", `{prompt: hi, task: "${{ workflow.inputs.task }}", slot: 3}`, invalid},
		{"reserved", `{prompt: hi, task: "${{ workflow.inputs.task }}", slot: reviewer}`, invalid},
		{"expression", `{prompt: hi, task: "${{ workflow.inputs.task }}", slot: "${{ workflow.inputs.task.id }}"}`, invalid},
		{"without task", `{prompt: hi, slot: triage}`, `step "s": with slot requires task`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := parseAgentRunWith(tc.with)
			if err == nil || !strings.HasSuffix(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to end %q", err, tc.want)
			}
		})
	}
}
