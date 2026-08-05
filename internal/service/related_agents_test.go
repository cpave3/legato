package service

import "testing"

func TestSummarizeRelatedAgentsGroupsByAttributionAndDetectsMixedState(t *testing.T) {
	agents := []AgentSession{
		{TaskID: "workflow-b", AttributionTaskID: "task-a", Status: "running", Activity: "working"},
		{TaskID: "workflow-c", AttributionTaskID: "task-a", Status: "running", Activity: "waiting"},
		{TaskID: "task-b", AttributionTaskID: "task-b", Status: "running", Activity: "error"},
		{TaskID: "dead", AttributionTaskID: "task-a", Status: "dead", Activity: "error"},
	}

	summaries := SummarizeRelatedAgents(agents)
	if got := summaries["task-a"]; !got.Active || got.State != AgentActivityMixed || got.Count != 2 {
		t.Fatalf("task-a summary = %#v", got)
	}
	if got := summaries["task-b"]; !got.Active || got.State != "error" || got.Count != 1 {
		t.Fatalf("task-b summary = %#v", got)
	}
}

func TestSummarizeRelatedAgentsTreatsIdleAsAStateAndIsOrderIndependent(t *testing.T) {
	forward := []AgentSession{
		{TaskID: "task-a", Status: "running", Activity: ""},
		{TaskID: "workflow-b", AttributionTaskID: "task-a", Status: "running", Activity: "working"},
	}
	reverse := []AgentSession{forward[1], forward[0]}

	for _, agents := range [][]AgentSession{forward, reverse} {
		got := SummarizeRelatedAgents(agents)["task-a"]
		if !got.Active || got.State != AgentActivityMixed || got.Count != 2 {
			t.Fatalf("summary = %#v", got)
		}
	}
}

func TestSummarizeRelatedAgentsPreservesSharedAndIdleStates(t *testing.T) {
	tests := []struct {
		name   string
		agents []AgentSession
		state  string
	}{
		{name: "shared", agents: []AgentSession{{TaskID: "a", Status: "running", Activity: "waiting"}, {TaskID: "b", AttributionTaskID: "a", Status: "running", Activity: "waiting"}}, state: "waiting"},
		{name: "idle", agents: []AgentSession{{TaskID: "a", Status: "running"}, {TaskID: "b", AttributionTaskID: "a", Status: "running"}}, state: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SummarizeRelatedAgents(tt.agents)["a"]
			if !got.Active || got.State != tt.state || got.Count != 2 {
				t.Fatalf("summary = %#v", got)
			}
		})
	}
}
