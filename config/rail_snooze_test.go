package config

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTheRailSnoozeIsThirtyMinutesUnlessSet(t *testing.T) {
	if got := (RailConfig{}).SnoozeDuration(); got != 30*time.Minute {
		t.Fatalf("unset snooze = %v, want 30m", got)
	}
	if got := (RailConfig{Snooze: "2h"}).SnoozeDuration(); got != 2*time.Hour {
		t.Fatalf("snooze 2h = %v", got)
	}
	// What the check refuses never reaches here, but a file can hold it.
	for _, bad := range []string{"0s", "-5m", "soon"} {
		if got := (RailConfig{Snooze: bad}).SnoozeDuration(); got != 30*time.Minute {
			t.Errorf("snooze %q = %v, want the default", bad, got)
		}
	}
}

func TestTheRailSnoozeSettingTakesOnlyAPositiveDuration(t *testing.T) {
	s, ok := LookupSetting("web.rail.snooze")
	if !ok {
		t.Fatal("web.rail.snooze is not a setting")
	}
	if s.Kind != KindDuration || s.Mode != ModeLive {
		t.Fatalf("web.rail.snooze is %s, %s; want a live duration", s.Kind, s.Mode)
	}
	for _, raw := range []string{`"0s"`, `"-1m"`, `"later"`} {
		if _, err := DecodeSetting(s, json.RawMessage(raw)); err == nil {
			t.Errorf("web.rail.snooze = %s was taken", raw)
		}
	}
	for _, raw := range []string{`"45m"`, `"2h"`, `""`} {
		if _, err := DecodeSetting(s, json.RawMessage(raw)); err != nil {
			t.Errorf("web.rail.snooze = %s: %v", raw, err)
		}
	}
}
