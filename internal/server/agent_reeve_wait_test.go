package server

import (
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
)

// An agent that waits on the Reeve says so, with since when; one past the
// wait needs the operator (LEG-613).
func TestAnAgentWaitingOnTheReeveSaysSo(t *testing.T) {
	srv := agentsServer()
	list := srv.agents.(*manyAgents).list
	since := time.Date(2026, 11, 24, 5, 0, 0, 0, time.UTC)
	list[0].Activity, list[0].Stage = "", "implementing"
	list[0].ReeveWait, list[0].ReeveWaitSince = service.ReeveWaitOpen, &since

	full := getAgents(t, srv, "?task=T-1,T-2")
	if full[0]["waiting_on"] != "reeve" || full[0]["waiting_since"] != "2026-11-24T05:00:00Z" || full[0]["glance"] != "working" {
		t.Fatalf("agent = waiting_on %v, waiting_since %v, glance %v", full[0]["waiting_on"], full[0]["waiting_since"], full[0]["glance"])
	}
	if _, ok := full[1]["waiting_on"]; ok {
		t.Fatalf("an agent waiting on no one: %v", full[1])
	}

	list[0].ReeveWait = service.ReeveWaitUnanswered
	if got := getAgents(t, srv, "?task=T-1")[0]; got["waiting_on"] != "reeve" || got["glance"] != "needs_you" {
		t.Fatalf("unanswered: waiting_on %v, glance %v", got["waiting_on"], got["glance"])
	}
}
