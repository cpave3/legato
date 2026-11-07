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
func TestAReviewOverrideIsCheckedAgainstWhatTheWorkerHas(t *testing.T) {
	advert := ReviewPassesAdvert{
		Defaults: ReviewPassSettings{Enabled: true, Agent: "codex", Model: "gpt-5.5", Skill: "3-tier-review", MaxPasses: 3},
		Adapters: []ReviewAdapter{{Name: "codex", Tiers: []string{"careful"}}, {Name: "claude-code"}},
		Skills:   []string{"3-tier-review"},
	}
	for _, tc := range []struct {
		name     string
		override ReviewPassOverride
		want     string
	}{
		{"unknown tool", ReviewPassOverride{Agent: "gemini"}, `reviewer tool "gemini"`},
		{"unknown tier", ReviewPassOverride{Agent: "codex", Tier: "fast"}, `tier "fast"`},
		{"unknown skill", ReviewPassOverride{Skill: "nitpick"}, `skill "nitpick"`},
		{"no passes", ReviewPassOverride{MaxPasses: -1}, "passes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := advert.Apply(tc.override); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
	off := false
	settings, err := advert.Apply(ReviewPassOverride{Enabled: &off})
	if err != nil || settings.Enabled {
		t.Fatalf("turned off: %+v, %v", settings, err)
	}
	settings, err = advert.Apply(ReviewPassOverride{Agent: "claude-code", Model: "opus", MaxPasses: 5})
	if err != nil || !settings.Enabled || settings.Agent != "claude-code" || settings.Model != "opus" || settings.Skill != "3-tier-review" || settings.MaxPasses != 5 {
		t.Fatalf("override = %+v, %v", settings, err)
	}
}
