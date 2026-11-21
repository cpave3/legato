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
	// Slots are the task's named slots (a reviewer) whose agent waits, each
	// with what it asks when it is an approval.
	Slots []NeedsYouSlot `json:"slots,omitempty"`
	// WaitingAgents of the task's Agents (main-slot agents, when more than
	// one runs) wait for the operator; WorkingAgents work.
	WaitingAgents int `json:"waiting_agents,omitempty"`
	WorkingAgents int `json:"working_agents,omitempty"`
	Agents        int `json:"agents,omitempty"`
	// Interrupted is the task's agent that a crash interrupted and that
	// can be resumed.
	Interrupted *NeedsYouInterrupted `json:"interrupted,omitempty"`
}

// NeedsYouSlot is a named slot whose agent waits: what it asks ("question",
// "permission to run a command") and its approval, when it is one.
type NeedsYouSlot struct {
	Slot       string `json:"slot"`
	Label      string `json:"label,omitempty"`
	ApprovalID string `json:"approval_id,omitempty"`
}

// NeedsYouInterrupted is an interrupted agent that can be resumed.
type NeedsYouInterrupted struct {
	AgentKind string `json:"agent_kind,omitempty"`
	Resumable bool   `json:"resumable"`
}

// NeedsYouSources is what NeedsYouByTask reads: Attention's open items,
// the pending approvals, the agent sessions and the interrupted agents.
type NeedsYouSources struct {
	Attention   []AttentionItem
	Approvals   []Approval
	Agents      []AgentSession
	Interrupted []InterruptedAgent
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

// NeedsYouByTask groups what waits on the operator by task, in one pass
// over each source: operator items and plans from Attention's open items,
// approvals from the pending ones (oldest first), named slots that wait
// (with their own approvals, which the main agent's do not count), how many
// of several main-slot agents wait, and interrupted agents that can be
// resumed. A task with nothing open has no entry.
func NeedsYouByTask(src NeedsYouSources) map[string]*CardNeedsYou {
	out := map[string]*CardNeedsYou{}
	entry := func(taskID string) *CardNeedsYou {
		if out[taskID] == nil {
			out[taskID] = &CardNeedsYou{}
		}
		return out[taskID]
	}
	for _, item := range src.Attention {
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

	// A named slot's approvals are its own.
	slotOf := map[string]string{}
	for _, agent := range src.Agents {
		if agent.Status == "running" && IsNamedSlot(agent.Slot) && agent.ExecutionID != "" {
			slotOf[agent.ExecutionID] = agent.Slot
		}
	}
	slotApproval := map[string]Approval{}
	for _, approval := range src.Approvals {
		if approval.TaskID == "" || approval.Status != "" && approval.Status != store.ApprovalPending {
			continue
		}
		if _, ok := slotOf[approval.ExecutionID]; ok {
			if _, seen := slotApproval[approval.ExecutionID]; !seen {
				slotApproval[approval.ExecutionID] = approval
			}
			continue
		}
		e := entry(approval.TaskID)
		if e.Approvals == nil {
			e.Approvals = &NeedsYouApprovals{ID: approval.ID, Label: approvalLabel(approval)}
		}
		e.Approvals.Count++
	}

	type mainCount struct{ all, waiting, working int }
	mains := map[string]*mainCount{}
	for _, agent := range src.Agents {
		if agent.Status != "running" {
			continue
		}
		taskID := agent.AttributionTaskID
		if taskID == "" {
			taskID = agent.TaskID
		}
		if IsNamedSlot(agent.Slot) {
			if agent.CardTaskID != "" {
				taskID = agent.CardTaskID
			}
			_, asks := slotApproval[agent.ExecutionID]
			if taskID == "" || agent.ShownActivity() != store.ActivityWaiting && !asks {
				continue
			}
			slot := NeedsYouSlot{Slot: agent.Slot}
			if approval, ok := slotApproval[agent.ExecutionID]; ok {
				slot.ApprovalID = approval.ID
				slot.Label = approvalLabel(approval)
				if approval.Source == ApprovalSourceAgentQuestion {
					slot.Label = "question"
				}
			}
			e := entry(taskID)
			e.Slots = append(e.Slots, slot)
			continue
		}
		if taskID == "" {
			continue
		}
		c := mains[taskID]
		if c == nil {
			c = &mainCount{}
			mains[taskID] = c
		}
		c.all++
		switch agent.ShownActivity() {
		case store.ActivityWaiting:
			c.waiting++
		case store.ActivityWorking:
			c.working++
		}
	}
	for taskID, c := range mains {
		if c.all > 1 && c.waiting > 0 && c.waiting < c.all {
			e := entry(taskID)
			e.Agents, e.WaitingAgents, e.WorkingAgents = c.all, c.waiting, c.working
		}
	}

	for _, agent := range src.Interrupted {
		if agent.TaskID == "" || !agent.Resumable {
			continue
		}
		entry(agent.TaskID).Interrupted = &NeedsYouInterrupted{AgentKind: agent.AgentKind, Resumable: true}
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
