package hooks

import (
	"strings"
	"testing"
)

func TestChimeraLaunchCommandWithModes(t *testing.T) {
	a := NewChimeraAdapter("/usr/bin/legato")
	a.SetModes(map[string]string{
		"conductor": "legato-orchestrator",
		"worker":    "legato-worker",
	})

	// Conductor case
	cmd := a.LaunchCommand(map[string]string{
		"LEGATO_ROLE_PROMPT_FILE": "/tmp/role.md",
		"LEGATO_AGENT_ROLE":       "conductor",
	}, "")
	if !strings.Contains(cmd, "--mode legato-orchestrator") {
		t.Errorf("conductor cmd missing --mode legato-orchestrator: %q", cmd)
	}

	// Worker case (specific role label)
	cmd = a.LaunchCommand(map[string]string{
		"LEGATO_ROLE_PROMPT_FILE": "/tmp/role.md",
		"LEGATO_AGENT_ROLE":       "backend",
	}, "")
	if !strings.Contains(cmd, "--mode legato-worker") {
		t.Errorf("worker cmd missing --mode legato-worker: %q", cmd)
	}

	// No modes set → no --mode flag
	a2 := NewChimeraAdapter("/usr/bin/legato")
	cmd = a2.LaunchCommand(map[string]string{
		"LEGATO_ROLE_PROMPT_FILE": "/tmp/role.md",
		"LEGATO_AGENT_ROLE":       "conductor",
	}, "")
	if strings.Contains(cmd, "--mode") {
		t.Errorf("expected no --mode flag with empty modes, got: %q", cmd)
	}
}
