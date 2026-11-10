package overlay

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func engramPicker() SpawnEngramPicker {
	return SpawnEngramPicker{
		Enabled: true, PlaceKind: "repo", PlaceLabel: "github.com/cpave3/legato", Default: "argus",
		Engrams: []SpawnEngram{
			{Name: "mneme", Mark: "last"}, {Name: "argus", Mark: "default"}, {Name: "hestia"}, {Name: "clio"},
		},
		Preselect: "mneme",
	}
}

// engramSpawn is a task's spawn overlay in the default directory, with the
// engram picker for it answered.
func engramSpawn(t *testing.T) AgentSpawnOverlay {
	t.Helper()
	m := NewAgentSpawn([]string{"codex"}, "claude-code", "/home/me/Projects/legato", "T-1", "Fix it")
	next, _ := m.Update(AgentSpawnEngramsMsg{Dir: "/home/me/Projects/legato", Picker: engramPicker()})
	return next.(AgentSpawnOverlay)
}

func viewText(m AgentSpawnOverlay) string { return ansi.Strip(m.View()) }

func TestTheEngramRowFollowsTheAgentAndStartsOnThePreselection(t *testing.T) {
	m := engramSpawn(t)
	view := viewText(m)
	agent, engram := strings.Index(view, "agent"), strings.Index(view, "engram")
	if agent < 0 || engram < agent {
		t.Fatalf("the engram row does not follow the agent row:\n%s", view)
	}
	if !strings.Contains(view, "‹ mneme · last on this task ›  2/5") {
		t.Fatalf("engram row:\n%s", view)
	}
}

func TestTheEngramRowCyclesInPickerOrder(t *testing.T) {
	m := engramSpawn(t)
	m, _ = spawnKey(t, m, keyTab) // agent → engram
	m, _ = spawnKey(t, m, runes("l"))
	if !strings.Contains(viewText(m), "‹ argus · default ›  3/5") {
		t.Fatalf("after l:\n%s", viewText(m))
	}
	m, _ = spawnKey(t, m, runes("h"), runes("h"))
	if !strings.Contains(viewText(m), "‹ no engram ›  1/5") {
		t.Fatalf("after h h:\n%s", viewText(m))
	}
	// It stops at the ends.
	m, _ = spawnKey(t, m, runes("h"))
	if !strings.Contains(viewText(m), "1/5") {
		t.Fatalf("past the start:\n%s", viewText(m))
	}
}

func TestTheEngramIsSubmitted(t *testing.T) {
	m := engramSpawn(t)
	_, msg := spawnKey(t, m, keyEnter)
	submit, ok := msg.(AgentSpawnSubmitMsg)
	if !ok || submit.Engram != "mneme" || submit.RememberEngram {
		t.Fatalf("submit = %#v", msg)
	}
}

func TestRememberIsOfferedOnlyWhenItWouldChangeSomething(t *testing.T) {
	m := engramSpawn(t)
	if !strings.Contains(viewText(m), "[ ] remember for this repo (r)") {
		t.Fatalf("mneme is not the default, so remembering is offered:\n%s", viewText(m))
	}
	m, _ = spawnKey(t, m, keyTab, runes("r"))
	if !strings.Contains(viewText(m), "[x] remember for this repo (r)") {
		t.Fatalf("r did not tick it:\n%s", viewText(m))
	}
	_, msg := spawnKey(t, m, keyEnter)
	if submit := msg.(AgentSpawnSubmitMsg); !submit.RememberEngram || submit.Engram != "mneme" {
		t.Fatalf("submit = %#v", submit)
	}
	// The default: nothing to remember, and a new pick unticks the box.
	m, _ = spawnKey(t, m, runes("l"))
	if strings.Contains(viewText(m), "remember for this repo") {
		t.Fatalf("remembering the default is offered:\n%s", viewText(m))
	}
	m, _ = spawnKey(t, m, runes("h"), runes("h"))
	if !strings.Contains(viewText(m), "[ ] clear this repo's default (r)") {
		t.Fatalf("no engram with a default offers to clear it:\n%s", viewText(m))
	}
	m, _ = spawnKey(t, m, runes("r"))
	_, msg = spawnKey(t, m, keyEnter)
	if submit := msg.(AgentSpawnSubmitMsg); !submit.RememberEngram || submit.Engram != "" {
		t.Fatalf("submit = %#v", submit)
	}
}

func TestRIsOnlyRememberOnTheEngramRow(t *testing.T) {
	m := NewAgentSpawn(nil, "claude-code", "/x", "", "")
	next, _ := m.Update(AgentSpawnEngramsMsg{Dir: "/x", Picker: engramPicker()})
	m = next.(AgentSpawnOverlay)
	m, _ = spawnKey(t, m, runes("r"))
	if m.title != "r" {
		t.Fatalf("r in the title field typed %q", m.title)
	}
}

func TestTheEngramRowIsHiddenForAShellAndWhenEngramIsOff(t *testing.T) {
	m := engramSpawn(t)
	// claude-code, codex, shell
	m, _ = spawnKey(t, m, runes("l"), runes("l"))
	if strings.Contains(viewText(m), "engram") {
		t.Fatalf("a shell is offered an engram:\n%s", viewText(m))
	}
	_, msg := spawnKey(t, m, keyEnter)
	if submit := msg.(AgentSpawnSubmitMsg); submit.Engram != "" {
		t.Fatalf("a shell was started as %q", submit.Engram)
	}
	off := NewAgentSpawn(nil, "claude-code", "/x", "T-1", "t")
	next, _ := off.Update(AgentSpawnEngramsMsg{Dir: "/x", Picker: SpawnEngramPicker{}})
	if strings.Contains(viewText(next.(AgentSpawnOverlay)), "engram") {
		t.Fatal("the engram row shows with engrams off")
	}
}

func TestAnotherPlaceAsksForItsEngrams(t *testing.T) {
	m := NewAgentSpawn(nil, "claude-code", "/home/me/Development/autopilot", "T-1", "t").
		WithLocations(SpawnDir{Path: "/home/me/Development/autopilot"}, spawnPresets())
	next, _ := m.Update(AgentSpawnEngramsMsg{Dir: "/home/me/Development/autopilot", Picker: engramPicker()})
	m = next.(AgentSpawnOverlay)
	m, _ = spawnKey(t, m, keyTab, keyTab) // engram → where
	_, msg := spawnKey(t, m, keyDown)
	wanted, ok := msg.(AgentSpawnEngramsWantedMsg)
	if !ok || wanted.Dir != "/home/me/Projects/legato" || wanted.TaskID != "T-1" {
		t.Fatalf("moving to another preset asked %#v", msg)
	}
	// An answer for another place is dropped.
	stale := engramPicker()
	stale.Preselect = "clio"
	next, _ = m.Update(AgentSpawnEngramsMsg{Dir: "/elsewhere", Picker: stale})
	if strings.Contains(viewText(next.(AgentSpawnOverlay)), "‹ clio") {
		t.Fatal("an answer for another place was taken")
	}
}

var _ = tea.KeyMsg{}
