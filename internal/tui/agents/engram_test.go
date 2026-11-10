package agents

import (
	"testing"

	"github.com/cpave3/legato/internal/service"
)

func TestAnAgentsEngramIsShownBesideItsTool(t *testing.T) {
	cases := map[string]service.AgentSession{
		"claude · hestia": {AgentKind: "claude-code", Engram: "hestia"},
		"codex":           {AgentKind: "codex"},
		"shell":           {},
	}
	for want, agent := range cases {
		if got := kindLabel(agent); got != want {
			t.Errorf("kindLabel(%+v) = %q, want %q", agent, got, want)
		}
	}
}
