package store

import (
	"context"
	"path/filepath"
	"testing"
)

// Before auto was a level that runs a change at once, reeve.levels.risky's
// auto meant the default (signed once a device is enrolled). A value saved
// then is renamed, so it never starts running risky changes unasked.
func TestMigrationRenamesTheOldRiskyAutoToDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "risky.db")
	s := newStoreAtMigration(t, path, "106_engram_home.sql")
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES ('reeve.levels.risky', '"auto"'), ('reeve.levels.change', '"approve"')`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	migrated, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	values, err := migrated.SettingValues(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(values["reeve.levels.risky"]); got != `"default"` {
		t.Fatalf("risky = %s, want \"default\"", got)
	}
	if got := string(values["reeve.levels.change"]); got != `"approve"` {
		t.Fatalf("change = %s, want it untouched", got)
	}
}
