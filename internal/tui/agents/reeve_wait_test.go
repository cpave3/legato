package agents

import (
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// An agent waiting on the Reeve is listed under Working, as waiting on the
// Reeve; once unanswered, under Needs you, saying so (LEG-613).
func TestAnAgentWaitingOnTheReeveSaysSoInTheList(t *testing.T) {
	waiting := service.AgentSession{TaskID: "LEG-613", Status: "running", Stage: "implementing", ReeveWait: service.ReeveWaitOpen}
	if sectionOf(waiting) != sectionWorking || stateWord(stateOf(waiting)) != "waiting on Reeve" {
		t.Fatalf("waiting: section %v, word %q", sectionOf(waiting), stateWord(stateOf(waiting)))
	}
	unanswered := waiting
	unanswered.ReeveWait = service.ReeveWaitUnanswered
	if sectionOf(unanswered) != sectionNeedsYou || stateWord(stateOf(unanswered)) != "Reeve hasn't answered" {
		t.Fatalf("unanswered: section %v, word %q", sectionOf(unanswered), stateWord(stateOf(unanswered)))
	}
}
