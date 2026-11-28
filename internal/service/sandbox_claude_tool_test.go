package service

import (
	"testing"

	"github.com/cpave3/legato/internal/engine/sandbox"
)

// Legato's own Claude Code lives in its data folder, which a sandbox never
// sees; a claude-code agent sees that one folder, read-only.
func TestAClaudeCodeAgentSeesLegatosOwnClaudeReadOnly(t *testing.T) {
	const tool = "/var/home/me/.local/share/legato/tools/claude"
	in := baseInput()
	fs := hostFS()
	fs[tool] = "dir"
	in.FS = fs
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := mounted(plan)[tool]; !ok || got.Kind != sandbox.ReadOnly {
		t.Fatalf("%s: %+v, want read-only", tool, got)
	}
	in.AgentKind = "codex"
	plan, err = BuildSandboxPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := mounted(plan)[tool]; ok {
		t.Fatalf("a codex agent sees %s: %+v", tool, got)
	}
}
