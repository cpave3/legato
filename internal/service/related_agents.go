package service

const AgentActivityMixed = "mixed"

type RelatedAgentSummary struct {
	Active bool
	State  string
	// Kind is the agent tool the task's agents run, empty when they run
	// different ones.
	Kind  string
	Count int
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
		summary, exists := summaries[taskID]
		if !exists {
			summary = RelatedAgentSummary{Active: true, State: agent.Activity, Kind: agent.AgentKind}
		} else {
			if summary.State != agent.Activity {
				summary.State = AgentActivityMixed
			}
			if summary.Kind != agent.AgentKind {
				summary.Kind = ""
			}
		}
		summary.Count++
		summaries[taskID] = summary
	}
	return summaries
}
