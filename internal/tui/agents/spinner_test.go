package agents

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

func TestARunningAgentsMarkTurns(t *testing.T) {
	m := New(theme.NewIcons("unicode"))
	m.SetAgents(mixedAgents())
	m.SetSize(140, 40)

	for _, frame := range []int{0, 1, 7} {
		m.SetSpinner(frame)
		if view := m.View(); !strings.Contains(view, theme.Spinner(frame)+" busy-1") {
			t.Errorf("frame %d: the running agent should be marked %q: %q", frame, theme.Spinner(frame), view)
		}
	}
}

func TestTheEnsembleTurnsTheMarkOfARunningAgentToo(t *testing.T) {
	m := ensemble(t)
	m.SetSpinner(4)

	view := m.View()
	if !strings.Contains(view, theme.Spinner(4)+" busy-1") || !strings.Contains(view, theme.Spinner(4)+" 1 running") {
		t.Errorf("the tile and the count should both turn: %q", view)
	}
}

func TestAnAgentThatIsNotRunningIsDrawnTheSameWhateverTheFrame(t *testing.T) {
	m := New(theme.NewIcons("unicode"))
	m.SetAgents([]service.AgentSession{{TaskID: "idle-1", Status: "running"}, {TaskID: "ask-1", Status: "running", Activity: "waiting"}})
	m.SetDurations(map[string]DurationData{"idle-1": {Working: 90_000_000_000}})
	m.SetSize(140, 40)
	still := m.View()

	m.SetSpinner(5)

	if m.View() != still {
		t.Error("nothing runs, so nothing should move")
	}
	if m.HasWorkingAgent() {
		t.Error("no agent is running")
	}
}

func TestAViewWithARunningAgentHasSomethingToAnimate(t *testing.T) {
	m := New(theme.NewIcons("unicode"))
	m.SetAgents(mixedAgents())

	if !m.HasWorkingAgent() {
		t.Error("busy-1 is running")
	}
}
