package server

import "testing"

// The agents list, full and compact, says where each agent was started and
// the job clone it works in (LEG-220).
func TestTheAgentsSayWhereTheyWereStarted(t *testing.T) {
	srv := agentsServer()
	list := srv.agents.(*manyAgents).list
	list[0].WorkingDir = "/home/me/jobs/ex-1/widget"
	list[0].CloneRepo = "acme/widget"
	list[0].CloneBranch = "legato/t-1"

	full := getAgents(t, srv, "?task=T-1,T-2")
	if full[0]["working_dir"] != "/home/me/jobs/ex-1/widget" {
		t.Fatalf("working_dir = %v", full[0]["working_dir"])
	}
	clone, _ := full[0]["clone"].(map[string]any)
	if clone["repo"] != "acme/widget" || clone["branch"] != "legato/t-1" {
		t.Fatalf("clone = %v", full[0]["clone"])
	}
	if _, ok := full[1]["clone"]; ok {
		t.Fatalf("an agent in no clone has one: %v", full[1])
	}
	if _, ok := full[1]["working_dir"]; ok {
		t.Fatalf("an agent with no recorded directory has one: %v", full[1])
	}

	compact := getAgents(t, srv, "?view=compact&task=T-1")
	if compact[0]["working_dir"] != "/home/me/jobs/ex-1/widget" {
		t.Fatalf("compact working_dir = %v", compact[0]["working_dir"])
	}
}
