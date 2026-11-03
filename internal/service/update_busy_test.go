package service

import "testing"

func TestAgentsMidTurn(t *testing.T) {
	cases := []struct {
		name     string
		sessions []AgentSession
		want     bool
	}{
		{"none", nil, false},
		{"working", []AgentSession{{Status: "running", Activity: "working"}}, true},
		{"a turn that will wake again", []AgentSession{{Status: "running", Activity: "background"}}, true},
		{"waiting for the person", []AgentSession{{Status: "running", Activity: "waiting"}}, false},
		{"idle", []AgentSession{{Status: "running", Activity: ""}}, false},
		{"ended while working", []AgentSession{{Status: "dead", Activity: "working"}}, false},
	}
	for _, c := range cases {
		if got := AgentsMidTurn(c.sessions); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
