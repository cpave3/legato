package main

import (
	"strings"
	"testing"
)

func TestHelpRequestAtEveryLevel(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"--help"}, ""},
		{[]string{"task", "--help"}, "task"},
		{[]string{"task", "update", "--help"}, "task update"},
		{[]string{"task", "update", "-h"}, "task update"},
		{[]string{"swarm", "propose-plan", "help"}, "swarm propose-plan"},
	}
	for _, tt := range tests {
		path, ok := helpRequest(tt.args)
		if !ok {
			t.Fatalf("helpRequest(%v) did not detect help", tt.args)
		}
		if got := strings.Join(path, " "); got != tt.want {
			t.Errorf("helpRequest(%v) path = %q, want %q", tt.args, got, tt.want)
		}
	}
}

func TestTaskUpdateHelpExplainsProviderRestrictions(t *testing.T) {
	out := helpText([]string{"task", "update"})
	for _, want := range []string{"--status", "--title", "--description", "Jira-backed"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q:\n%s", want, out)
		}
	}
}

func TestAgentPrimerCoversDiscoveryAndCoreWorkflow(t *testing.T) {
	for _, want := range []string{
		"# Legato CLI primer",
		"legato task create",
		"legato task update",
		"LEGATO_TASK_ID",
		"legato <path> --help",
	} {
		if !strings.Contains(agentPrimer, want) {
			t.Errorf("primer missing %q", want)
		}
	}
}
