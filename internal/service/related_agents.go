package service

import "github.com/cpave3/legato/internal/engine/store"

const AgentActivityMixed = "mixed"

type RelatedAgentSummary struct {
	Active bool
	State  string
	// Kind is the agent tool the task's agents run, empty when they run
	// different ones.
	Kind string
	// Engram is the engram the task's agents run as, empty for none or
	// when they run as different ones.
	Engram string
	Count  int
	// MainState is the main-slot agents' own state, which a named slot
	// waiting does not change ("" when only named slots run). The web card
	// says the slot's wait in a strip of its own.
	MainState string
	// Slots are the task's running named slots (a reviewer), each a chip
	// beside the main slot's state. State is "waiting" when one of them
	// needs the person.
	Slots []SlotAgentSummary
	// Stage and StageReason are the main slot's agent's stage, "" when no
	// main agent has one. A stage never moves the card.
	Stage, StageReason string
	// Glance is the glance group (Glance) of the main slot's agent that
	// names the stage, else of the first main agent; "" when only named
	// slots run.
	Glance string
}

// SlotAgentSummary is a running named slot's agent on a card.
type SlotAgentSummary struct {
	Slot  string `json:"slot"`
	State string `json:"state"`
	// Stage is the slot's agent's stage, "" for none.
	Stage string `json:"stage,omitempty"`
}

// SummarizeRelatedAgents groups running sessions by their user-facing task.
func SummarizeRelatedAgents(agents []AgentSession) map[string]RelatedAgentSummary {
	summaries := make(map[string]RelatedAgentSummary)
	for _, agent := range agents {
		if agent.Status != "running" {
			continue
		}
		taskID := agent.AttributionTaskID
		if taskID == "" {
			taskID = agent.TaskID
		}
		if taskID == "" {
			continue
		}
		if IsNamedSlot(agent.Slot) {
			if agent.CardTaskID != "" {
				taskID = agent.CardTaskID
			}
			summary := summaries[taskID]
			summary.Active = true
			summary.Slots = append(summary.Slots, SlotAgentSummary{Slot: agent.Slot, State: agent.ShownActivity(), Stage: agent.Stage})
			summaries[taskID] = summary
			continue
		}
		summary, exists := summaries[taskID]
		if exists && summary.Count == 0 {
			// Only named slots were seen so far.
			summary.State, summary.Kind, summary.Engram = agent.ShownActivity(), agent.AgentKind, agent.Engram
			summary.Stage, summary.StageReason = agent.Stage, agent.StageReason
			summary.Glance = agent.Glance()
			summary.Count = 1
			summaries[taskID] = summary
			continue
		}
		if !exists {
			summary = RelatedAgentSummary{Active: true, State: agent.ShownActivity(), Kind: agent.AgentKind, Engram: agent.Engram, Glance: agent.Glance()}
		} else {
			if summary.State != agent.ShownActivity() {
				summary.State = AgentActivityMixed
			}
			if summary.Kind != agent.AgentKind {
				summary.Kind = ""
			}
			if summary.Engram != agent.Engram {
				summary.Engram = ""
			}
		}
		// The card's own main agent names the stage, else the first with one.
		if agent.Stage != "" && (summary.Stage == "" || agent.CardTaskID == taskID || agent.TaskID == taskID) {
			summary.Stage, summary.StageReason = agent.Stage, agent.StageReason
			summary.Glance = agent.Glance()
		}
		summary.Count++
		summaries[taskID] = summary
	}
	for taskID, summary := range summaries {
		summary.MainState = summary.State
		for _, slot := range summary.Slots {
			if slot.State == store.ActivityWaiting {
				summary.State = store.ActivityWaiting
			}
		}
		summaries[taskID] = summary
	}
	return summaries
}
