package agents

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

func reeveAgents() []service.AgentSession {
	return []service.AgentSession{
		{ID: 1, TaskID: "T-1", Title: "Fix the sync", AgentKind: "claude-code", Status: "running", Activity: "waiting"},
		{ID: 2, TaskID: "rv-1", Title: "The Reeve", AgentKind: "claude-code", Status: "running", Reeve: true},
		{ID: 3, TaskID: "T-2", Title: "Docs", AgentKind: "codex", Status: "running", Activity: "working"},
	}
}

// The Reeve is pinned on the first row, above the state groups, which do
// not count it.
func TestTheReeveIsPinnedAboveTheStateGroups(t *testing.T) {
	m := New(theme.NewIcons("unicode"))
	m.SetAgents(reeveAgents())
	m.SetSize(120, 40)
	if m.agents[0].TaskID != "rv-1" {
		t.Fatalf("first row %q", m.agents[0].TaskID)
	}
	out := m.View()
	reeve, needs := strings.Index(out, "Reeve"), strings.Index(out, "NEEDS YOU · 1")
	if reeve < 0 || needs < 0 || reeve > needs {
		t.Fatalf("the Reeve is not above the groups:\n%s", out)
	}
	if strings.Contains(out, "IDLE") {
		t.Fatalf("the Reeve was counted in a group:\n%s", out)
	}
}

// a asks the Reeve about the selected agent; on the Reeve itself it does
// nothing.
func TestAAsksTheReeveAboutTheSelectedAgent(t *testing.T) {
	m := New(theme.NewIcons("unicode"))
	m.SetAgents(reeveAgents())
	m.SetSize(120, 40)
	m.SetReeveEnabled(true)
	m.selected = 1 // T-1, which needs the person
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd == nil {
		t.Fatal("no command from a")
	}
	ask, ok := cmd().(AskReeveMsg)
	if !ok || ask.TaskID != "T-1" || ask.Title != "Fix the sync" || ask.AgentKind != "claude-code" || ask.State != "needs you" {
		t.Fatalf("got %#v", ask)
	}
	m.selected = 0
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}); cmd != nil {
		t.Fatalf("a on the Reeve gave %#v", cmd())
	}
}

// A named Reeve's row says its name where an unnamed one says Reeve.
func TestANamedReevesRowSaysItsName(t *testing.T) {
	m := New(theme.NewIcons("unicode"))
	list := reeveAgents()
	list[1].ReeveName, list[1].Title = "Tessitura", "Tessitura"
	m.SetAgents(list)
	m.SetSize(120, 40)
	first := m.renderSidebarEntry(m.agents[0], true, 60)[0]
	if !strings.Contains(first, "Tessitura") || strings.Contains(first, "Reeve") {
		t.Fatalf("first line of the Reeve's row: %q", first)
	}
	if m.ReeveName() != "Tessitura" {
		t.Fatalf("ReeveName = %q", m.ReeveName())
	}
}
