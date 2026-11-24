package server

import (
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// An agent on a workflow run's own task names the card the run is for
// (LEG-613): the run's task is named after the run.
func TestAnAgentOnARunsTaskNamesTheRunsCard(t *testing.T) {
	srv := agentsServer()
	list := srv.agents.(*manyAgents).list
	list[0].RunCard = &service.RunCard{TaskID: "c9x2k1m4", Key: "LEG-9", Title: "Fix login"}

	full := getAgents(t, srv, "?task=T-1,T-2")
	card, _ := full[0]["run_card"].(map[string]any)
	if card["task_id"] != "c9x2k1m4" || card["key"] != "LEG-9" || card["title"] != "Fix login" {
		t.Fatalf("run_card = %v", full[0]["run_card"])
	}
	if _, ok := full[1]["run_card"]; ok {
		t.Fatalf("an agent on no run's task has one: %v", full[1])
	}
}
