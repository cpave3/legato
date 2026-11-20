package server

import "testing"

// The agents list, full and compact, gives each agent's open operator
// items beside its activity, which it leaves as the agent tool said.
func TestTheAgentsSayHowManyOperatorItemsTheyHaveOpen(t *testing.T) {
	srv := agentsServer()
	list := srv.agents.(*manyAgents).list
	list[0].OpenItems = 2
	list[0].Activity = ""

	for _, view := range []string{"?task=T-1", "?view=compact&task=T-1"} {
		got := getAgents(t, srv, view)
		if got[0]["open_items"] != float64(2) || got[0]["activity"] != "" {
			t.Fatalf("%s: open_items = %v, activity = %v", view, got[0]["open_items"], got[0]["activity"])
		}
	}
}
