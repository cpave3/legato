package agents

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

func stagedAgents() []service.AgentSession {
	return []service.AgentSession{
		{ID: 1, TaskID: "LEG-409", Status: "running", Activity: "waiting", Stage: "implementing", StageReason: "6 files changed"},
		{ID: 2, TaskID: "LEG-386", Status: "running", Stage: "implementing", StageReason: "fixing review feedback · pass 2"},
		{ID: 3, TaskID: "LEG-407", Status: "running", Activity: "working", Stage: "implementing", StageReason: "9 files changed"},
		{ID: 4, TaskID: "LEG-408", Status: "running", Activity: "background", Stage: "reviewing", StageReason: "PR #214 open"},
		{ID: 5, TaskID: "LEG-405", Status: "running", Stage: "finished", StageReason: "PR #212 merged"},
		{ID: 6, TaskID: "LEG-401", Status: "dead"},
	}
}

func stagedModel() Model {
	m := New(theme.NewIcons("unicode"))
	m.SetAgents(stagedAgents())
	m.SetSize(140, 50)
	return m
}

func listedIDs(m Model) []string {
	var ids []string
	for _, a := range m.agents {
		ids = append(ids, a.TaskID)
	}
	return ids
}

// The list groups agents as every surface does: needs you, then your turn,
// working, finished, ended.
func TestTheListGroupsAgentsByGlance(t *testing.T) {
	m := stagedModel()
	view := m.View()
	order := []string{"NEEDS YOU · 1", "YOUR TURN · 1", "WORKING · 2", "FINISHED · 1", "ENDED · 1"}
	last := -1
	for _, heading := range order {
		at := strings.Index(view, heading)
		if at < 0 || at < last {
			t.Fatalf("headings out of order or missing (%q): %q", heading, view)
		}
		last = at
	}
	if got := fmt.Sprint(listedIDs(m)); got != "[LEG-409 LEG-386 LEG-407 LEG-408 LEG-405 LEG-401]" {
		t.Fatalf("order = %s", got)
	}
}

// Each entry carries its stage's glyph and why.
func TestEachEntryCarriesItsStage(t *testing.T) {
	view := stagedModel().View()
	for _, want := range []string{"◑ implementing · fixing review f", "◕ reviewing · PR #214 open", "● finished · PR #212 merged"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q", want)
		}
	}
}

// f cycles the stage filter: all, each stage, then all again.
func TestFCyclesTheStageFilter(t *testing.T) {
	m := stagedModel()
	f := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}}
	var seen []string
	for range 6 {
		m, _ = m.Update(f)
		seen = append(seen, m.stageFilter+":"+fmt.Sprint(len(m.agents)))
	}
	if got := strings.Join(seen, " "); got != "exploring:0 planning:0 implementing:3 reviewing:1 finished:1 :6" {
		t.Fatalf("cycle = %s", got)
	}

	m, _ = m.Update(f)
	m, _ = m.Update(f)
	m, _ = m.Update(f)
	m, _ = m.Update(f)
	if m.stageFilter != "reviewing" {
		t.Fatalf("filter = %q", m.stageFilter)
	}
	view := m.View()
	if !strings.Contains(view, "stage: reviewing") || !strings.Contains(view, "1 needs you outside this filter") {
		t.Fatalf("a filtered list says so, and what needs you outside it: %q", view)
	}
	// A refresh keeps the filter.
	m, _ = m.Update(AgentsRefreshedMsg{Agents: stagedAgents()})
	if fmt.Sprint(listedIDs(m)) != "[LEG-408]" {
		t.Fatalf("after a refresh = %v", listedIDs(m))
	}
}

// The ensemble's record lists stage changes beside state changes, and its
// tiles name their stage.
func TestTheEnsembleRecordsStageChanges(t *testing.T) {
	m := stagedModel()
	m.now = func() time.Time { return time.Date(2026, 11, 22, 10, 0, 0, 0, time.Local) }
	changed := stagedAgents()
	changed[3].Stage, changed[3].StageReason = "finished", "PR #214 merged"
	m, _ = m.Update(AgentsRefreshedMsg{Agents: changed})

	var got []string
	for _, event := range m.feed {
		got = append(got, event.taskID+" "+event.text)
	}
	if fmt.Sprint(got) != "[LEG-408 finished · PR #214 merged]" {
		t.Fatalf("feed = %v", got)
	}

	m.SetSize(170, 44)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	view := m.View()
	if !strings.Contains(view, "◑ implementing") {
		t.Errorf("tiles should name their stage: %q", view)
	}
	if !strings.Contains(view, "1 needs you") || !strings.Contains(view, "1 your turn") {
		t.Errorf("the ensemble header counts by glance: %q", view)
	}
}

