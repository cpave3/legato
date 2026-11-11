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
