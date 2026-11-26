package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUsageProfilesLoadWithTiersAndReviewer(t *testing.T) {
	cfg, err := LoadBytes([]byte(`
adapters:
  chimera:
    tiers:
      careful: {launch_args: [--model, example]}
  codex:
    tiers:
      review: {launch_args: [--model, example]}
usage:
  profiles:
    - name: Economy
      agent: chimera
      tier: careful
      reviewer: {agent: codex, tier: review}
    - name: Claude
      agent: claude-code
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Usage.Profiles) != 2 {
		t.Fatalf("profiles = %+v", cfg.Usage.Profiles)
	}
	p := cfg.Usage.Profiles[0]
	if p.Name != "Economy" || p.Agent != "chimera" || p.Tier != "careful" || p.Reviewer == nil || p.Reviewer.Agent != "codex" || p.Reviewer.Tier != "review" {
		t.Fatalf("profile = %+v", p)
	}
	if cfg.Usage.Profiles[1].Reviewer != nil {
		t.Fatal("absent reviewer became an override")
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"name":"Economy","agent":"chimera","tier":"careful","reviewer":{"agent":"codex","tier":"review"}}` {
		t.Fatalf("JSON = %s", data)
	}
}

func TestUsageProfilesRejectInvalidDefaults(t *testing.T) {
	for name, profile := range map[string]string{
		"empty name":             `{name: ' ', agent: codex}`,
		"unknown agent":          `{name: work, agent: other}`,
		"missing agent":          `{name: work}`,
		"unknown tier":           `{name: work, agent: codex, tier: absent}`,
		"missing reviewer agent": `{name: work, agent: codex, reviewer: {tier: careful}}`,
		"unknown reviewer agent": `{name: work, agent: codex, reviewer: {agent: other}}`,
		"unknown reviewer tier":  `{name: work, agent: codex, reviewer: {agent: chimera, tier: absent}}`,
		"duplicate name":         "{name: work, agent: codex}\n    - {name: WORK, agent: chimera}",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadBytes([]byte("usage:\n  profiles:\n    - " + profile + "\n"))
			if err == nil || !strings.Contains(err.Error(), "usage.profiles") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestUsageProfilesRejectRawModels(t *testing.T) {
	for _, profile := range []string{
		`{name: work, agent: codex, model: gpt-example}`,
		`{name: work, agent: codex, reviewer: {agent: chimera, model: null}}`,
	} {
		if _, err := LoadBytes([]byte("usage:\n  profiles: [" + profile + "]\n")); err == nil || !strings.Contains(err.Error(), "model") {
			t.Fatalf("raw model accepted: %v", err)
		}
	}
}

func TestUsageRulesLoadQualifiedMetersAndExplicitOrder(t *testing.T) {
	for _, meter := range []string{"claude.five_hour", "claude.weekly", "claude.weekly_opus", "claude.weekly_sonnet", "codex.five_hour", "codex.weekly"} {
		cfg, err := LoadBytes([]byte(`usage:
  profiles: [{name: Economy, agent: chimera}]
  fallback_profile: economy
  rules:
    - {name: Switch, order: 0, meter: ` + meter + `, at_least: 90.5, profile: ECONOMY}
`))
		if err != nil {
			t.Fatal(err)
		}
		if len(cfg.Usage.Rules) != 1 {
			t.Fatalf("rules = %+v", cfg.Usage.Rules)
		}
		r := cfg.Usage.Rules[0]
		if r.Name != "Switch" || r.Order == nil || *r.Order != 0 || r.Meter != meter || r.AtLeast != 90.5 || !strings.EqualFold(r.Profile, "Economy") {
			t.Fatalf("rule = %+v", r)
		}
		if !strings.EqualFold(cfg.Usage.FallbackProfile, "Economy") {
			t.Fatalf("fallback = %q", cfg.Usage.FallbackProfile)
		}
		data, err := json.Marshal(r)
		if err != nil || !strings.Contains(string(data), `"at_least":90.5`) {
			t.Fatalf("JSON = %s, %v", data, err)
		}
	}
}

func TestUsageRulesRejectInvalidPolicy(t *testing.T) {
	for name, policy := range map[string]string{
		"empty name":        "rules: [{name: ' ', order: 1, meter: claude.weekly, at_least: 90, profile: work}]",
		"duplicate name":    "rules: [{name: Switch, order: 1, meter: claude.weekly, at_least: 90, profile: work}, {name: SWITCH, order: 2, meter: codex.weekly, at_least: 90, profile: work}]",
		"missing order":     "rules: [{name: Switch, meter: claude.weekly, at_least: 90, profile: work}]",
		"unqualified meter": "rules: [{name: Switch, order: 1, meter: weekly, at_least: 90, profile: work}]",
		"unknown meter":     "rules: [{name: Switch, order: 1, meter: codex.weekly_opus, at_least: 90, profile: work}]",
		"below minimum":     "rules: [{name: Switch, order: 1, meter: codex.weekly, at_least: 0.9, profile: work}]",
		"over maximum":      "rules: [{name: Switch, order: 1, meter: codex.weekly, at_least: 100.1, profile: work}]",
		"not a number":      "rules: [{name: Switch, order: 1, meter: codex.weekly, at_least: .nan, profile: work}]",
		"unknown profile":   "rules: [{name: Switch, order: 1, meter: codex.weekly, at_least: 90, profile: absent}]",
		"missing profile":   "rules: [{name: Switch, order: 1, meter: codex.weekly, at_least: 90}]",
		"unknown fallback":  "fallback_profile: absent",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadBytes([]byte("usage:\n  profiles: [{name: work, agent: codex}]\n  " + policy + "\n"))
			if err == nil || !strings.Contains(err.Error(), "usage.") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestUsagePolicyOverlaysProfilesRulesAndFallbackTogether(t *testing.T) {
	cfg, err := LoadBytes([]byte(`usage:
  profiles: [{name: Pinned, agent: claude-code}]
`))
	if err != nil {
		t.Fatal(err)
	}
	err = OverlayAll(cfg, map[string]json.RawMessage{"usage.fallback_profile": json.RawMessage(`"economy"`)}, map[string]map[string]json.RawMessage{
		"adapters": {"chimera": json.RawMessage(`{"tiers":{"careful":{"launch_args":["--model","example"]}}}`)},
		"usage.profiles": {
			"Economy": json.RawMessage(`{"agent":"chimera","tier":"careful","reviewer":{"agent":"codex"}}`),
			"PINNED":  json.RawMessage(`{"agent":"invalid"}`),
		},
		"usage.rules": {"Switch": json.RawMessage(`{"order":1,"meter":"claude.weekly","at_least":90,"profile":"ECONOMY"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Usage.Profiles) != 2 || cfg.Usage.Profiles[0].Agent != "claude-code" || cfg.Usage.Profiles[1].Name != "Economy" || len(cfg.Usage.Rules) != 1 || cfg.Usage.Rules[0].Name != "Switch" || cfg.Usage.FallbackProfile != "economy" {
		t.Fatalf("usage = %+v", cfg.Usage)
	}
	if !cfg.PinnedEntry("usage.profiles", "PINNED") {
		t.Fatal("file profile was not pinned")
	}
	value, ok, err := EntryValue(cfg, "usage.rules", "switch")
	if err != nil || !ok || value.(map[string]any)["at_least"] != 90 {
		t.Fatalf("entry = %+v, %v, %v", value, ok, err)
	}
}

func TestUsagePolicyOverlaysRejectInvalidEntriesAndReferences(t *testing.T) {
	for name, tc := range map[string]struct{ list, value string }{
		"raw model":          {"usage.profiles", `{"agent":"codex","model":"example"}`},
		"raw reviewer model": {"usage.profiles", `{"agent":"codex","reviewer":{"agent":"chimera","model":"example"}}`},
		"unknown agent":      {"usage.profiles", `{"agent":"other"}`},
		"unknown tier":       {"usage.profiles", `{"agent":"codex","tier":"absent"}`},
		"unknown profile":    {"usage.rules", `{"order":1,"meter":"claude.weekly","at_least":90,"profile":"absent"}`},
		"missing order":      {"usage.rules", `{"meter":"claude.weekly","at_least":90,"profile":"work"}`},
		"invalid threshold":  {"usage.rules", `{"order":1,"meter":"claude.weekly","at_least":101,"profile":"work"}`},
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := LoadBytes([]byte("usage:\n  profiles: [{name: work, agent: codex}]\n"))
			if err != nil {
				t.Fatal(err)
			}
			err = OverlayEntries(cfg, map[string]map[string]json.RawMessage{tc.list: {"new": json.RawMessage(tc.value)}})
			if err == nil || !strings.Contains(err.Error(), tc.list) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	cfg, err := LoadBytes(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := Overlay(cfg, map[string]json.RawMessage{"usage.fallback_profile": json.RawMessage(`"absent"`)}); err == nil || !strings.Contains(err.Error(), "usage.fallback_profile") {
		t.Fatalf("err = %v", err)
	}
}

func TestUsageNotificationsAndSettingsAreLive(t *testing.T) {
	cfg, err := LoadBytes(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Notifications.UsageEnabled() {
		t.Fatal("usage notifications default off")
	}
	if v, err := SettingValue(cfg, "notifications.usage"); err != nil || v != true {
		t.Fatalf("notification default = %v, %v", v, err)
	}
	if err := Overlay(cfg, map[string]json.RawMessage{"notifications.usage": json.RawMessage(`false`)}); err != nil {
		t.Fatal(err)
	}
	if cfg.Notifications.UsageEnabled() {
		t.Fatal("overlay did not turn notifications off")
	}
	file, err := LoadBytes([]byte("notifications:\n  usage: false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if file.Notifications.UsageEnabled() {
		t.Fatal("file did not turn notifications off")
	}
	for _, key := range []string{"usage.fallback_profile", "notifications.usage"} {
		s, ok := LookupSetting(key)
		if !ok || s.Mode != ModeLive {
			t.Fatalf("%s = %+v, %v", key, s, ok)
		}
	}
	for _, key := range []string{"usage.claude.enabled", "usage.codex.enabled", "usage.poll_interval", "usage.fallback_profile"} {
		s, ok := LookupSetting(key)
		if !ok || s.Section != "usage" {
			t.Fatalf("%s section = %q", key, s.Section)
		}
	}
	for _, key := range []string{"usage.profiles", "usage.rules"} {
		l, ok := LookupEntryList(key)
		if !ok || l.Mode != ModeLive || l.NameField != "name" || l.Ordered {
			t.Fatalf("%s = %+v", key, l)
		}
	}
}
