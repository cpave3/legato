package hooks

import (
	"strings"
	"testing"
)

// The solo Chimera agent is the primary review-capture path: its general
// prompt must teach reasonable semantic commits and chapter-based review order.
func TestChimeraGeneralPromptTeachesReviewCapture(t *testing.T) {
	prompt := NewChimeraAdapter("legato").GeneralPrompt()

	for _, want := range []string{
		"reasonable semantic commits",
		"granular reading order",
		"legato review chapter",
		"--include <path>:<1-based-hunk>",
		"legato review annotate",
		"--file <path> --hunk <1-based N>",
		"legato review show",
		"diff",
		"legato review ready",
		"legato review answer",
		"Format review answers as Markdown",
		"[legato review]",
		"LEGATO_TASK_ID is unset",
		"explicitly asks for a review",
		"legato review restart",
		"legato review discard",
		"legato review chapters --json",
		"legato review chapter show",
		"--lines <start>-<end>",
		"legato review chapter edit",
		"legato review chapter remove",
		"legato review annotation edit",
		"legato review annotation remove",
		// Named review tours
		"--name <review-name>",
		"LEGATO_REVIEW_NAME",
		"single-feature session",
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
		"reasonable semantic commit",
		"granular reading order",
		"legato review chapter",
		"--include <path>:<1-based-hunk>",
		"legato review annotate",
		"--file <path> --hunk <1-based N>",
		"legato review show",
		"diff",
		"legato review ready",
		"legato review answer",
		"Format review answers as Markdown",
		"[legato review]",
		"Only use the review workflow when `LEGATO_TASK_ID` is set",
		"this is an ephemeral task",
		"skip review capture and all `legato review` commands",
		// Named review tours
		"--name <review-name>",
		"LEGATO_REVIEW_NAME",
		"single-feature swarm",
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
