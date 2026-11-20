package server

import (
	"context"
	"errors"
	"testing"
)

// operatorTurnAgents records the messages it is told the operator sent.
type operatorTurnAgents struct {
	promptAgentService
	noted []string
}

func (a *operatorTurnAgents) RecordOperatorMessage(_ context.Context, taskID, text string) error {
	a.noted = append(a.noted, taskID+": "+text)
	return nil
}

// A message the operator types in the web UI is their turn, for an agent
// tool whose own reports carry no text: the agent service is told once it
// is delivered, and not when it failed.
func TestTheOperatorsWebMessageIsNotedOnceDelivered(t *testing.T) {
	agents := &operatorTurnAgents{}
	s := New(nil, agents, nil, "")
	client := &wsClient{ctx: context.Background(), sendFn: func(WSMessage) {}}

	s.handleWSMessage(client, WSMessage{Type: MsgAgentMessage, AgentID: "task-1", Content: "Use Postgres", Queue: true})
	agents.queueErr = errors.New("the bridge is gone")
	s.handleWSMessage(client, WSMessage{Type: MsgAgentMessage, AgentID: "task-1", Content: "Not delivered", Queue: true})

	if len(agents.noted) != 1 || agents.noted[0] != "task-1: Use Postgres" {
		t.Fatalf("noted = %q", agents.noted)
	}
}
