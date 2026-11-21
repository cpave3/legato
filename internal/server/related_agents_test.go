package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

type fakeRelatedAgents struct {
	rows  []service.RelatedAgent
	asked []string
}

func (f *fakeRelatedAgents) RelatedAgents(_ context.Context, agentID string) ([]service.RelatedAgent, error) {
	f.asked = append(f.asked, agentID)
	return f.rows, nil
}

func TestAnAgentsRelatedAgentsAreListedByItsAgentID(t *testing.T) {
	related := &fakeRelatedAgents{rows: []service.RelatedAgent{{AgentID: "card-1", TaskID: "card-1", Role: service.RelatedRoleAuthor, Status: service.RelatedAgentRunning}}}
	srv := New(nil, nil, nil, ":0")
	srv.SetRelatedAgentsService(related)

	var listed struct {
		RelatedAgents []service.RelatedAgent `json:"related_agents"`
	}
	if status := serveJSON(t, srv, http.MethodGet, "/api/agents/card-1_reviewer/related", "", &listed); status != http.StatusOK ||
		len(listed.RelatedAgents) != 1 || listed.RelatedAgents[0].AgentID != "card-1" || related.asked[0] != "card-1_reviewer" {
		t.Fatalf("status %d listed %+v asked %v", status, listed, related.asked)
	}
}

func TestRelatedAgentsAreAnEmptyListWithoutTheService(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	var listed struct {
		RelatedAgents []service.RelatedAgent `json:"related_agents"`
	}
	if status := serveJSON(t, srv, http.MethodGet, "/api/agents/t1/related", "", &listed); status != http.StatusOK || listed.RelatedAgents == nil || len(listed.RelatedAgents) != 0 {
		t.Fatalf("status %d listed %+v", status, listed)
	}
}
