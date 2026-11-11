package agents

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

func interruptedModel() Model {
	m := New(theme.NewIcons("unicode"))
	m.SetAgents(testAgents())
	m.SetSize(120, 40)
	m, _ = m.Update(InterruptedRefreshedMsg{Agents: []service.InterruptedAgent{
		{TaskID: "T-1", Title: "Fix the sync", AgentKind: "claude-code", Resumable: true},
		{TaskID: "T-2", Title: "Write the docs", AgentKind: "codex", CanStartNew: true, Reason: "it never reported its conversation"},
	}})
	return m
}

func press(m Model, key rune) tea.Msg {
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
	if cmd == nil {
		return nil
	}
	return cmd()
}

func TestInterruptedAgentsAreListedInTheirOwnGroup(t *testing.T) {
	m := interruptedModel()

	out := m.View()
	if !strings.Contains(out, "INTERRUPTED · 2") {
		t.Fatalf("no interrupted group: %q", out)
	}
	// Running agents are still listed, and a refresh of them keeps the
	// interrupted ones.
	m, _ = m.Update(AgentsRefreshedMsg{Agents: testAgents()})
	if len(m.agents) != 4 {
		t.Fatalf("agents = %d, want 2 running and 2 interrupted", len(m.agents))
	}
}

func TestReloadResumesAnInterruptedAgentOrStartsANewOne(t *testing.T) {
	m := interruptedModel()
	m.SelectByTaskID("T-1")
	if got, ok := press(m, 'R').(ResumeInterruptedMsg); !ok || got.TaskID != "T-1" || got.Fresh {
		t.Fatalf("R on a resumable agent = %#v, want ResumeInterruptedMsg", got)
	}
	m.SelectByTaskID("T-2")
	if got, ok := press(m, 'R').(ResumeInterruptedMsg); !ok || got.TaskID != "T-2" || !got.Fresh {
		t.Fatalf("R on one with no conversation = %#v, want a fresh start", got)
	}
}

func TestKillDismissesAnInterruptedAgent(t *testing.T) {
	m := interruptedModel()
	m.SelectByTaskID("T-1")
	if got, ok := press(m, 'X').(DismissInterruptedMsg); !ok || got.TaskID != "T-1" {
		t.Fatalf("X = %#v, want DismissInterruptedMsg", press(m, 'X'))
	}
}

func TestResumeAllAsksForEveryInterruptedAgent(t *testing.T) {
	m := interruptedModel()
	if _, ok := press(m, 'A').(ResumeAllInterruptedMsg); !ok {
		t.Fatalf("A = %#v, want ResumeAllInterruptedMsg", press(m, 'A'))
	}
}

func TestTheSelectedInterruptedAgentSaysWhyItCannotBeResumed(t *testing.T) {
	m := interruptedModel()
	m.SelectByTaskID("T-2")
	if out := m.View(); !strings.Contains(out, "never reported its conversation") {
		t.Fatalf("no reason shown: %q", out)
	}
}
