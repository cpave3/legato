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
	// Slots are the task's running named slots (a reviewer), each a chip
	// beside the main slot's state. State is "waiting" when one of them
	// needs the person.
	Slots []SlotAgentSummary
}

// SlotAgentSummary is a running named slot's agent on a card.
type SlotAgentSummary struct {
	Slot  string `json:"slot"`
	State string `json:"state"`
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
			summary.Slots = append(summary.Slots, SlotAgentSummary{Slot: agent.Slot, State: agent.Activity})
			summaries[taskID] = summary
			continue
		}
		summary, exists := summaries[taskID]
		if exists && summary.Count == 0 {
			// Only named slots were seen so far.
			summary.State, summary.Kind, summary.Engram = agent.Activity, agent.AgentKind, agent.Engram
			summary.Count = 1
			summaries[taskID] = summary
			continue
		}
		if !exists {
			summary = RelatedAgentSummary{Active: true, State: agent.Activity, Kind: agent.AgentKind, Engram: agent.Engram}
		} else {
			if summary.State != agent.Activity {
				summary.State = AgentActivityMixed
			}
			if summary.Kind != agent.AgentKind {
				summary.Kind = ""
			}
			if summary.Engram != agent.Engram {
				summary.Engram = ""
			}
		}
		summary.Count++
		summaries[taskID] = summary
	}
	for taskID, summary := range summaries {
		for _, slot := range summary.Slots {
			if slot.State == store.ActivityWaiting {
				summary.State = store.ActivityWaiting
			}
		}
		summaries[taskID] = summary
	}
	return summaries
}
