package agents

import (
	"testing"

	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

// An agent waiting on the Reeve is listed under Working, as waiting on the
// Reeve; once unanswered, under Needs you, saying so (LEG-613). A named
// Reeve is named.
func TestAnAgentWaitingOnTheReeveSaysSoInTheList(t *testing.T) {
	waiting := service.AgentSession{TaskID: "LEG-613", Status: "running", Stage: "implementing", ReeveWait: service.ReeveWaitOpen}
	unanswered := waiting
	unanswered.TaskID, unanswered.ReeveWait = "LEG-614", service.ReeveWaitUnanswered
	m := New(theme.NewIcons("unicode"))
	m.SetAgents([]service.AgentSession{waiting, unanswered})
	if sectionOf(waiting) != sectionWorking || m.stateWord(stateOf(waiting)) != "waiting on the Reeve" {
		t.Fatalf("waiting: section %v, word %q", sectionOf(waiting), m.stateWord(stateOf(waiting)))
	}
	if sectionOf(unanswered) != sectionNeedsYou || m.stateWord(stateOf(unanswered)) != "the Reeve hasn't answered" {
		t.Fatalf("unanswered: section %v, word %q", sectionOf(unanswered), m.stateWord(stateOf(unanswered)))
	}

	reeve := service.AgentSession{TaskID: "rv-1", Status: "running", Reeve: true, ReeveName: "Hestia"}
	m.SetAgents([]service.AgentSession{waiting, unanswered, reeve})
	if got := m.stateWord(stateOf(waiting)); got != "waiting on Hestia" {
		t.Fatalf("named: %q", got)
	}
	if got := m.stateWord(stateOf(unanswered)); got != "Hestia hasn't answered" {
		t.Fatalf("named, unanswered: %q", got)
	}
}
