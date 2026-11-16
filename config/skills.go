package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SkillsConfig is where Legato finds the skills it gives agents. See
// docs/ai/skills.md.
type SkillsConfig struct {
	// Dir is the skills folder: one folder per skill, each with a SKILL.md.
	// <legato home>/skills (~/.legato/skills) when empty.
	Dir string `yaml:"dir"`
}

// ResolveSkillsDir is the skills folder: skills.dir, else skills in
// Legato's home folder (~/.legato, or $LEGATO_HOME), which the caller
// passes since config reads no other package.
func ResolveSkillsDir(cfg *Config, legatoHome string) string {
	if cfg.Skills.Dir != "" {
		return cfg.Skills.Dir
	}
	return filepath.Join(legatoHome, "skills")
}

func normalizeSkills(cfg *Config) error {
	dir := strings.TrimSpace(cfg.Skills.Dir)
	if dir == "" {
		cfg.Skills.Dir = ""
		return nil
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("skills.dir: %w", err)
		}
		dir = filepath.Join(home, strings.TrimPrefix(dir[1:], "/"))
	}
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("skills.dir: %q must be absolute or start with ~/", cfg.Skills.Dir)
	}
	cfg.Skills.Dir = filepath.Clean(dir)
	return nil
}
