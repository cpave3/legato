package board

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestThePanelNamesTheEngramTheTasksAgentRunsAs(t *testing.T) {
	m := sizedModel(160, 40)
	m.SetActiveAgents(map[string]bool{"REX-1": true})
	m.SetAgentStates(map[string]string{"REX-1": "working"})
	m.SetAgentEngrams(map[string]string{"REX-1": "hestia"})
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "engram") || !strings.Contains(view, "hestia") {
		t.Fatalf("the panel lacks the engram row:\n%s", view)
	}
	m.SetAgentEngrams(nil)
	if strings.Contains(ansi.Strip(m.View()), "hestia") {
		t.Fatal("an agent without an engram still shows one")
	}
}
