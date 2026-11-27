package config

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestNamedClaudeAccountProfileAndMeterLoad(t *testing.T) {
	root := filepath.Join(t.TempDir(), "work")
	cfg, err := LoadBytes([]byte(fmt.Sprintf(`claude_accounts:
  - {name: work, label: Work subscription, config_dir: %q}
usage:
  profiles:
    - {name: Work, agent: claude-code, account: work, reviewer: {agent: claude-code, account: work}}
  rules:
    - {name: Full, order: 1, meter: claude.work.weekly, at_least: 90, profile: Work}
`, root)))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ClaudeAccounts) != 1 || cfg.ClaudeAccounts[0].ConfigDir != root || cfg.Usage.Profiles[0].Account != "work" || cfg.Usage.Profiles[0].Reviewer.Account != "work" {
		t.Fatalf("account selection lost: %+v", cfg)
	}
}

func TestClaudeAccountConfigurationRejectsAmbiguousSelections(t *testing.T) {
	root := filepath.Join(t.TempDir(), "work")
	for name, body := range map[string]string{
		"reserved":             fmt.Sprintf("claude_accounts: [{name: default, config_dir: %q}]", root),
		"invalid slug":         fmt.Sprintf("claude_accounts: [{name: work.extra, config_dir: %q}]", root),
		"relative root":        "claude_accounts: [{name: work, config_dir: relative}]",
		"root":                 "claude_accounts: [{name: work, config_dir: /}]",
		"overlap":              fmt.Sprintf("claude_accounts: [{name: work, config_dir: %q}, {name: second, config_dir: %q}]", root, filepath.Join(root, "nested")),
		"duplicate":            fmt.Sprintf("claude_accounts: [{name: work, config_dir: %q}, {name: work, config_dir: %q}]", root, filepath.Join(t.TempDir(), "second")),
		"unknown selection":    "usage: {profiles: [{name: Work, agent: claude-code, account: missing}]}",
		"unknown reeve":        "reeve: {agent: claude-code, account: missing}",
		"unknown conductor":    "swarm: {conductor: {agent: claude-code, account: missing}}",
		"worker tool conflict": fmt.Sprintf("claude_accounts: [{name: work, config_dir: %q}]\nswarm: {worker: {agent: codex, account: work}}", root),
		"incompatible":         fmt.Sprintf("claude_accounts: [{name: work, config_dir: %q}]\nusage: {profiles: [{name: Work, agent: codex, account: work}]}", root),
		"unknown rule":         "usage: {profiles: [{name: Work, agent: claude-code}], rules: [{name: full, order: 1, meter: claude.missing.weekly, at_least: 90, profile: Work}]}",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadBytes([]byte(body)); err == nil {
				t.Fatal("invalid account configuration accepted")
			}
		})
	}
}
