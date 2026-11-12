package config

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestTheReevesLevelsTakeAuto(t *testing.T) {
	for _, key := range []string{"reeve.levels.change", "reeve.levels.risky"} {
		s, ok := LookupSetting(key)
		if !ok {
			t.Fatalf("%s is not registered", key)
		}
		if !slices.Contains(s.Enum, "auto") {
			t.Errorf("%s does not take auto: %v", key, s.Enum)
		}
	}
	risky, _ := LookupSetting("reeve.levels.risky")
	if !slices.Contains(risky.Enum, "default") {
		t.Errorf("reeve.levels.risky has no default: %v", risky.Enum)
	}
}

func TestHostAutoIsOneOfTheReevesOwnRulesAndOffByDefault(t *testing.T) {
	s, ok := LookupSetting("reeve.host_operations_auto")
	if !ok || !s.OwnRule || s.Kind != KindBool {
		t.Fatalf("reeve.host_operations_auto = %+v, %v", s, ok)
	}
	if Defaults().Reeve.HostOperationsAutoOn() {
		t.Fatal("host auto is on by default")
	}
	cfg := Defaults()
	if err := OverlayAll(cfg, map[string]json.RawMessage{"reeve.host_operations_auto": json.RawMessage("true")}, nil); err != nil {
		t.Fatal(err)
	}
	if !cfg.Reeve.HostOperationsAutoOn() {
		t.Fatal("host auto set in the database is not on")
	}
	off := false
	cfg.Reeve.HostOperations = &off
	if cfg.Reeve.HostOperationsAutoOn() {
		t.Fatal("host auto is on while host operations are off")
	}
}

// Before auto was a level, reeve.levels.risky: auto meant the default
// (signed once a device is enrolled). A file that says so keeps that
// meaning: risky changes never start running without the person because
// of a word written for something else.
func TestRiskyAutoInTheFileKeepsItsOldMeaning(t *testing.T) {
	cfg, err := LoadBytes([]byte("reeve:\n  levels:\n    risky: auto\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Reeve.Levels.Risky != "default" {
		t.Fatalf("risky = %q, want default", cfg.Reeve.Levels.Risky)
	}
}
