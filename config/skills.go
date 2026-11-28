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

// normalizeFolders checks the folders Legato reads what it gives agents
// from: skills.dir and managed_hooks.dir.
func normalizeFolders(cfg *Config) error {
	for _, folder := range []struct {
		key string
		dir *string
	}{{"skills.dir", &cfg.Skills.Dir}, {"managed_hooks.dir", &cfg.ManagedHooks.Dir}} {
		dir, err := normalizeFolder(folder.key, *folder.dir)
		if err != nil {
			return err
		}
		*folder.dir = dir
	}
	return nil
}

// normalizeFolder checks a folder setting: empty, or a path that is
// absolute or starts with ~/, which it expands and cleans.
func normalizeFolder(key, value string) (string, error) {
	dir := strings.TrimSpace(value)
	if dir == "" {
		return "", nil
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return value, fmt.Errorf("%s: %w", key, err)
		}
		dir = filepath.Join(home, strings.TrimPrefix(dir[1:], "/"))
	}
	if !filepath.IsAbs(dir) {
		return value, fmt.Errorf("%s: %q must be absolute or start with ~/", key, value)
	}
	return filepath.Clean(dir), nil
}
