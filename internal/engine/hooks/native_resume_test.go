package hooks_test

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
)

func TestNativeAdaptersResumeExactConversation(t *testing.T) {
	env := map[string]string{"LEGATO_SESSION_POLICY": "resume", "LEGATO_RESUME_SESSION_ID": "conversation with spaces", "LEGATO_ROLE_PROMPT_FILE": "/fresh/prompt"}
	claude := hooks.NewClaudeCodeAdapter("legato")
	claude.SetLaunchArgs([]string{"--permission-mode", "acceptEdits"})
	cmd := claude.LaunchCommand(env, "", "selected-model", "")
	if !strings.HasPrefix(cmd, "claude --resume 'conversation with spaces'") || !strings.Contains(cmd, "--permission-mode acceptEdits") || !strings.Contains(cmd, "--model selected-model") || strings.Contains(cmd, "append-system-prompt") {
		t.Fatalf("Claude resume command = %s", cmd)
	}
	for _, native := range []bool{false, true} {
		codex := hooks.NewCodexAdapter("legato")
		codex.SetNativeBridge(native)
		codex.SetLaunchArgs([]string{"-c", "sandbox_mode=workspace-write"})
		cmd = codex.LaunchCommand(env, "", "selected-model", "")
		prefix := "codex resume 'conversation with spaces'"
		if native {
			prefix = "legato agent codex -- resume 'conversation with spaces'"
		}
		if !strings.HasPrefix(cmd, prefix) || !strings.Contains(cmd, "sandbox_mode=workspace-write") || !strings.Contains(cmd, "--model selected-model") || strings.Contains(cmd, "developer_instructions") {
			t.Fatalf("Codex resume command = %s", cmd)
		}
	}
	env["LEGATO_SESSION_POLICY"] = "new"
	if cmd := claude.LaunchCommand(env, "", "", ""); strings.Contains(cmd, "--resume") || !strings.Contains(cmd, "append-system-prompt") {
		t.Fatalf("Claude fresh command = %s", cmd)
	}
	if cmd := hooks.NewCodexAdapter("legato").LaunchCommand(env, "", "", ""); strings.Contains(cmd, " resume ") || !strings.Contains(cmd, "developer_instructions") {
		t.Fatalf("Codex fresh command = %s", cmd)
	}
}
