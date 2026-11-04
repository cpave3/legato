package config

import (
	"reflect"
	"testing"
)

func TestBurnliteDefaults(t *testing.T) {
	writeTestConfig(t, `theme: default`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	b := cfg.Burnlite
	if b.URL != "" || len(b.Projects) != 0 || b.SyncIntervalSeconds != 60 || !b.StatusSyncEnabled {
		t.Errorf("burnlite = %+v", b)
	}
	want := BurnliteColumns{Ready: "Ready", Blocked: "Backlog", Claimed: "Doing", Closed: "Done"}
	if b.Columns != want {
		t.Errorf("columns = %+v, want %+v", b.Columns, want)
	}
}

func TestBurnliteSettingsAreRead(t *testing.T) {
	writeTestConfig(t, `
burnlite:
  url: https://burnlite.example.com/
  projects: [EST, OPS]
  sync_interval_seconds: 120
  status_sync_enabled: false
  columns:
    claimed: Review
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	b := cfg.Burnlite
	if b.URL != "https://burnlite.example.com/" || !reflect.DeepEqual(b.Projects, []string{"EST", "OPS"}) || b.SyncIntervalSeconds != 120 || b.StatusSyncEnabled {
		t.Errorf("burnlite = %+v", b)
	}
	// A column left out keeps its default.
	if want := (BurnliteColumns{Ready: "Ready", Blocked: "Backlog", Claimed: "Review", Closed: "Done"}); b.Columns != want {
		t.Errorf("columns = %+v, want %+v", b.Columns, want)
	}
}
