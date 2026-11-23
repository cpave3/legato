package workflow_test

import (
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

// burnlite.task.close closes a task's Burnlite ticket (LEG-540).
func TestABurnliteCloseStepNamesItsTaskAndReason(t *testing.T) {
	step := func(with string) string {
		return checkBase + `steps:
  - id: close
    title: Close the ticket
    type: burnlite.task.close
    with:
` + with
	}
	definition, diagnostics := workflow.Check("close.yaml", []byte(step("      task: ${{ workflow.inputs.task }}\n      summary: Merged\n      optional: true\n")))
	if definition == nil || len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	for _, reason := range []string{"completed", "not_planned"} {
		if _, err := workflow.Parse("close.yaml", []byte(step("      task: LEG-7\n      reason: "+reason+"\n"))); err != nil {
			t.Fatalf("reason %s: %v", reason, err)
		}
	}
	for name, with := range map[string]string{
		"no task":     "      summary: Merged\n",
		"reason done": "      task: LEG-7\n      reason: done\n",
	} {
		if _, err := workflow.Parse("close.yaml", []byte(step(with))); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}
