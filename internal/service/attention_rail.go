package service

import "github.com/cpave3/legato/config"

// railGroups maps each signal kind to the group of the needs-you rail it is
// switched with (config.RailConfig). A kind not here is Legato's.
var railGroups = map[string]string{
	AttentionSignalOperatorAction:   "actions",
	AttentionSignalOperatorQuestion: "questions",
	AttentionSignalOperatorDone:     "done",
	AttentionSignalOperatorOther:    "other",

	AttentionSignalApproval: "approvals",

	AttentionSignalAgentWaiting: "agents",
	AttentionSignalAgentError:   "agents",

	AttentionSignalWorkflowManualGate:          "workflows",
	AttentionSignalWorkflowWaitingAgent:        "workflows",
	AttentionSignalWorkflowFailed:              "workflows",
	AttentionSignalWorkflowReviewStuck:         "workflows",
	AttentionSignalWorkflowReviewAuthor:        "workflows",
	AttentionSignalWorkflowMergeWaiting:        "workflows",
	AttentionSignalWorkflowGate:                "workflows",
	AttentionSignalWorkflowCompletion:          "workflows",
	AttentionSignalWorkflowTriggerInput:        "workflows",
	AttentionSignalWorkflowTriggerConfirmation: "workflows",
	AttentionSignalWorkflowTriggerFailed:       "workflows",
	AttentionSignalWorkflowTriggerLoopStop:     "workflows",

	AttentionSignalPlanDecision: "plans",

	AttentionSignalReviewReady: "review",
	AttentionSignalHandoffDone: "review",

	AttentionSignalPRMergeBlocked: "merge",
	AttentionSignalArgusCase:      "merge",
	AttentionSignalArgusNeedsYou:  "merge",
}

// RailGroupOf is the rail group of a signal kind: "legato" for one with no
// group of its own.
func RailGroupOf(signal string) string {
	if group, ok := railGroups[signal]; ok {
		return group
	}
	return "legato"
}

// RailView is the items the rail shows under rail: those of the groups
// switched on, each with its group. The items given are not changed.
func RailView(items []AttentionItem, rail config.RailConfig) []AttentionItem {
	out := make([]AttentionItem, 0, len(items))
	for _, item := range items {
		group := RailGroupOf(item.SignalKind)
		if !rail.Shows(group) {
			continue
		}
		item.RailGroup = group
		out = append(out, item)
	}
	return out
}
