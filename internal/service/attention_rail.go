package service

import (
	"context"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

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
	AttentionSignalWorkflowReviewSteward:       "workflows",
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

// RailAsks is what agents' executions already ask the operator, so the rail
// can show one item for one wait. The zero value asks nothing.
type RailAsks struct {
	// blocked is the executions with a pending approval or an open
	// operator item: their Waiting item says nothing more.
	blocked map[string]bool
	// questioned is the executions with a pending question approval: their
	// operator questions repeat it.
	questioned map[string]bool
	// operatorExecution is each open operator item's execution.
	operatorExecution map[string]string
}

// RailAsks reads what agents' executions ask now: their pending approvals
// and their open operator items.
func (s *AttentionService) RailAsks(ctx context.Context) (RailAsks, error) {
	approvals, err := s.store.ListApprovals(ctx, store.ApprovalFilter{Status: store.ApprovalPending})
	if err != nil {
		return RailAsks{}, err
	}
	items, err := s.store.ListOpenOperatorItems(ctx)
	if err != nil {
		return RailAsks{}, err
	}
	asks := RailAsks{blocked: map[string]bool{}, questioned: map[string]bool{}, operatorExecution: map[string]string{}}
	for _, approval := range approvals {
		if approval.ExecutionID == "" {
			continue
		}
		asks.blocked[approval.ExecutionID] = true
		if approval.Source == ApprovalSourceAgentQuestion {
			asks.questioned[approval.ExecutionID] = true
		}
	}
	for _, item := range items {
		if item.ExecutionID == "" {
			continue
		}
		asks.blocked[item.ExecutionID] = true
		asks.operatorExecution[item.ID] = item.ExecutionID
	}
	return asks, nil
}

// repeats says whether the rail leaves item out because another item of
// the same wait says it: an agent's Waiting while it has an approval or an
// operator item, and its operator question while it asks one with its
// tool's own question prompt.
func (a RailAsks) repeats(item AttentionItem) bool {
	switch {
	case item.Producer == "agents" && item.SignalKind == AttentionSignalAgentWaiting:
		return a.blocked[item.SourceKey]
	case item.Producer == OperatorItemsAttentionProducer && item.SignalKind == AttentionSignalOperatorQuestion:
		execution, ok := a.operatorExecution[item.SourceKey]
		return ok && a.questioned[execution]
	}
	return false
}

// RailView is the items the rail shows under rail: those of the groups
// switched on, each with its group, less those that repeat another item
// of the same wait (asks). The items given are not changed.
func RailView(items []AttentionItem, rail config.RailConfig, asks RailAsks) []AttentionItem {
	out := make([]AttentionItem, 0, len(items))
	for _, item := range items {
		group := RailGroupOf(item.SignalKind)
		if !rail.Shows(group) || asks.repeats(item) {
			continue
		}
		item.RailGroup = group
		out = append(out, item)
	}
	return out
}
