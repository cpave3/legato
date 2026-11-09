package config

import (
	"encoding/json"
	"testing"
)

func TestADatabaseEntryJoinsTheFilesEntries(t *testing.T) {
	writeTestConfig(t, "workspaces:\n  - name: Work\n    color: \"#ff0000\"\nmacros:\n  - name: hi\n    keys: hello\n")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	err = OverlayEntries(cfg, map[string]map[string]json.RawMessage{
		"workspaces": {"Home": json.RawMessage(`{"color": "#00ff00"}`), "work": json.RawMessage(`{"color": "#0000ff"}`)},
		"adapters":   {"codex": json.RawMessage(`{"launch_args": ["--full-auto"]}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Workspaces) != 2 || cfg.Workspaces[0].Name != "Work" || cfg.Workspaces[0].Color != "#ff0000" || cfg.Workspaces[1].Name != "Home" {
		t.Fatalf("workspaces = %+v (the file's Work is pinned: the database's work, matched by name, is left out)", cfg.Workspaces)
	}
	if got := cfg.Adapters["codex"].LaunchArgs; len(got) != 1 || got[0] != "--full-auto" {
		t.Fatalf("adapters = %+v", cfg.Adapters)
	}
	if !cfg.PinnedEntry("workspaces", "work") || cfg.PinnedEntry("workspaces", "Home") {
		t.Fatal("pins by name are wrong")
	}
	v, ok, _ := EntryValue(cfg, "workspaces", "Home")
	if !ok || v.(map[string]any)["color"] != "#00ff00" {
		t.Fatalf("entry = %v", v)
	}
}

func TestAnEntryThatLeavesTheConfigurationInvalidIsRefused(t *testing.T) {
	writeTestConfig(t, "")
	cfg, _ := Load()
	err := OverlayEntries(cfg, map[string]map[string]json.RawMessage{
		"credentials": {"elevenlabs": json.RawMessage(`{"secret": "not a reference", "profile": "elevenlabs"}`)},
	})
	if err == nil {
		t.Fatal("an invalid credential was taken")
	}
}
