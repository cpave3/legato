package config

import (
	"strings"
	"testing"
	"time"
)

func TestSubscriptionUsageReadsBothProvidersEveryFiveMinutesByDefault(t *testing.T) {
	cfg := Defaults()
	if !cfg.Usage.Claude.IsEnabled() || !cfg.Usage.Codex.IsEnabled() {
		t.Fatal("a provider is off by default")
	}
	if got := cfg.Usage.PollIntervalDuration(); got != 5*time.Minute {
		t.Fatalf("poll interval = %s", got)
	}
	for _, key := range []string{"usage.claude.enabled", "usage.codex.enabled"} {
		if v, err := SettingValue(cfg, key); err != nil || v != true {
			t.Fatalf("%s = %v, %v", key, v, err)
		}
	}
}

func TestSubscriptionUsageSettingsAreReadFromTheFile(t *testing.T) {
	cfg, err := LoadBytes([]byte("usage:\n  poll_interval: 10m\n  codex:\n    enabled: false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Usage.PollIntervalDuration() != 10*time.Minute || cfg.Usage.Codex.IsEnabled() || !cfg.Usage.Claude.IsEnabled() {
		t.Fatalf("usage = %+v", cfg.Usage)
	}
}

func TestAPollIntervalUnderTwoMinutesIsRefused(t *testing.T) {
	if _, err := LoadBytes([]byte("usage:\n  poll_interval: 30s\n")); err == nil || !strings.Contains(err.Error(), "usage.poll_interval") {
		t.Fatalf("err = %v", err)
	}
	s, _ := LookupSetting("usage.poll_interval")
	if _, err := DecodeSetting(s, []byte(`"1m"`)); err == nil {
		t.Fatal("the setting took 1m")
	}
	if _, err := DecodeSetting(s, []byte(`"15m"`)); err != nil {
		t.Fatalf("15m: %v", err)
	}
}
