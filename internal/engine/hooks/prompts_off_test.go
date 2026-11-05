package hooks_test

import (
	"slices"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
)

// Each agent tool a worker can start a handed-off agent with turns its own
// permission prompts off with these arguments.
func TestEachAgentToolTurnsItsPermissionPromptsOff(t *testing.T) {
	for name, tc := range map[string]struct {
		tool interface{ PromptsOffArgs() []string }
		want []string
	}{
		"claude-code": {hooks.NewClaudeCodeAdapter("legato"), []string{"--dangerously-skip-permissions"}},
		"codex":       {hooks.NewCodexAdapter("legato"), []string{"-c", `approval_policy="never"`, "-c", `sandbox_mode="danger-full-access"`}},
		"chimera":     {hooks.NewChimeraAdapter("legato"), []string{"--auto-approve", "all"}},
	} {
		if got := tc.tool.PromptsOffArgs(); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
}
