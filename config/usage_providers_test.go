package config

import (
	"reflect"
	"testing"
)

// The provider table is the one place that says which subscriptions Legato
// can read, what they are called and which meters they report: the service's
// names and labels, the TUI's and the web's catalog, and the meters usage
// rules and pins may name all derive from it.
func TestUsageProvidersDeclareIdentityAndMeters(t *testing.T) {
	providers := UsageProviders()
	var ids []string
	for _, p := range providers {
		ids = append(ids, p.ID)
	}
	if want := []string{"claude", "codex", "opencode"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("provider ids = %v", ids)
	}
	names := map[string]string{}
	for _, p := range providers {
		names[p.ID] = p.Name
	}
	if want := map[string]string{"claude": "Claude", "codex": "Codex", "opencode": "OpenCode Go"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("provider names = %v", names)
	}

	// The six existing qualified meters keep their order and identity; Go
	// adds its three.
	got := UsageQualifiedMeters()
	want := []string{
		"claude.five_hour", "claude.weekly", "claude.weekly_opus", "claude.weekly_sonnet",
		"codex.five_hour", "codex.weekly",
		"opencode.five_hour", "opencode.weekly", "opencode.monthly",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("qualified meters = %v", got)
	}
	for _, meter := range want {
		if !IsUsageMeter(meter) {
			t.Errorf("%s is not a qualified meter", meter)
		}
	}
	for _, meter := range []string{"opencode.rolling", "opencode.seven_day", "", "claude.monthly"} {
		if IsUsageMeter(meter) {
			t.Errorf("%q qualified", meter)
		}
	}
}

func TestUsageMetersCarryTheirOwnLabels(t *testing.T) {
	cases := map[string]UsageMeterInfo{
		"five_hour":     {Window: "five_hour", Short: "5h", Long: "5-hour"},
		"weekly":        {Window: "weekly", Short: "wk", Long: "weekly"},
		"weekly_opus":   {Window: "weekly_opus", Short: "Opus", Long: "weekly Opus"},
		"weekly_sonnet": {Window: "weekly_sonnet", Short: "Sonnet", Long: "weekly Sonnet"},
		"monthly":       {Window: "monthly", Short: "mo", Long: "monthly"},
	}
	for window, want := range cases {
		got, ok := UsageMeterInfoOf(window)
		if !ok {
			t.Fatalf("%s is not a known meter", window)
		}
		if got != want {
			t.Errorf("%s = %+v, want %+v", window, got, want)
		}
	}
	if _, ok := UsageMeterInfoOf("rolling"); ok {
		t.Error("rolling is the endpoint's name, not a meter")
	}
}

// OpenCode Go is opt-in: it needs a configured key and makes external calls,
// so unlike the Claude and Codex readers it is off when unset.
func TestOpenCodeUsageIsOffUntilItIsTurnedOnAndNamesAKeySecret(t *testing.T) {
	cfg := Defaults()
	if cfg.Usage.OpenCode.IsEnabled() {
		t.Fatal("OpenCode Go reads usage by default")
	}
	if key, err := SettingValue(cfg, "usage.opencode.api_key"); err != nil || key != "" {
		t.Fatalf("api_key = %v, %v", key, err)
	}
	if v, err := SettingValue(cfg, "usage.opencode.enabled"); err != nil || v != false {
		t.Fatalf("enabled = %v, %v", v, err)
	}

	cfg, err := LoadBytes([]byte("usage:\n  opencode:\n    enabled: true\n    api_key: legato://opencode-go\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Usage.OpenCode.IsEnabled() || cfg.Usage.OpenCode.APIKey != "legato://opencode-go" {
		t.Fatalf("opencode = %+v", cfg.Usage.OpenCode)
	}

	enabled, ok := LookupSetting("usage.opencode.enabled")
	if !ok {
		t.Fatal("usage.opencode.enabled is not a setting")
	}
	if enabled.Secret || enabled.Class != ClassChange || enabled.Mode != ModeRestart {
		t.Fatalf("usage.opencode.enabled = %+v", enabled)
	}
	key, ok := LookupSetting("usage.opencode.api_key")
	if !ok {
		t.Fatal("usage.opencode.api_key is not a setting")
	}
	if !key.Secret || key.Class != ClassRisky || key.Mode != ModeRestart {
		t.Fatalf("usage.opencode.api_key = %+v", key)
	}
	// The key setting holds a secret's name, never the key itself.
	if _, err := DecodeSetting(key, []byte(`"opencode-secret-key"`)); err == nil {
		t.Error("a plain key value was accepted")
	}
	if _, err := DecodeSetting(key, []byte(`"op://vault/opencode/key"`)); err != nil {
		t.Errorf("a 1Password reference was refused: %v", err)
	}
}
