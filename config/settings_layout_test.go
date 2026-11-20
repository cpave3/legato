package config

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// Every setting has one home: a known section and a group within it.
func TestEverySettingNamesAKnownSectionAndAGroup(t *testing.T) {
	for _, s := range Settings() {
		if !slices.Contains(SettingSections(), s.Section) {
			t.Errorf("%s: section %q is not one of %v", s.Key, s.Section, SettingSections())
		}
		if strings.TrimSpace(s.Group) == "" {
			t.Errorf("%s has no group", s.Key)
		}
	}
}

// A whole number counted in seconds, milliseconds or bytes says so in its
// Unit, so its label need not.
func TestANumberNamedInAUnitHasThatUnit(t *testing.T) {
	for _, s := range Settings() {
		var want SettingUnit
		switch {
		case s.Kind != KindInt:
		case strings.HasSuffix(s.Key, "_seconds"):
			want = UnitSeconds
		case strings.HasSuffix(s.Key, "_ms"):
			want = UnitMs
		case strings.HasSuffix(s.Key, "_bytes"), strings.HasSuffix(s.Key, "_output_cap"):
			want = UnitBytes
		}
		if s.Unit != want {
			t.Errorf("%s: unit %q, want %q", s.Key, s.Unit, want)
		}
		if s.Kind == KindDuration && s.Unit != "" {
			t.Errorf("%s is a duration and needs no unit", s.Key)
		}
	}
}

// Help replaces Description; the API keeps description as an alias for a
// release, and the JSON carries the new fields.
func TestASettingsJSONCarriesHelpWithDescriptionAsItsAlias(t *testing.T) {
	s, _ := LookupSetting("agents.default_tool")
	if s.Help == "" {
		t.Fatal("agents.default_tool has no help")
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["help"] != s.Help || got["description"] != s.Help {
		t.Fatalf("help %v, description %v, want both %q", got["help"], got["description"], s.Help)
	}
	if got["section"] != "agents" || got["group"] != "Defaults" {
		t.Fatalf("section %v, group %v", got["section"], got["group"])
	}
	n, _ := LookupSetting("swarm.brief_kickoff_delay_ms")
	data, _ = json.Marshal(n)
	if !strings.Contains(string(data), `"unit":"ms"`) {
		t.Fatalf("no unit in %s", data)
	}
}

// The layout table names only registered settings.
func TestTheLayoutNamesOnlyRegisteredSettings(t *testing.T) {
	for key := range settingLayout {
		if strings.HasSuffix(key, ".") {
			if !slices.ContainsFunc(Settings(), func(s Setting) bool { return strings.HasPrefix(s.Key, key) }) {
				t.Errorf("settingLayout places %s*, under which nothing is registered", key)
			}
			continue
		}
		if _, ok := LookupSetting(key); !ok {
			t.Errorf("settingLayout names %s, which is not registered", key)
		}
	}
}
