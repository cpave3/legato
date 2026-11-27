package config

import (
	"encoding/json"
	"strings"
	"testing"
)

const usageRepositoryProfiles = `usage:
  profiles: [{name: Claude Work, agent: claude-code}, {name: Default, agent: claude-code}, {name: Codex, agent: codex}]
  fallback_profile: Default
`

func TestUsageRepositoryDefaultsMatchExactBeforeOwner(t *testing.T) {
	cfg, err := LoadBytes([]byte(usageRepositoryProfiles + `  repositories:
    - {repository: cpave3/*, profile: default}
    - {repository: github.com/Autopilot-Dev/Autopilot, profile: claude work}
    - {repository: cpave3/legato, profile: Codex}
`))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ repo, profile, match, kind string }{
		{"autopilot-dev/autopilot", "Claude Work", "autopilot-dev/autopilot", "exact"},
		{"https://github.com/AUTOPILOT-DEV/autopilot.git", "Claude Work", "autopilot-dev/autopilot", "exact"},
		{"cpave3/legato", "Codex", "cpave3/legato", "exact"},
		{"git@github.com:cpave3/other.git", "Default", "cpave3/*", "owner"},
	} {
		d, kind, ok := cfg.Usage.RepositoryDefault(tc.repo)
		if !ok || d.Profile != tc.profile || d.Repository != tc.match || kind != tc.kind {
			t.Fatalf("%s = %+v %q %v, want %s via %s (%s)", tc.repo, d, kind, ok, tc.profile, tc.match, tc.kind)
		}
	}
	for _, repo := range []string{"", "other/legato", "gitlab.com/cpave3/legato", "not a repo"} {
		if d, _, ok := cfg.Usage.RepositoryDefault(repo); ok {
			t.Fatalf("%q matched %+v", repo, d)
		}
	}
}

func TestUsageRepositoryDefaultsRejectInvalidEntries(t *testing.T) {
	for name, list := range map[string]string{
		"star":            "[{repository: '*', profile: Default}]",
		"star owner":      "[{repository: '*/legato', profile: Default}]",
		"partial pattern": "[{repository: 'cp*/legato', profile: Default}]",
		"no owner":        "[{repository: legato, profile: Default}]",
		"other host":      "[{repository: gitlab.com/cpave3/legato, profile: Default}]",
		"empty":           "[{repository: ' ', profile: Default}]",
		"duplicate":       "[{repository: cpave3/*, profile: Default}, {repository: CPAVE3/*, profile: Codex}]",
		"unknown profile": "[{repository: cpave3/*, profile: absent}]",
		"no profile":      "[{repository: cpave3/*}]",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadBytes([]byte(usageRepositoryProfiles + "  repositories: " + list + "\n"))
			if err == nil || !strings.Contains(err.Error(), "usage.repositories") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestUsageRepositoryDefaultsAreALiveEntryList(t *testing.T) {
	l, ok := LookupEntryList("usage.repositories")
	if !ok || l.Mode != ModeLive || l.NameField != "repository" {
		t.Fatalf("list = %+v, %v", l, ok)
	}
	cfg, err := LoadBytes([]byte(usageRepositoryProfiles))
	if err != nil {
		t.Fatal(err)
	}
	if err := OverlayEntries(cfg, map[string]map[string]json.RawMessage{"usage.repositories": {"autopilot-dev/autopilot": json.RawMessage(`{"profile":"Claude Work"}`)}}); err != nil {
		t.Fatal(err)
	}
	if d, kind, ok := cfg.Usage.RepositoryDefault("autopilot-dev/autopilot"); !ok || d.Profile != "Claude Work" || kind != "exact" {
		t.Fatalf("overlay default = %+v %q %v", d, kind, ok)
	}
	if err := OverlayEntries(cfg, map[string]map[string]json.RawMessage{"usage.repositories": {"cpave3/*": json.RawMessage(`{"profile":"absent"}`)}}); err == nil || !strings.Contains(err.Error(), "usage.repositories") {
		t.Fatalf("err = %v", err)
	}
}

func TestUsageRulesWhenDefaultNamesProfiles(t *testing.T) {
	cfg, err := LoadBytes([]byte(usageRepositoryProfiles + `  rules:
    - {name: work-weekly, order: 1, meter: claude.weekly, at_least: 90, profile: Codex, when_default: [claude WORK]}
    - {name: global, order: 2, meter: codex.weekly, at_least: 90, profile: Default}
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Usage.Rules[0].WhenDefault; len(got) != 1 || got[0] != "Claude Work" {
		t.Fatalf("when_default = %q", got)
	}
	if got := cfg.Usage.Rules[1].WhenDefault; len(got) != 0 {
		t.Fatalf("unscoped when_default = %q", got)
	}
	if _, err := LoadBytes([]byte(usageRepositoryProfiles + "  rules: [{name: r, order: 1, meter: claude.weekly, at_least: 90, profile: Codex, when_default: [absent]}]\n")); err == nil || !strings.Contains(err.Error(), "when_default") {
		t.Fatalf("err = %v", err)
	}
}
