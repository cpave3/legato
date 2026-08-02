package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestNewRecordsBackendNeutralMigrationLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.db")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	status, err := s.SchemaStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status.Current != len(migrations) || status.Supported != len(migrations) {
		t.Fatalf("schema status = %#v", status)
	}
	var count int
	if err := s.db.Get(&count, "SELECT count(*) FROM schema_migrations"); err != nil {
		t.Fatal(err)
	}
	if count != len(migrations) {
		t.Fatalf("ledger entries = %d, want %d", count, len(migrations))
	}
}

func TestNewBootstrapsMigrationLedgerFromLegacyUserVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-ledger.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		data, err := migrationsFS.ReadFile("migrations/" + migrations[i])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(data)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("PRAGMA user_version = 3"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var firstName string
	if err := s.db.Get(&firstName, "SELECT name FROM schema_migrations WHERE version = 1"); err != nil {
		t.Fatal(err)
	}
	if firstName != migrations[0] {
		t.Fatalf("first migration = %q, want %q", firstName, migrations[0])
	}
}

func TestNewRejectsDivergentLegacyAndAuthoritativeSchemaVersions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "divergent-ledger.db")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	if opened, err := New(path); err == nil {
		opened.Close()
		t.Fatal("divergent schema markers were accepted")
	}
}

func TestNewUsesLedgerAsAuthoritativeSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future-ledger.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations(version, name) VALUES (?, 'future')`, len(migrations)+1); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if s, err := New(path); err == nil {
		s.Close()
		t.Fatal("future ledger was accepted")
	}
}
