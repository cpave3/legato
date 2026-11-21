package config

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
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

// Every setting says what it does, and its label carries neither a group
// prefix nor a unit: the group heading and the Unit say those.
func TestEverySettingHasHelpAndAPlainLabel(t *testing.T) {
	unitInLabel := regexp.MustCompile(`\((seconds|ms|bytes|s|minutes)\)`)
	for _, s := range Settings() {
		if strings.TrimSpace(s.Help) == "" {
			t.Errorf("%s has no help", s.Key)
		}
		if strings.Contains(s.Title, ":") || unitInLabel.MatchString(s.Title) {
			t.Errorf("%s: label %q carries a prefix or a unit", s.Key, s.Title)
		}
		if strings.EqualFold(strings.TrimSuffix(s.Help, "."), s.Title) {
			t.Errorf("%s: help only restates the label", s.Key)
		}
	}
}

// A DefaultLabel says what unset means when that is not the zero value; it
// never repeats a default the registry already reports.
func TestADefaultLabelIsGivenOnlyWhenUnsetIsTheZeroValue(t *testing.T) {
	cfg := Defaults()
	for _, s := range Settings() {
		if s.DefaultLabel == "" {
			continue
		}
		v, err := SettingValue(cfg, s.Key)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.ValueOf(v).IsZero() {
			t.Errorf("%s: default %v is already reported; drop DefaultLabel %q", s.Key, v, s.DefaultLabel)
		}
	}
}

// DefaultLabel agrees with what the configuration's own accessors use when
// a setting is unset.
func TestADefaultLabelIsWhatTheRuntimeUses(t *testing.T) {
	cfg := Defaults()
	maxAge := cfg.RepositoryCache.MaxAgeDuration()
	keepIdle, _ := cfg.RepositoryCache.KeepIdleDuration()
	historyKeep, _ := cfg.Agents.History.KeepDuration()
	runtime := map[string]string{
		"repository_cache.max_age":   maxAge.String(),
		"repository_cache.keep_idle": fmt.Sprintf("%dd", int(keepIdle.Hours()/24)),
		"agents.history.keep":        fmt.Sprintf("%dd", int(historyKeep.Hours()/24)),
		"reeve.voice.pause":          cfg.Reeve.Voice.PauseDuration().String(),
		"reeve.voice.follow_up":      cfg.Reeve.Voice.FollowUpDuration().String(),
		"reeve.host_timeout":         cfg.Reeve.HostCommandTimeout().String(),
		"reeve.host_output_cap":      strconv.Itoa(cfg.Reeve.HostOutputLimit()),
		"reeve.watch.settle":         cfg.Reeve.WatchSettle().String(),
		"reeve.idle_timeout":         cfg.Reeve.ReeveIdleTimeout().String(),
		"web.rail.snooze":            cfg.Web.Rail.SnoozeDuration().String(),
		"integrations.argus.agent":   "claude-code",
	}
	for key, want := range runtime {
		s, _ := LookupSetting(key)
		if got := normalDuration(s.DefaultLabel); got != normalDuration(want) {
			t.Errorf("%s: DefaultLabel %q, the runtime uses %q", key, s.DefaultLabel, want)
		}
	}
}

// normalDuration reads "10m" and "10m0s" alike.
func normalDuration(s string) string {
	if d, err := time.ParseDuration(s); err == nil {
		return d.String()
	}
	return s
}
