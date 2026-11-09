package store

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/jmoiron/sqlx"
)

// The tint, the update mode and Burnlite's projects were instance values;
// the settings table takes them, keeping their values.
func TestSettingsMigrationMovesInstanceValuesKeepingThem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pre-settings.db")
	db, err := sqlx.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	before := 0
	for i, name := range migrations {
		if name == "098_settings.sql" {
			before = i
			break
		}
		data, _ := migrationsFS.ReadFile("migrations/" + name)
		sqlText, err := prepareMigration(db, name, string(data))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(sqlText); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO instance_values (key, value) VALUES ('web.tint', 'teal'), ('updates.mode', 'off'), ('burnlite.projects', 'EST,LEG'), ('instance.id', 'keep-me')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version = ` + strconv.Itoa(before)); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	values, err := s.SettingValues(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"web.tint": `"teal"`, "updates.mode": `"off"`, "burnlite.projects": `["EST","LEG"]`} {
		if string(values[key]) != want {
			t.Errorf("%s = %s, want %s", key, values[key], want)
		}
	}
	for _, key := range []string{"web.tint", "updates.mode", "burnlite.projects"} {
		if v, _ := s.GetInstanceValue(t.Context(), key); v != "" {
			t.Errorf("%s is still an instance value: %q", key, v)
		}
	}
	if v, _ := s.GetInstanceValue(t.Context(), "instance.id"); v != "keep-me" {
		t.Errorf("another instance value was touched: %q", v)
	}
}

