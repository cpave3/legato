package service

import (
	"os"
	"path/filepath"
	"sort"
)

// ReviewPassSettings are the review settings of one handoff: the worker's
// defaults, or what a main's override made of them.
type ReviewPassSettings struct {
	Enabled   bool   `json:"enabled"`
	Agent     string `json:"agent"`
	Model     string `json:"model,omitempty"`
	Tier      string `json:"tier,omitempty"`
	Skill     string `json:"skill,omitempty"`
	MaxPasses int    `json:"max_passes"`
}

// ReviewAdapter is an agent tool a worker can start a reviewer with, and
// its tiers.
type ReviewAdapter struct {
	Name  string   `json:"name"`
	Tiers []string `json:"tiers,omitempty"`
}

// ReviewPassesAdvert is what a worker with review passes on tells main
// instances in /api/meta: its defaults, and the tools, tiers and skills a
// main's override may choose from.
type ReviewPassesAdvert struct {
	Defaults ReviewPassSettings `json:"defaults"`
	Adapters []ReviewAdapter    `json:"adapters"`
	Skills   []string           `json:"skills"`
}

// agentSkillDirs are where the agent tools keep the skills they can be told
// to use, under the home directory.
var agentSkillDirs = []string{".claude/skills", ".codex/skills", ".agents/skills"}

// AgentSkills lists the skills the agent tools of home have: each folder of
// a skills directory with a SKILL.md, by name, once.
func AgentSkills(home string) []string {
	seen := map[string]bool{}
	for _, dir := range agentSkillDirs {
		entries, err := os.ReadDir(filepath.Join(home, dir))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
				continue
			}
			if _, err := os.Stat(filepath.Join(home, dir, entry.Name(), "SKILL.md")); err == nil {
				seen[entry.Name()] = true
			}
		}
	}
	skills := make([]string, 0, len(seen))
	for name := range seen {
		skills = append(skills, name)
	}
	sort.Strings(skills)
	return skills
}
