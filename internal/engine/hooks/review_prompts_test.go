package hooks

import (
	"strings"
	"testing"
)

// The solo Chimera agent is the primary review-capture path: its general
// prompt must teach semantic commits and the three review verbs.
func TestChimeraGeneralPromptTeachesReviewCapture(t *testing.T) {
	prompt := NewChimeraAdapter("legato").GeneralPrompt()

	for _, want := range []string{
		"semantic commit",
		"legato review annotate",
		"--file <path> --hunk <1-based N>",
		"legato review show",
		"diff",
		"legato review ready",
		"legato review answer",
		"[legato review]",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("GeneralPrompt missing %q", want)
		}
	}
}

func TestChimeraSandboxPreambleCoversReviewVerbs(t *testing.T) {
	preamble := NewChimeraAdapter("legato").RolePromptPreamble()
	if !strings.Contains(preamble, "legato review") {
		t.Error("sandbox preamble should name legato review as host-mode-required")
	}
}

// The conductor owns the review packet for swarms: checkpoint commits with
// the sub-task trailer, ready at finish, and the answer protocol.
func TestConductorPromptTeachesReviewPacket(t *testing.T) {
	prompt := builtinRolePrompt("conductor")

	for _, want := range []string{
		"Legato-Subtask:",
		"legato review annotate",
		"--file <path> --hunk <1-based N>",
		"legato review show",
		"diff",
		"legato review ready",
		"legato review answer",
		"[legato review]",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("conductor prompt missing %q", want)
		}
	}
}

// Workers stay out of the review packet entirely — their contract (including
// "do not commit") is unchanged.
func TestWorkerPromptUnchangedByReviewFeature(t *testing.T) {
	prompt := builtinRolePrompt("backend")
	if strings.Contains(prompt, "legato review") {
		t.Error("worker prompt must not mention review verbs (packet is the conductor's job)")
	}
	if !strings.Contains(prompt, "Do not commit") {
		t.Error("worker do-not-commit contract must remain")
	}
}
