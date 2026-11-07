package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentSkillsAreTheSkillFoldersOfTheAgentTools(t *testing.T) {
	home := t.TempDir()
	for _, dir := range []string{".claude/skills/3-tier-review", ".claude/skills/tdd", ".codex/skills/3-tier-review", ".agents/skills/code-review", ".claude/skills/not-a-skill"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(dir, "not-a-skill") {
			if err := os.WriteFile(filepath.Join(home, dir, "SKILL.md"), []byte("---\nname: x\n---\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	if got := strings.Join(AgentSkills(home), ","); got != "3-tier-review,code-review,tdd" {
		t.Fatalf("skills = %s", got)
	}
}
