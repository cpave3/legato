package config

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTheVoicePauseIsASecondUnlessSetAndStaysInItsRange(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"": time.Second, "500ms": 500 * time.Millisecond, "2s": 2 * time.Second,
		"100ms": 300 * time.Millisecond, "5s": 2 * time.Second, "soon": time.Second,
	} {
		if got := (ReeveVoiceConfig{Pause: in}).PauseDuration(); got != want {
			t.Errorf("pause %q = %v, want %v", in, got, want)
		}
	}
}

func TestTheFollowUpWindowIsSixSecondsUnlessSetAndZeroMeansOneTurn(t *testing.T) {
	for in, want := range map[string]time.Duration{"": 6 * time.Second, "0s": 0, "0": 0, "10s": 10 * time.Second, "-1s": 6 * time.Second} {
		if got := (ReeveVoiceConfig{FollowUp: in}).FollowUpDuration(); got != want {
			t.Errorf("follow_up %q = %v, want %v", in, got, want)
		}
	}
}

func TestPartialWordsAreOnUnlessTurnedOff(t *testing.T) {
	off := false
	if !(ReeveVoiceConfig{}).PartialsOn() || (ReeveVoiceConfig{Partials: &off}).PartialsOn() {
		t.Fatal("partials")
	}
}

func TestTheVoiceSettingsAreCheckedByTheirRules(t *testing.T) {
	for key, raw := range map[string]string{
		"reeve.voice.pause":     `"3s"`,
		"reeve.voice.follow_up": `"-2s"`,
	} {
		s, ok := LookupSetting(key)
		if !ok {
			t.Fatalf("%s is not a setting", key)
		}
		if _, err := DecodeSetting(s, json.RawMessage(raw)); err == nil {
			t.Errorf("%s = %s was taken", key, raw)
		}
	}
	for key, raw := range map[string]string{
		"reeve.voice.pause":     `"800ms"`,
		"reeve.voice.follow_up": `"0s"`,
		"reeve.voice.partials":  `false`,
	} {
		s, ok := LookupSetting(key)
		if !ok {
			t.Fatalf("%s is not a setting", key)
		}
		if _, err := DecodeSetting(s, json.RawMessage(raw)); err != nil {
			t.Errorf("%s = %s: %v", key, raw, err)
		}
	}
}

func TestQuietHoursCoverTheirRangeInLocalTimeAcrossMidnight(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 11, 11, h, m, 0, 0, time.Local) }
	night := ReeveVoiceConfig{QuietHours: "22:00-07:00"}
	for _, c := range []struct {
		t    time.Time
		want bool
	}{{at(23, 30), true}, {at(2, 0), true}, {at(6, 59), true}, {at(7, 0), false}, {at(12, 0), false}, {at(22, 0), true}, {at(21, 59), false}} {
		if got := night.QuietAt(c.t); got != c.want {
			t.Errorf("22:00-07:00 at %s = %v", c.t.Format("15:04"), got)
		}
	}
	lunch := ReeveVoiceConfig{QuietHours: "12:00-13:30"}
	if !lunch.QuietAt(at(12, 45)) || lunch.QuietAt(at(13, 30)) {
		t.Error("12:00-13:30")
	}
	if (ReeveVoiceConfig{}).QuietAt(at(3, 0)) || (ReeveVoiceConfig{QuietHours: "late"}).QuietAt(at(3, 0)) {
		t.Error("no quiet hours, or ones that do not parse, are never quiet")
	}
}

func TestQuietHoursAreCheckedByTheirRule(t *testing.T) {
	s, ok := LookupSetting("reeve.voice.quiet_hours")
	if !ok {
		t.Fatal("not a setting")
	}
	for _, bad := range []string{`"10pm-7am"`, `"22:00"`, `"25:00-07:00"`, `"22:00-22:00"`} {
		if _, err := DecodeSetting(s, json.RawMessage(bad)); err == nil {
			t.Errorf("%s was taken", bad)
		}
	}
	for _, good := range []string{`""`, `"22:00-07:00"`, `"9:30-10:00"`} {
		if _, err := DecodeSetting(s, json.RawMessage(good)); err != nil {
			t.Errorf("%s: %v", good, err)
		}
	}
}
