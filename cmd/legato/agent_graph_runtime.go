package main

import (
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// agentLinksChanged is the IPC message a process sends when the links of
// the web UI's graph of agents changed (a peer message was sent): each
// Legato serving the web UI tells its clients to read the graph again.
const agentLinksChanged = "agent_links_changed"

// peerMessageSent tells every Legato process that one agent messaged
// another, so the graph draws the link at once. The agent service calls it
// from whichever process sent the message: the CLI, `legato mcp serve`, or
// a Legato itself.
func peerMessageSent() {
	ipc.Broadcast(ipc.Message{Type: agentLinksChanged})
}

// agentGraphFor is the service behind GET /api/agents/graph; nil without
// agents. workflows, when not nil, says which runs need the operator.
func agentGraphFor(db *store.Store, agents service.AgentService, workflows *service.WorkflowService) *service.AgentGraphService {
	if agents == nil {
		return nil
	}
	opts := service.AgentGraphOptions{
		Store: db, Agents: agents, Related: service.NewRelatedAgentsService(db), Inspector: checkoutInspector(),
	}
	if workflows != nil {
		opts.Runs = workflows
	}
	return service.NewAgentGraphService(opts)
}
