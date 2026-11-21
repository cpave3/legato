package service

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/cpave3/legato/internal/engine/store"
)

// CardNeedsYou is what waits on the operator for one task, for its board
// card's strip: open operator items by kind, approvals, plans. See
// docs/ai/agent-states.md § What the card says needs you.
type CardNeedsYou struct {
	// Items counts the agent's open operator items by kind: question,
	// action, done, other.
	Items     map[string]int     `json:"items,omitempty"`
	Approvals *NeedsYouApprovals `json:"approvals,omitempty"`
	Plans     *NeedsYouPlans     `json:"plans,omitempty"`
}

// NeedsYouApprovals are a task's pending approvals: how many, and the
// first's ID and short label ("permission to run a command", or its
// question).
type NeedsYouApprovals struct {
	Count int    `json:"count"`
	ID    string `json:"id"`
	Label string `json:"label"`
}

// NeedsYouPlans are a task's proposed plans waiting for a decision, and the
// first one's ID.
type NeedsYouPlans struct {
	Count  int    `json:"count"`
	PlanID string `json:"plan_id"`
}

var operatorItemKinds = map[string]string{
	AttentionSignalOperatorAction:   OperatorItemAction,
	AttentionSignalOperatorQuestion: OperatorItemQuestion,
	AttentionSignalOperatorDone:     OperatorItemDone,
	AttentionSignalOperatorOther:    OperatorItemOther,
}

// NeedsYouByTask groups what is open by task, in one pass: operator items
// and plans from Attention's open items, approvals from the pending ones
// (oldest first). A task with nothing open has no entry.
func NeedsYouByTask(attention []AttentionItem, approvals []Approval) map[string]*CardNeedsYou {
	out := map[string]*CardNeedsYou{}
	entry := func(taskID string) *CardNeedsYou {
		if out[taskID] == nil {
			out[taskID] = &CardNeedsYou{}
		}
		return out[taskID]
	}
	for _, item := range attention {
		if item.TaskID == "" {
			continue
		}
		if kind, ok := operatorItemKinds[item.SignalKind]; ok {
			e := entry(item.TaskID)
			if e.Items == nil {
				e.Items = map[string]int{}
			}
			e.Items[kind]++
			continue
		}
		if item.SignalKind == AttentionSignalPlanDecision {
			e := entry(item.TaskID)
			if e.Plans == nil {
				e.Plans = &NeedsYouPlans{PlanID: item.Navigation.Target}
			}
			e.Plans.Count++
		}
	}
	for _, approval := range approvals {
		if approval.TaskID == "" || approval.Status != "" && approval.Status != store.ApprovalPending {
			continue
		}
		e := entry(approval.TaskID)
		if e.Approvals == nil {
			e.Approvals = &NeedsYouApprovals{ID: approval.ID, Label: approvalLabel(approval)}
		}
		e.Approvals.Count++
	}
	return out
}

// approvalLabel is an approval as the strip says it: a question as asked,
// an agent's permission request as "permission to run a command", anything
// else by its title.
func approvalLabel(approval Approval) string {
	switch approval.Source {
	case ApprovalSourceAgentQuestion:
		return approval.Title
	case ApprovalSourceAgentPermission:
		return "permission to " + lowerFirst(approval.Title)
	}
	return lowerFirst(approval.Title)
}

func lowerFirst(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if size == 0 {
		return s
	}
	// Keep a word that is all capitals ("CI", "SSH") as it is.
	if word, _, _ := strings.Cut(s, " "); strings.ToUpper(word) == word && utf8.RuneCountInString(word) > 1 {
		return s
	}
	return string(unicode.ToLower(r)) + s[size:]
}
