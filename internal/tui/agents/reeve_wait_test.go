package agents

import (
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

// An agent waiting on the Reeve is listed under Working, as waiting on the
// Reeve; once unanswered, under Needs you, saying what it waits on and for
// how long (LEG-613, LEG-623). A named Reeve is named.
func TestAnAgentWaitingOnTheReeveSaysSoInTheList(t *testing.T) {
	waiting := service.AgentSession{TaskID: "LEG-613", Status: "running", Stage: "implementing", ReeveWait: service.ReeveWaitOpen}
	unanswered := waiting
	unanswered.TaskID, unanswered.ReeveWait = "LEG-614", service.ReeveWaitUnanswered
	asked := time.Now().Add(-20 * time.Minute)
	unanswered.ReeveWaitSince = &asked
	m := New(theme.NewIcons("unicode"))
	m.SetAgents([]service.AgentSession{waiting, unanswered})
	if sectionOf(waiting) != sectionWorking || m.stateWord(stateOf(waiting)) != "waiting on the Reeve" {
		t.Fatalf("waiting: section %v, word %q", sectionOf(waiting), m.stateWord(stateOf(waiting)))
	}
	if sectionOf(unanswered) != sectionNeedsYou || m.stateWord(stateOf(unanswered)) != "waiting on the Reeve" {
		t.Fatalf("unanswered: section %v, word %q", sectionOf(unanswered), m.stateWord(stateOf(unanswered)))
	}
	if got := m.stateDuration(unanswered); got != "20m" {
		t.Fatalf("unanswered for %q, want 20m since it asked", got)
	}

	reeve := service.AgentSession{TaskID: "rv-1", Status: "running", Reeve: true, ReeveName: "Hestia"}
	m.SetAgents([]service.AgentSession{waiting, unanswered, reeve})
	if got := m.stateWord(stateOf(waiting)); got != "waiting on Hestia" {
		t.Fatalf("named: %q", got)
	}
	if got := m.stateWord(stateOf(unanswered)); got != "waiting on Hestia" {
		t.Fatalf("named, unanswered: %q", got)
	}
}

// A filter that hides the Reeve keeps its name in the wait's words.
func TestAFilterThatHidesTheReeveKeepsItsName(t *testing.T) {
	waiting := service.AgentSession{TaskID: "LEG-613", Status: "running", Stage: "implementing", ReeveWait: service.ReeveWaitOpen}
	reeve := service.AgentSession{TaskID: "rv-1", Status: "running", Reeve: true, ReeveName: "Hestia"}
	m := New(theme.NewIcons("unicode"))
	m.SetAgents([]service.AgentSession{waiting, reeve})
	m.stageFilter = "implementing"
	m.agents = m.filtered()
	for _, a := range m.agents {
		if a.Reeve {
			t.Fatal("the filter did not hide the Reeve")
		}
	}
	if got := m.stateWord(stateOf(waiting)); got != "waiting on Hestia" {
		t.Fatalf("filtered: %q", got)
	}
}
