package hooks

import (
	"strings"
	"testing"
)

type reviewsSwitch interface {
	RoleSystemPrompt(role string) string
	SetReviews(on bool)
}

func TestReviewsOffLeavesReviewsOutOfTheConductorPrompt(t *testing.T) {
	for name, adapter := range map[string]reviewsSwitch{
		"claude-code": NewClaudeCodeAdapter("legato"),
		"codex":       NewCodexAdapter("legato"),
		"chimera":     NewChimeraAdapter("legato"),
	} {
		t.Run(name, func(t *testing.T) {
			on := adapter.RoleSystemPrompt("conductor")
			if !strings.Contains(on, "legato review ready") {
				t.Fatal("with reviews on, the conductor prompt should teach the review packet")
			}
			adapter.SetReviews(false)
			off := adapter.RoleSystemPrompt("conductor")
			for _, gone := range []string{"legato review", "review tour", "handoff", "<!--"} {
				if strings.Contains(off, gone) {
					t.Errorf("with reviews off, the conductor prompt still has %q", gone)
				}
			}
			for _, kept := range []string{"sole committer", "Legato-Subtask:", "legato swarm finish", "legato agent peers"} {
				if !strings.Contains(off, kept) {
					t.Errorf("with reviews off, the conductor prompt lost %q", kept)
				}
			}
		})
	}
}

func TestReviewsOffLeavesReviewsOutOfChimerasGeneralPrompt(t *testing.T) {
	adapter := NewChimeraAdapter("legato")
	adapter.SetReviews(false)
	prompt := adapter.GeneralPrompt()
	for _, gone := range []string{"legato review", "Review capture", "review tour"} {
		if strings.Contains(prompt, gone) {
			t.Errorf("with reviews off, the general prompt still has %q", gone)
		}
	}
	for _, kept := range []string{"legato plan submit", "legato agent peers", "semantic commits"} {
		if !strings.Contains(prompt, kept) {
			t.Errorf("with reviews off, the general prompt lost %q", kept)
		}
	}
}

func TestReviewMarkersNeverReachAPrompt(t *testing.T) {
	if strings.Contains(NewClaudeCodeAdapter("legato").RoleSystemPrompt("conductor"), "<!--") {
		t.Fatal("the conductor prompt shows its review markers")
	}
}

// Claude Code and Codex are taught reviews only by the launch prompt Legato
// writes; Chimera's general prompt teaches them itself.
func TestOnlyAdaptersWithoutAReviewPromptAskForOne(t *testing.T) {
	type asker interface{ NeedsReviewsPrompt() bool }
	for name, adapter := range map[string]any{
		"claude-code": NewClaudeCodeAdapter("legato"),
		"codex":       NewCodexAdapter("legato"),
		"chimera":     NewChimeraAdapter("legato"),
	} {
		a, ok := adapter.(asker)
		asks := ok && a.NeedsReviewsPrompt()
		if want := name != "chimera"; asks != want {
			t.Errorf("%s asks for a review prompt = %v, want %v", name, asks, want)
		}
	}
}
