package config

import (
	"strings"
	"testing"
	"time"
)

func TestAgentHistoryKeep(t *testing.T) {
	for _, tc := range []struct {
		yaml    string
		want    time.Duration
		forever bool
	}{{"", 180 * 24 * time.Hour, false}, {"keep: 30d", 30 * 24 * time.Hour, false}, {"keep: 36h", 36 * time.Hour, false},
		{"keep: 0", 0, true}, {"keep: 0s", 0, true}} {
		writeTestConfig(t, "agents:\n  history:\n    "+tc.yaml+"\n")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("%q: %v", tc.yaml, err)
		}
		got, forever := cfg.Agents.History.KeepDuration()
		if got != tc.want || forever != tc.forever {
			t.Fatalf("%q = %v forever %v, want %v forever %v", tc.yaml, got, forever, tc.want, tc.forever)
		}
	}
}

func TestAgentHistoryRejectsABadKeep(t *testing.T) {
	for _, bad := range []string{"soon", "-1d", "-2h", "1.5d"} {
		writeTestConfig(t, "agents:\n  history:\n    keep: "+bad+"\n")
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "agents.history.keep") {
			t.Fatalf("keep %s: error = %v", bad, err)
		}
	}
	setting, ok := LookupSetting("agents.history.keep")
	if !ok {
		t.Fatal("agents.history.keep is not in the settings registry")
	}
	if setting.Check == nil || setting.Check("soon") == nil || setting.Check("30d") != nil {
		t.Fatalf("agents.history.keep's check does not refuse a bad value")
	}
}
