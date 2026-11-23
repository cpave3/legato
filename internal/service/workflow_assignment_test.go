package service

import (
	"strings"
	"testing"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// The completion contract tells a workflow agent where its result must go:
// what it says in the conversation is lost when Legato removes it (LEG-532).
func TestWorkflowAssignmentSaysWhereTheResultMustGo(t *testing.T) {
	step := func(with map[string]any, outputs map[string]workflowengine.Output) ResolvedWorkflowStep {
		return ResolvedWorkflowStep{RunID: "wr-1", TaskID: "wf-1", StepID: "digest", AttemptID: "wa-1", With: with, Outputs: outputs}
	}

	declared := buildWorkflowAssignment("Write the digest.", step(nil, map[string]workflowengine.Output{
		"digest": {Type: "string"}, "links": {Type: "json"},
	}), "")
	for _, want := range []string{`Declared outputs: ["- digest (string)","- links (json)"]`, "not kept", "declared outputs"} {
		if !strings.Contains(declared, want) {
			t.Fatalf("assignment with outputs lacks %q:\n%s", want, declared)
		}
	}

	none := buildWorkflowAssignment("Write the digest.", step(nil, nil), "")
	for _, want := range []string{"This step declares no outputs", "nothing you write in this conversation is kept", "delivered in chat"} {
		if !strings.Contains(none, want) {
			t.Fatalf("assignment without outputs lacks %q:\n%s", want, none)
		}
	}

	kept := buildWorkflowAssignment("Write the digest.", step(map[string]any{"keep_agent": true}, nil), "")
	if !strings.Contains(kept, "stays running") || strings.Contains(kept, "nothing you write in this conversation is kept") {
		t.Fatalf("assignment of a kept agent:\n%s", kept)
	}
}
