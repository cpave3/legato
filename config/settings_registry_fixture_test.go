package config

import (
	"encoding/json"
	"flag"
	"os"
	"testing"
)

var updateRegistryFixture = flag.Bool("update", false, "rewrite the web UI's copy of the settings registry")

// The web UI's Settings page test reads this file to check that every key
// and list in the registry has exactly one row to open at. It is a copy, so
// that test needs no Go toolchain and does not compile Go inside its timeout.
const registryFixture = "../web/src/pages/__tests__/fixtures/settings-registry.json"

func registryFixtureJSON(t *testing.T) []byte {
	t.Helper()
	data, err := json.MarshalIndent(map[string]any{
		"settings":    Settings(),
		"file_only":   FileOnlySettings(),
		"lists":       EntryListDefs(),
		"entry_lists": EntryLists(),
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func TestTheWebSettingsRegistryFixtureIsCurrent(t *testing.T) {
	want := registryFixtureJSON(t)
	if *updateRegistryFixture {
		if err := os.WriteFile(registryFixture, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(registryFixture)
	if err != nil {
		t.Fatalf("%v (run go test ./config -run TestTheWebSettingsRegistryFixtureIsCurrent -update)", err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s is out of date with the settings registry (run go test ./config -run TestTheWebSettingsRegistryFixtureIsCurrent -update)", registryFixture)
	}
}
