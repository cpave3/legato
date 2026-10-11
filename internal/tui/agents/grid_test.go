package agents

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/hotkey"
	"github.com/cpave3/legato/internal/tui/theme"
)

func key(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

// ensemble is the agents view switched to its grid, at a size that has
// three tiles in a row.
func ensemble(t *testing.T) Model {
	t.Helper()
	m := New(theme.NewIcons("unicode"))
	m.SetAgents(mixedAgents())
	m.SetSize(170, 44)
	m, _ = m.Update(key('e'))
	if m.Layout() != LayoutGrid {
		t.Fatalf("e should switch to the ensemble, layout = %v", m.Layout())
	}
	return m
}

func TestEChangesBetweenTheListAndTheEnsemble(t *testing.T) {
	m := ensemble(t)
	if m.HotkeyMode() != hotkey.ModeGrid {
		t.Errorf("keys of the ensemble should apply, mode = %q", m.HotkeyMode())
	}
	if view := m.View(); !strings.Contains(view, "ensemble") || !strings.Contains(view, "ACTIVITY") {
		t.Errorf("the ensemble should name itself and show its activity: %q", view)
	}

	m, _ = m.Update(key('e'))
	if m.Layout() != LayoutList || m.HotkeyMode() != hotkey.ModeList {
		t.Errorf("e should go back to the list, layout = %v", m.Layout())
	}
}

func TestEnsembleShowsEveryAgentAsATile(t *testing.T) {
	m := ensemble(t)
	m.SetDurations(map[string]DurationData{"busy-1": {Working: 3*time.Hour + 34*time.Minute}})

	view := m.View()

	for _, want := range []string{
		"ask-1 codex", "NEEDS YOU", "rf dominion",
		"busy-1 chimera", "3h 34m", "Uplift",
		"idle-1 claude", "IDLE", "Legato app",
		"fail-1", "ERROR", "back-1", "BACKGROUND", "gone-1", "ENDED",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the ensemble should contain %q: %q", want, view)
		}
	}
}

func TestEnsembleCountsAgentsByWhatTheyAreDoing(t *testing.T) {
	header := strings.Split(ensemble(t).View(), "\n")[0]

	for _, want := range []string{"♩ legato", "2 need you", "1 running", "1 in background", "1 idle", "1 ended"} {
		if !strings.Contains(header, want) {
			t.Errorf("header should contain %q, got: %q", want, header)
		}
	}
}

func TestTileShowsTheEndOfWhatTheAgentsTerminalShows(t *testing.T) {
	m := ensemble(t)

	m, _ = m.Update(PreviewMsg{TaskID: "busy-1", Output: "old line\n\x1b[32m✓ 632 passed\x1b[0m\n$ pnpm build\n\n\n"})

	view := m.View()
	if !strings.Contains(view, "✓ 632 passed") || !strings.Contains(view, "$ pnpm build") {
		t.Errorf("the tile should show the last lines of the terminal: %q", view)
	}
	if strings.Contains(view, "\x1b[32m") {
		t.Errorf("the terminal's own colors should not reach the tile")
	}
}

func TestEnsembleOffersToSpawnWhereThereIsRoom(t *testing.T) {
	m := New(theme.NewIcons("unicode"))
	m.SetAgents(mixedAgents()[:2])
	m.SetSize(170, 44)
	m, _ = m.Update(key('e'))

	if view := m.View(); !strings.Contains(view, "spawn agent") {
		t.Errorf("the ensemble should offer to spawn an agent: %q", view)
	}
}

func TestEnsembleFillsExactlyTheSizeItWasGiven(t *testing.T) {
	for _, size := range []struct{ width, height int }{{170, 44}, {110, 30}, {70, 20}} {
		m := New(theme.NewIcons("unicode"))
		m.SetAgents(mixedAgents())
		m.SetSize(size.width, size.height)
		m, _ = m.Update(key('e'))

		lines := strings.Split(m.View(), "\n")

		if len(lines) != size.height {
			t.Errorf("%dx%d: view is %d lines tall", size.width, size.height, len(lines))
		}
		for i, line := range lines {
			if w := lipgloss.Width(line); w != size.width {
				t.Errorf("%dx%d: line %d is %d wide: %q", size.width, size.height, i, w, line)
				break
			}
		}
	}
}

func TestEnsembleMovesTheSelectionAlongRowsAndColumns(t *testing.T) {
	m := ensemble(t) // six agents, three to a row
	at := func() string { return m.SelectedAgent().TaskID }
	press := func(r rune) { m, _ = m.Update(key(r)) }
	order := taskIDs(m.Agents())

	press('l')
	if at() != order[1] {
		t.Fatalf("l selected %s, want %s", at(), order[1])
	}
	press('j')
	if at() != order[4] {
		t.Fatalf("j selected %s, want %s, the tile below", at(), order[4])
	}
	press('h')
	if at() != order[3] {
		t.Fatalf("h selected %s, want %s", at(), order[3])
	}
	press('h')
	if at() != order[3] {
		t.Fatalf("h at the start of a row selected %s, want it to stay on %s", at(), order[3])
	}
	press('k')
	if at() != order[0] {
		t.Fatalf("k selected %s, want %s, the tile above", at(), order[0])
	}
	press('k')
	if at() != order[0] {
		t.Fatalf("k in the first row selected %s, want it to stay on %s", at(), order[0])
	}
}

func TestEnsembleKeepsTheSelectedTileInView(t *testing.T) {
	var many []service.AgentSession
	for i := 0; i < 20; i++ {
		many = append(many, service.AgentSession{TaskID: fmt.Sprintf("agent-%02d", i), Status: "running"})
	}
	m := New(theme.NewIcons("unicode"))
	m.SetAgents(many)
	m.SetSize(170, 30)
	m, _ = m.Update(key('e'))
	m.SelectByTaskID("agent-19")

	if view := m.View(); !strings.Contains(view, "agent-19") || strings.Contains(view, "agent-00") {
		t.Errorf("the grid should scroll to the selected tile: %q", view)
	}
}

func TestActivityRecordsWhatChangesAfterTheFirstLook(t *testing.T) {
	m := New(theme.NewIcons("unicode"))
	m.now = func() time.Time { return time.Date(2026, 10, 11, 17, 12, 0, 0, time.Local) }
	m.SetAgents(mixedAgents())
	if len(m.feed) != 0 {
		t.Fatalf("what was already there is not news: %+v", m.feed)
	}

	changed := mixedAgents()
	changed[0].Activity = "waiting"                                                     // idle-1 now asks
	changed = append(changed[:2], changed[3:]...)                                       // gone-1 was removed
	changed = append(changed, service.AgentSession{TaskID: "new-1", Status: "running"}) // new-1 appeared
	m, _ = m.Update(AgentsRefreshedMsg{Agents: changed})

	var got []string
	for _, event := range m.feed {
		got = append(got, event.taskID+" "+event.text)
	}
	want := []string{"idle-1 needs you", "new-1 started", "gone-1 closed"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("feed = %v, want %v", got, want)
	}

	m.SetSize(170, 44)
	m, _ = m.Update(key('e'))
	if view := m.View(); !strings.Contains(view, "17:12") || !strings.Contains(view, "idle-1 needs you") {
		t.Errorf("the ensemble should show the activity with its time: %q", view)
	}
}

func TestPreviewsAreAskedForOnlyInTheEnsembleAndNotTooOften(t *testing.T) {
	start := time.Date(2026, 10, 11, 17, 0, 0, 0, time.Local)
	list := newTestModel()
	if list.PreviewsDue(start) {
		t.Fatal("the list shows one terminal; it needs no previews")
	}

	m := ensemble(t)
	if !m.PreviewsDue(start) {
		t.Fatal("the ensemble should ask for previews at once")
	}
	m.PreviewsRequested(start)
	if m.PreviewsDue(start.Add(300 * time.Millisecond)) {
		t.Fatal("previews were just asked for")
	}
	if !m.PreviewsDue(start.Add(2 * time.Second)) {
		t.Fatal("previews should be asked for again after a while")
	}
}
