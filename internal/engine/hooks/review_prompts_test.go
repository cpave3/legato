package hooks

import (
	"strings"
	"testing"
)

// The solo Chimera agent is the primary review-capture path: its general
// prompt must teach reasonable semantic commits and chapter-based review order.
func TestChimeraGeneralPromptTeachesReviewCapture(t *testing.T) {
	prompt := NewChimeraAdapter("legato").GeneralPrompt()

	if strings.Contains(prompt, `legato review answer <step-id> \"`) {
		t.Error("standalone prompt contains the unsafe positional answer form")
	}
	for _, want := range []string{
		"reasonable semantic commits",
		"legato plan submit",
		"do not implement until approved",
		"legato plan complete",
		"legato-plan skill",
		"granular reading order",
		"legato review chapter",
		"--include <path>:<1-based-hunk>",
		"legato review annotate",
		"--file <path> --hunk <1-based N>",
		"legato review show",
		"diff",
		"legato review ready",
		"legato review answer <step-id> --file - <<'LEGATO_ANSWER'",
		"Format review answers as Markdown",
		"[legato review]",
		"LEGATO_TASK_EPHEMERAL=1",
		"explicitly asks for a review",
		"legato review restart --feedback",
		"captured plan",
		"review pass",
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

func TestManagedAgentPromptsTeachPeerCoordination(t *testing.T) {
	for name, prompt := range map[string]string{
		"standalone": NewChimeraAdapter("legato").GeneralPrompt(),
		"conductor":  builtinRolePrompt("conductor"),
		"worker":     builtinRolePrompt("worker"),
	} {
		t.Run(name, func(t *testing.T) {
			for _, want := range []string{"legato agent peers", "legato agent message", "legato agent broadcast", "Unexpected changes", "do not revert"} {
				if !strings.Contains(prompt, want) {
					t.Errorf("prompt missing %q", want)
				}
			}
		})
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

	if strings.Contains(prompt, `legato review answer <step-id> \"`) {
		t.Error("conductor prompt contains the unsafe positional answer form")
	}
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
		"legato review answer <step-id> --file - <<'LEGATO_ANSWER'",
		"Format review answers as Markdown",
		"[legato review]",
		"If `LEGATO_TASK_EPHEMERAL=1`",
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
