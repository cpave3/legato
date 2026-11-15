package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestThemePreferenceSurvivesPartialMigrationReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial.db")
	db, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := db.ChangeThemePreference(context.Background(), 0, "ocular-dark-vivid", "web")
	if err != nil {
		t.Fatal(err)
	}
	// A schema created before its ledger entry must not reset the preference.
	if _, err := db.DB().Exec(`DELETE FROM schema_migrations WHERE version = 113; PRAGMA user_version = 112`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	reopened, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	current, err := reopened.ThemePreference(context.Background())
	if err != nil || current != saved {
		t.Fatalf("replayed migration: %+v %v", current, err)
	}
}
