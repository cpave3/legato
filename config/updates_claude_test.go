package config

import (
	"encoding/json"
	"testing"
)

func TestClaudeCodesUpdateSettingsAreRead(t *testing.T) {
	writeTestConfig(t, "updates:\n  claude:\n    command: /opt/claude/bin/claude\n")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Updates.Claude.Command != "/opt/claude/bin/claude" || !cfg.Pinned("updates.claude.command") {
		t.Fatalf("updates.claude = %+v", cfg.Updates.Claude)
	}
	if err := Overlay(cfg, map[string]json.RawMessage{"updates.claude.channel": json.RawMessage(`"latest"`)}); err != nil {
		t.Fatal(err)
	}
	if cfg.Updates.Claude.Channel != "latest" {
		t.Fatalf("channel = %q", cfg.Updates.Claude.Channel)
	}
	s, ok := LookupSetting("updates.claude.channel")
	if !ok || s.DefaultLabel != "stable" {
		t.Fatalf("updates.claude.channel = %+v", s)
	}
	if _, err := DecodeSetting(s, json.RawMessage(`"beta"`)); err == nil {
		t.Fatal("a channel other than stable or latest was taken")
	}
}

func TestClaudeCodesCommandMustBeAPathOrACommandName(t *testing.T) {
	s, _ := LookupSetting("updates.claude.command")
	for _, ok := range []string{`""`, `"claude"`, `"/usr/local/bin/claude"`} {
		if _, err := DecodeSetting(s, json.RawMessage(ok)); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{`"bin/claude"`, `"claude --resume"`, `"~/bin/claude"`} {
		if _, err := DecodeSetting(s, json.RawMessage(bad)); err == nil {
			t.Errorf("%s taken", bad)
		}
	}
}
