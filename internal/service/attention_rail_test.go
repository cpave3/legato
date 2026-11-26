package service

import (
	"testing"

	"github.com/cpave3/legato/config"
)

// Every signal kind Attention knows belongs to one of the rail's groups,
// and a kind with no group of its own falls into Legato's.
func TestEveryKnownSignalHasARailGroup(t *testing.T) {
	groups := map[string]bool{}
	for _, group := range config.RailGroups {
		groups[group] = true
	}
	for signal := range knownAttentionSignals {
		if !groups[RailGroupOf(signal)] {
			t.Errorf("%s is in rail group %q, which is not one", signal, RailGroupOf(signal))
		}
	}
	for signal, want := range map[string]string{
		AttentionSignalOperatorAction: "actions", AttentionSignalOperatorQuestion: "questions",
		AttentionSignalOperatorDone: "done", AttentionSignalOperatorOther: "other",
		AttentionSignalApproval: "approvals", AttentionSignalReevePeer: "legato",
		AttentionSignalAgentWaiting: "agents", AttentionSignalAgentError: "agents",
		AttentionSignalWorkflowFailed: "workflows", AttentionSignalWorkflowTriggerLoopStop: "workflows", AttentionSignalWorkflowReviewStuck: "workflows",
		AttentionSignalWorkflowReviewAuthor: "workflows", AttentionSignalWorkflowReviewSteward: "workflows",
		AttentionSignalPlanDecision: "plans",
		AttentionSignalReviewReady:  "review", AttentionSignalHandoffDone: "review",
		AttentionSignalPRMergeBlocked: "merge", AttentionSignalArgusCase: "merge", AttentionSignalArgusNeedsYou: "merge",
		AttentionSignalUpdateAvailable: "legato", AttentionSignalAgentsInterrupted: "legato", AttentionSignalHandoffFailed: "legato",
		"something_new": "legato",
	} {
		if got := RailGroupOf(signal); got != want {
			t.Errorf("RailGroupOf(%s) = %q, want %q", signal, got, want)
		}
	}
}

func TestTheRailViewFollowsEachSetting(t *testing.T) {
	items := []AttentionItem{
		{ID: "a", SignalKind: AttentionSignalOperatorAction},
		{ID: "r", SignalKind: AttentionSignalReviewReady},
		{ID: "m", SignalKind: AttentionSignalPRMergeBlocked},
		{ID: "u", SignalKind: AttentionSignalUpdateAvailable},
	}
	ids := func(rail config.RailConfig) string {
		out := ""
		for _, item := range RailView(items, rail) {
			out += item.ID + ":" + item.RailGroup + " "
		}
		return out
	}
	if got := ids(config.RailConfig{}); got != "a:actions u:legato " {
		t.Fatalf("by default the rail shows %q", got)
	}
	on, off := true, false
	if got := ids(config.RailConfig{Review: &on, Merge: &on, Actions: &off}); got != "r:review m:merge u:legato " {
		t.Fatalf("with review and merge on and actions off, the rail shows %q", got)
	}
	if items[0].RailGroup != "" {
		t.Fatal("the rail view changed the items it was given")
	}
}
