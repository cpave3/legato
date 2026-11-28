package hooks_test

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
)

func TestClaudeCodeLaunchesTheCommandItIsGiven(t *testing.T) {
	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	command, managed := "/data/legato/tools/claude/claude", true
	adapter.SetCommand(func() (string, bool) { return command, managed })

	launch := adapter.LaunchCommand(map[string]string{}, "", "", "")
	if !strings.HasPrefix(launch, "/data/legato/tools/claude/claude") {
		t.Fatalf("launch = %q", launch)
	}
	resume := adapter.LaunchCommand(map[string]string{"LEGATO_SESSION_POLICY": "resume", "LEGATO_RESUME_SESSION_ID": "abc"}, "", "", "")
	if !strings.HasPrefix(resume, "/data/legato/tools/claude/claude --resume abc") {
		t.Fatalf("resume = %q", resume)
	}
	if vars := adapter.EnvVars("t1", ""); vars["DISABLE_AUTOUPDATER"] != "1" {
		t.Fatalf("a managed claude may update itself: %v", vars)
	}

	// Each launch asks again, so a rollback reaches the next agent.
	command, managed = "/home/me/my claude", false
	if launch := adapter.LaunchCommand(map[string]string{}, "", "", ""); !strings.HasPrefix(launch, "'/home/me/my claude'") {
		t.Fatalf("launch after the change = %q", launch)
	}
	if vars := adapter.EnvVars("t1", ""); vars["DISABLE_AUTOUPDATER"] != "" {
		t.Fatalf("the operator's own claude was told not to update: %v", vars)
	}
}

func TestClaudeCodeWithNoCommandLaunchesClaudeFromPath(t *testing.T) {
	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	if launch := adapter.LaunchCommand(map[string]string{}, "", "", ""); !strings.HasPrefix(launch, "claude") {
		t.Fatalf("launch = %q", launch)
	}
	adapter.SetCommand(func() (string, bool) { return "", false })
	if launch := adapter.LaunchCommand(map[string]string{}, "", "", ""); !strings.HasPrefix(launch, "claude") {
		t.Fatalf("launch = %q", launch)
	}
}
