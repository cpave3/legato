package hooks_test

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
)

// On macOS Legato's sandbox is Seatbelt, and a process inside a Seatbelt
// sandbox cannot apply one of its own: Claude Code and Codex start with
// their own sandbox off there, attended or not. Inside bwrap they keep it.
func TestInsideSeatbeltTheToolsOwnSandboxIsOff(t *testing.T) {
	seatbelt := map[string]string{"LEGATO_SANDBOXED": "1", "LEGATO_SANDBOX_BACKEND": "seatbelt"}
	bwrap := map[string]string{"LEGATO_SANDBOXED": "1", "LEGATO_SANDBOX_BACKEND": "bwrap"}
	for _, tc := range []struct {
		name   string
		launch func(map[string]string) string
		off    string
	}{
		{"claude-code", func(env map[string]string) string {
			return hooks.NewClaudeCodeAdapter("legato").LaunchCommand(env, "", "", "")
		}, `--settings '{"sandbox":{"enabled":false}}'`},
		{"codex", func(env map[string]string) string {
			return hooks.NewCodexAdapter("legato").LaunchCommand(env, "", "", "")
		}, `-c 'sandbox_mode="danger-full-access"'`},
	} {
		if got := tc.launch(seatbelt); !strings.Contains(got, tc.off) {
			t.Errorf("%s in Seatbelt: %s", tc.name, got)
		}
		if got := tc.launch(bwrap); strings.Contains(got, tc.off) {
			t.Errorf("%s in bwrap turns its sandbox off: %s", tc.name, got)
		}
	}
}
