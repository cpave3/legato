package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTheReeveNameIsALiveSettingOfItsOwn(t *testing.T) {
	s, ok := LookupSetting("reeve.name")
	if !ok {
		t.Fatal("reeve.name is not a setting")
	}
	if s.Kind != KindString || s.Mode != ModeLive || s.Class != ClassChange || s.OwnRule {
		t.Fatalf("reeve.name = %+v, want a live string changed with a tap, not an own rule", s)
	}
}

func TestTheReeveNameIsOneLineOfPlainText(t *testing.T) {
	for _, ok := range []string{"", "Tessitura", "  Tess  ", "Anne-Marie", "O'Neill", "Zoë", "R2 D2"} {
		if err := CheckReeveName(ok); err != nil {
			t.Errorf("CheckReeveName(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{
		"Tess\nitura", "Tess\titura", "\x1b[31mred", "**Tess**", "[Tess](http://x)", "`Tess`", "# Tess", "<b>Tess</b>", "Tess_",
		strings.Repeat("a", 41),
	} {
		if err := CheckReeveName(bad); err == nil {
			t.Errorf("CheckReeveName(%q) = nil, want an error", bad)
		}
	}
	if err := CheckReeveName(strings.Repeat("é", 40)); err != nil {
		t.Errorf("40 letters: %v, want nil (the limit counts characters, not bytes)", err)
	}
}

func TestTheReeveNameSettingIsChecked(t *testing.T) {
	s, _ := LookupSetting("reeve.name")
	if _, err := DecodeSetting(s, json.RawMessage(`"**Tess**"`)); err == nil {
		t.Fatal("a Markdown name was accepted")
	}
	if v, err := DecodeSetting(s, json.RawMessage(`"Tessitura"`)); err != nil || v != "Tessitura" {
		t.Fatalf("DecodeSetting = %v, %v", v, err)
	}
}

func TestTheReeveNameIsReadTrimmedAndAnInvalidOneIsNone(t *testing.T) {
	for in, want := range map[string]string{"": "", "  Tessitura ": "Tessitura", "**Tess**": ""} {
		if got := (ReeveConfig{Name: in}).DisplayName(); got != want {
			t.Errorf("DisplayName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConfigYamlWithABadReeveNameDoesNotLoad(t *testing.T) {
	if _, err := LoadBytes([]byte("reeve:\n  name: \"[Tess](x)\"\n")); err == nil {
		t.Fatal("loaded a Markdown reeve.name")
	}
	cfg, err := LoadBytes([]byte("reeve:\n  name: Tessitura\n"))
	if err != nil || cfg.Reeve.DisplayName() != "Tessitura" {
		t.Fatalf("LoadBytes = %v, %v", cfg, err)
	}
}
