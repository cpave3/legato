package config

import (
	"reflect"
	"testing"
)

// A setting kept as a pointer is unset when nil, and the runtime gives it a
// meaning then. The registry must report that same value as the default, so
// every such setting is named here with what nil means where it is read.
// A new one fails until it is added.
func TestANilSettingReportsTheDefaultTheRuntimeUses(t *testing.T) {
	runtimeDefault := map[string]any{
		"notifications.approvals":       true,
		"notifications.handoff":         true,
		"agents.sandbox.default_mounts": true,
		"reeve.host_operations":         true,
		// cmd/legato announceWebServer: off only when set to false.
		"web.discovery": true,
		// GHChannelConfig.IsolateLogin: nil is on.
		"credential_channels.gh.isolate_login": true,
	}
	cfg := Defaults()
	for _, s := range Settings() {
		field, err := settingField(cfg, s.Key, false)
		if err != nil || field.Kind() != reflect.Pointer {
			continue
		}
		want, named := runtimeDefault[s.Key]
		if !named {
			want = reflect.Zero(field.Type().Elem()).Interface()
			t.Errorf("%s is kept as a pointer: name what nil means at runtime in this test (zero value %v?)", s.Key, want)
			continue
		}
		if !field.IsNil() {
			continue
		}
		got, err := SettingValue(cfg, s.Key)
		if err != nil || got != want {
			t.Errorf("%s with nothing set = %v (%v), the runtime uses %v", s.Key, got, err, want)
		}
	}
	if !cfg.CredentialChannels.GH.IsolateLogin() {
		t.Fatal("isolate_login is no longer on when unset; update this test")
	}
}
