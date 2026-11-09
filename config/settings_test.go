package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEverySettingIsDeclaredOnceWithAClassAndAMode(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range Settings() {
		if seen[s.Key] {
			t.Errorf("%s declared twice", s.Key)
		}
		seen[s.Key] = true
		if s.Class != ClassRead && s.Class != ClassChange && s.Class != ClassRisky {
			t.Errorf("%s: class %q", s.Key, s.Class)
		}
		if s.Mode != ModeLive && s.Mode != ModeNewAgents && s.Mode != ModeRestart {
			t.Errorf("%s: mode %q", s.Key, s.Mode)
		}
		if _, err := SettingValue(Defaults(), s.Key); err != nil {
			t.Errorf("%s does not name a field of the configuration: %v", s.Key, err)
		}
	}
	for _, key := range []string{"agents.reviews", "theme", "icons", "editor", "updates.mode", "web.tint", "burnlite.projects", "notifications.ntfy.topic"} {
		if !seen[key] {
			t.Errorf("%s is not registered", key)
		}
	}
}

func TestADatabaseValueApplies(t *testing.T) {
	writeTestConfig(t, "theme: default\n")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := Overlay(cfg, map[string]json.RawMessage{"agents.reviews": json.RawMessage(`false`), "updates.mode": json.RawMessage(`"off"`)}); err != nil {
		t.Fatal(err)
	}
	if cfg.Agents.Reviews || cfg.Updates.Mode != "off" {
		t.Fatalf("reviews = %v, updates.mode = %q", cfg.Agents.Reviews, cfg.Updates.Mode)
	}
}

func TestAFileValuePinsItsSetting(t *testing.T) {
	writeTestConfig(t, "agents:\n  reviews: true\nicons: nerdfonts\n")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Pinned("agents.reviews") || !cfg.Pinned("icons") || cfg.Pinned("agents.peer_awareness") || cfg.Pinned("theme") {
		t.Fatalf("pins = %v", cfg.pins)
	}
	if err := Overlay(cfg, map[string]json.RawMessage{"agents.reviews": json.RawMessage(`false`), "icons": json.RawMessage(`"unicode"`)}); err != nil {
		t.Fatal(err)
	}
	if !cfg.Agents.Reviews || cfg.Icons != "nerdfonts" {
		t.Fatalf("a pinned setting took the database's value: reviews = %v, icons = %q", cfg.Agents.Reviews, cfg.Icons)
	}
}

func TestAValueIsCheckedByItsOwnRule(t *testing.T) {
	for key, raw := range map[string]string{
		"agents.reviews":          `"yes"`,
		"icons":                   `"emoji"`,
		"updates.mode":            `"sometimes"`,
		"agents.sparkline_window": `"ten minutes"`,
		"burnlite.projects":       `"EST"`,
	} {
		s, _ := LookupSetting(key)
		if _, err := DecodeSetting(s, json.RawMessage(raw)); err == nil {
			t.Errorf("%s = %s was taken", key, raw)
		}
	}
	s, _ := LookupSetting("burnlite.projects")
	if v, err := DecodeSetting(s, json.RawMessage(`["EST","LEG"]`)); err != nil || len(v.([]string)) != 2 {
		t.Fatalf("burnlite.projects: %v, %v", v, err)
	}
}

// A change that leaves the whole configuration invalid is refused with the
// error config.Load gives for the same file.
func TestAnInvalidChangeIsRefusedWithLoadsError(t *testing.T) {
	writeTestConfig(t, "agents:\n  sandbox:\n    enabled: true\n    backend: nspawn\n")
	_, loadErr := Load()
	if loadErr == nil {
		t.Fatal("the file loaded")
	}
	writeTestConfig(t, "agents:\n  sandbox:\n    enabled: true\n")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	err = Overlay(cfg, map[string]json.RawMessage{"agents.sandbox.backend": json.RawMessage(`"nspawn"`)})
	if err == nil || err.Error() != loadErr.Error() {
		t.Fatalf("overlay error = %v, want %v", err, loadErr)
	}
	if !strings.Contains(err.Error(), "nspawn") {
		t.Fatalf("error = %v", err)
	}
}

func TestFileOnlySettingsAreNotRegistered(t *testing.T) {
	for _, key := range FileOnlySettings() {
		if _, ok := LookupSetting(key); ok {
			t.Errorf("%s is file-only but registered", key)
		}
	}
}
