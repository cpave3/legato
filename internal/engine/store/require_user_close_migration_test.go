package store

import (
	"context"
	"path/filepath"
	"testing"
)

// swarm.require_user_close was removed; a value saved for it is deleted so
// it never shows as an unknown setting.
func TestMigrationDeletesTheRemovedRequireUserCloseSetting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "close.db")
	s := newStoreAtMigration(t, path, "128_operator_items.sql")
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES ('swarm.require_user_close', 'true'), ('swarm.strict_scope', 'true')`); err != nil {
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
	if _, ok := values["swarm.require_user_close"]; ok {
		t.Fatal("swarm.require_user_close is still stored")
	}
	if got := string(values["swarm.strict_scope"]); got != "true" {
		t.Fatalf("strict_scope = %s, want it untouched", got)
	}
}
