package agents

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

func testAgents() []service.AgentSession {
	return []service.AgentSession{
		{ID: 1, TaskID: "REX-1238", TmuxSession: "legato-REX-1238", Command: "shell", Status: "running", StartedAt: time.Now().Add(-5 * time.Minute)},
		{ID: 2, TaskID: "REX-1239", TmuxSession: "legato-REX-1239", Command: "shell", Status: "running", StartedAt: time.Now().Add(-12 * time.Minute)},
	}
}

func newTestModel() Model {
	m := New(theme.NewIcons("unicode"))
	m.SetAgents(testAgents())
	m.SetSize(120, 40)
	return m
}

func TestNavigationJK(t *testing.T) {
	m := newTestModel()

	if m.selected != 0 {
		t.Fatalf("initial selected = %d, want 0", m.selected)
	}

	// j moves down
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.selected != 1 {
		t.Errorf("after j: selected = %d, want 1", m.selected)
	}

	// j at bottom stays
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.selected != 1 {
		t.Errorf("after j at bottom: selected = %d, want 1", m.selected)
	}

	// k moves up
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m.selected != 0 {
		t.Errorf("after k: selected = %d, want 0", m.selected)
	}

	// k at top stays
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m.selected != 0 {
		t.Errorf("after k at top: selected = %d, want 0", m.selected)
	}
}

func TestSpawnKeybinding(t *testing.T) {
	m := newTestModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd == nil {
		t.Fatal("expected command from 's' key")
	}
	msg := cmd()
	if _, ok := msg.(OpenAgentSpawnMsg); !ok {
		t.Errorf("expected OpenAgentSpawnMsg, got %T", msg)
	}
}

func TestKillKeybinding(t *testing.T) {
	m := newTestModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	if cmd == nil {
		t.Fatal("expected command from 'X' key")
	}
	msg := cmd()
	kill, ok := msg.(KillAgentMsg)
	if !ok {
		t.Fatalf("expected KillAgentMsg, got %T", msg)
	}
	if kill.TaskID != "REX-1238" {
		t.Errorf("TaskID = %q, want %q", kill.TaskID, "REX-1238")
	}
}

func TestKillNoAgentNoCmd(t *testing.T) {
	m := New(theme.NewIcons("unicode"))
	m.SetSize(120, 40)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	if cmd != nil {
		t.Error("expected nil command when no agents")
	}
}

func TestEnterAttachKeybinding(t *testing.T) {
	m := newTestModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command from enter key")
	}
	msg := cmd()
	attach, ok := msg.(AttachSessionMsg)
	if !ok {
		t.Fatalf("expected AttachSessionMsg, got %T", msg)
	}
	if attach.TmuxSession != "legato-REX-1238" {
		t.Errorf("TmuxSession = %q, want %q", attach.TmuxSession, "legato-REX-1238")
	}
}

func TestEscReturnsToBoard(t *testing.T) {
	m := newTestModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected command from esc key")
	}
	msg := cmd()
	if _, ok := msg.(ReturnToBoardMsg); !ok {
		t.Errorf("expected ReturnToBoardMsg, got %T", msg)
	}
}

func TestCaptureOutputMsgUpdatesContent(t *testing.T) {
	m := newTestModel()
	m, _ = m.Update(CaptureOutputMsg{Output: "hello world\n$ "})
	if m.termContent != "hello world\n$ " {
		t.Errorf("termContent = %q, want %q", m.termContent, "hello world\n$ ")
	}
}

func TestAgentsRefreshedMsgUpdatesAgents(t *testing.T) {
	m := newTestModel()
	m.selected = 1 // select second agent

	// Refresh with only one agent
	m, _ = m.Update(AgentsRefreshedMsg{
		Agents: []service.AgentSession{
			{ID: 1, TaskID: "REX-1238", TmuxSession: "legato-REX-1238", Command: "shell", Status: "running", StartedAt: time.Now()},
		},
	})
	if len(m.agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(m.agents))
	}
	if m.selected != 0 {
		t.Errorf("selected = %d, want 0 (clamped)", m.selected)
	}
}

// TestAgentsRefreshedMsgPreservesSelectionAcrossReorder ensures that when
// a refresh reorders the slice (e.g. a swarm agent is now grouped ahead of
// the previously-selected solo), the visual selection follows the same
// agent by TaskID rather than staying on the same index.
func TestAgentsRefreshedMsgPreservesSelectionAcrossReorder(t *testing.T) {
	m := newTestModel()
	now := time.Now()

	// First refresh: only one solo agent, selected at index 0.
	m, _ = m.Update(AgentsRefreshedMsg{
		Agents: []service.AgentSession{
			{ID: 1, TaskID: "solo-a", TmuxSession: "legato-solo-a", Status: "running", StartedAt: now},
		},
	})
	if m.selected != 0 || m.agents[m.selected].TaskID != "solo-a" {
		t.Fatalf("setup: selected = %d (%q), want 0 (solo-a)", m.selected, m.agents[m.selected].TaskID)
	}

	// Second refresh: a swarm group is now ahead of solo-a in the sorted
	// order. Index 0 will become the conductor; we want selection to follow
	// solo-a to its new index.
	m, _ = m.Update(AgentsRefreshedMsg{
		Agents: []service.AgentSession{
			{ID: 1, TaskID: "solo-a", TmuxSession: "legato-solo-a", Status: "running", StartedAt: now},
			{ID: 2, TaskID: "swarm-1", TmuxSession: "legato-swarm-1", Status: "running", Role: "conductor", ParentTaskID: "swarm-1", StartedAt: now.Add(-1 * time.Minute)},
		},
	})
	if got := m.agents[m.selected].TaskID; got != "solo-a" {
		t.Errorf("selection drifted: m.agents[%d].TaskID = %q, want solo-a (full order: %v)", m.selected, got, taskIDs(m.agents))
	}
}

func taskIDs(agents []service.AgentSession) []string {
	out := make([]string, len(agents))
	for i, a := range agents {
		out[i] = a.TaskID
	}
	return out
}

// TestAgentsRefreshedMsgGroupsSwarmsAndSolo guards against the regression
// where a newly-spawned solo agent appeared above an existing swarm group
// in the sidebar because AgentsRefreshedMsg bypassed sortAgentsForGrouping.
func TestAgentsRefreshedMsgGroupsSwarmsAndSolo(t *testing.T) {
	m := newTestModel()
	now := time.Now()

	// Order mimics the backend: started_at DESC. Newly-spawned solo first,
	// then swarm members, then older solos.
	m, _ = m.Update(AgentsRefreshedMsg{
		Agents: []service.AgentSession{
			{ID: 10, TaskID: "new-solo", TmuxSession: "legato-new-solo", Status: "running", StartedAt: now},
			{ID: 11, TaskID: "st-worker", TmuxSession: "legato-st-worker", Status: "running", Role: "backend", ParentTaskID: "swarm-1", StartedAt: now.Add(-1 * time.Minute)},
			{ID: 12, TaskID: "swarm-1", TmuxSession: "legato-swarm-1", Status: "running", Role: "conductor", ParentTaskID: "swarm-1", StartedAt: now.Add(-2 * time.Minute)},
			{ID: 13, TaskID: "old-solo", TmuxSession: "legato-old-solo", Status: "running", StartedAt: now.Add(-3 * time.Minute)},
		},
	})

	got := make([]string, len(m.agents))
	for i, a := range m.agents {
		got[i] = a.TaskID
	}
	want := []string{"swarm-1", "st-worker", "new-solo", "old-solo"}
	if len(got) != len(want) {
		t.Fatalf("got %d agents, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("agents[%d] = %q, want %q (full order: %v)", i, got[i], want[i], got)
		}
	}
}

func TestViewContainsElements(t *testing.T) {
	m := newTestModel()
	view := m.View()

	if view == "" {
		t.Fatal("expected non-empty view")
	}

	// Header should contain selected agent's ticket ID
	if !containsStr(view, "REX-1238") {
		t.Error("view should contain selected agent REX-1238")
	}

	// Should contain keybinding hints
	if !containsStr(view, "spawn") {
		t.Error("view should contain keybinding help")
	}

	// Sidebar should list both agents
	if !containsStr(view, "REX-1239") {
		t.Error("view should contain second agent REX-1239 in sidebar")
	}
}

func TestViewEmptyState(t *testing.T) {
	m := New(theme.NewIcons("unicode"))
	m.SetSize(120, 40)
	view := m.View()

	if !containsStr(view, "No agents") {
		t.Error("empty state should show 'No agents' message")
	}
}

func TestSelectedAgentReturnsCorrectAgent(t *testing.T) {
	m := newTestModel()

	a := m.SelectedAgent()
	if a == nil {
		t.Fatal("expected non-nil agent")
	}
	if a.TaskID != "REX-1238" {
		t.Errorf("TaskID = %q, want %q", a.TaskID, "REX-1238")
	}

	m.selected = 1
	a = m.SelectedAgent()
	if a.TaskID != "REX-1239" {
		t.Errorf("TaskID = %q, want %q", a.TaskID, "REX-1239")
	}
}

func TestSelectedAgentNilWhenEmpty(t *testing.T) {
	m := New(theme.NewIcons("unicode"))
	if a := m.SelectedAgent(); a != nil {
		t.Error("expected nil agent when list is empty")
	}
}

func TestSidebarActivityIndicators(t *testing.T) {
	agents := []service.AgentSession{
		{ID: 1, TaskID: "t1", TmuxSession: "legato-t1", Command: "shell", Status: "running", Activity: "working", StartedAt: time.Now()},
		{ID: 2, TaskID: "t2", TmuxSession: "legato-t2", Command: "shell", Status: "running", Activity: "waiting", StartedAt: time.Now()},
		{ID: 3, TaskID: "t3", TmuxSession: "legato-t3", Command: "shell", Status: "running", Activity: "", StartedAt: time.Now()},
		{ID: 4, TaskID: "t4", TmuxSession: "legato-t4", Command: "shell", Status: "dead", Activity: "", StartedAt: time.Now()},
	}

	m := New(theme.NewIcons("unicode"))
	m.SetAgents(agents)
	m.SetSize(120, 40)

	view := m.View()

	// Check activity labels match board card indicators
	if !containsStr(view, "RUNNING") {
		t.Error("view should contain RUNNING indicator")
	}
	if !containsStr(view, "WAITING") {
		t.Error("view should contain WAITING indicator")
	}
	if !containsStr(view, "IDLE") {
		t.Error("view should contain IDLE indicator for idle agent")
	}
	if !containsStr(view, "DEAD") {
		t.Error("view should contain DEAD indicator")
	}
}

func TestSidebarSelectionHighlighting(t *testing.T) {
	m := newTestModel()
	// Selection is on first agent (REX-1238)
	view1 := m.View()

	// Move selection to second agent
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	view2 := m.View()

	// Both views should show both agents but selection should differ
	if !containsStr(view1, "REX-1238") || !containsStr(view1, "REX-1239") {
		t.Error("sidebar should list all agents")
	}
	// Views should differ (different selection highlighting)
	if view1 == view2 {
		t.Error("view should change when selection moves")
	}
}

func TestSidebarWidth(t *testing.T) {
	m := newTestModel()
	// The sidebar is SidebarWidth=30, terminal fills the rest
	tw := m.terminalWidth()
	expected := 120 - SidebarWidth
	if tw != expected {
		t.Errorf("terminalWidth() = %d, want %d", tw, expected)
	}
}

func TestSpawnMsgIsEphemeral(t *testing.T) {
	m := newTestModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd == nil {
		t.Fatal("expected command from 's' key")
	}
	msg := cmd()
	if _, ok := msg.(OpenAgentSpawnMsg); !ok {
		t.Fatalf("expected OpenAgentSpawnMsg, got %T", msg)
	}
}

func TestEmptySidebarShowsSpawnHint(t *testing.T) {
	m := New(theme.NewIcons("unicode"))
	m.SetSize(120, 40)
	view := m.View()

	if !containsStr(view, "s to spawn") {
		t.Error("empty sidebar should show spawn hint")
	}
}

func TestLongTaskIDTruncated(t *testing.T) {
	m := New(theme.NewIcons("unicode"))
	m.SetAgents([]service.AgentSession{
		{ID: 1, TaskID: "VERY-LONG-PROJECT-12345", TmuxSession: "legato-long", Command: "shell", Status: "running", StartedAt: time.Now()},
	})
	m.SetSize(120, 40)
	view := m.View()

	// Should render without panicking; ID should be truncated
	if view == "" {
		t.Error("view should not be empty with long task ID")
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
