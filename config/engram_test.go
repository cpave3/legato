package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEngramDefaultsToAuto(t *testing.T) {
	cfg, err := LoadBytes([]byte(""))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Engram.Enabled != EngramAuto || cfg.Engram.Command != "" {
		t.Fatalf("engram = %+v, want auto with no command", cfg.Engram)
	}
}

func TestEngramLoadsItsSettings(t *testing.T) {
	cfg, err := LoadBytes([]byte("engram:\n  enabled: on\n  command: /opt/engram/bin/engram\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Engram.Enabled != EngramOn || cfg.Engram.Command != "/opt/engram/bin/engram" {
		t.Fatalf("engram = %+v", cfg.Engram)
	}
}

func TestEngramRejectsAnUnknownMode(t *testing.T) {
	_, err := LoadBytes([]byte("engram:\n  enabled: sometimes\n"))
	if err == nil || !strings.Contains(err.Error(), "engram.enabled") {
		t.Fatalf("err = %v, want one naming engram.enabled", err)
	}
}

func TestEngramSettingsLiveInTheDatabase(t *testing.T) {
	for _, key := range []string{"engram.enabled", "engram.command"} {
		s, ok := LookupSetting(key)
		if !ok {
			t.Fatalf("%s is not registered", key)
		}
		if s.Mode != ModeLive {
			t.Errorf("%s mode = %s, want live: the probe reads it for each spawn dialog", key, s.Mode)
		}
	}
	cfg := Defaults()
	if err := Overlay(cfg, map[string]json.RawMessage{"engram.enabled": json.RawMessage(`"off"`)}); err != nil {
		t.Fatal(err)
	}
	if cfg.Engram.Enabled != EngramOff {
		t.Fatalf("enabled = %q after overlay", cfg.Engram.Enabled)
	}
}
