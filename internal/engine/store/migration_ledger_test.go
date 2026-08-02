package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewRecordsBackendNeutralMigrationLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.db")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	status, err := s.SchemaStatus(context.Background())
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

func TestNewRepairsPartialMultiColumnMigrationBeforeAdvancingLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial-migration.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	initial, err := migrationsFS.ReadFile("migrations/" + migrations[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(initial)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE tickets ADD COLUMN stale_at TEXT; PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := New(path)
	if err != nil {
		t.Fatalf("recover partial migration: %v", err)
	}
	defer s.Close()
	status, err := s.SchemaStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Current != len(migrations) {
		t.Fatalf("recovered schema version = %d, want %d", status.Current, len(migrations))
	}
}

func TestNewRejectsIncompatibleExistingMigrationColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "incompatible-column.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	initial, err := migrationsFS.ReadFile("migrations/" + migrations[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(initial)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE tickets ADD COLUMN stale_at INTEGER NOT NULL DEFAULT 0; PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if opened, err := New(path); err == nil || !strings.Contains(err.Error(), "incompatible") {
		if opened != nil {
			opened.Close()
		}
		t.Fatalf("error = %v, want incompatible column refusal", err)
	}
}

func TestNewResumesPartiallyAppliedReviewPassMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial-review-pass.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 34; i++ {
		data, err := migrationsFS.ReadFile("migrations/" + migrations[i])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(data)); err != nil {
			t.Fatalf("migration %d: %v", i+1, err)
		}
	}
	partial := `
		CREATE TABLE review_passes (id TEXT PRIMARY KEY, tour_id TEXT NOT NULL, number INTEGER NOT NULL, status TEXT NOT NULL DEFAULT 'capturing', summary TEXT NOT NULL DEFAULT '', guidance TEXT NOT NULL DEFAULT '', head_sha TEXT NOT NULL DEFAULT '', ready_at DATETIME, reviewed_at DATETIME, created_at DATETIME NOT NULL DEFAULT (datetime('now')), updated_at DATETIME NOT NULL DEFAULT (datetime('now')), UNIQUE(tour_id, number));
		CREATE TABLE review_pass_plans (pass_id TEXT PRIMARY KEY, plan_id TEXT NOT NULL, revision_id TEXT NOT NULL, markdown TEXT NOT NULL, created_at DATETIME NOT NULL DEFAULT (datetime('now')));
		INSERT INTO review_passes (id, tour_id, number, status, summary, head_sha, ready_at, reviewed_at, created_at, updated_at) SELECT id || '-p1', id, 1, status, summary, head_sha, ready_at, CASE WHEN status = 'reviewed' THEN updated_at ELSE NULL END, created_at, updated_at FROM review_tours;
		ALTER TABLE review_steps ADD COLUMN pass_id TEXT NOT NULL DEFAULT '';
		PRAGMA user_version = 34;`
	if _, err := db.Exec(partial); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := New(path)
	if err != nil {
		t.Fatalf("resume migration 035: %v", err)
	}
	defer s.Close()
	status, err := s.SchemaStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Current != len(migrations) {
		t.Fatalf("schema version = %d, want %d", status.Current, len(migrations))
	}
}

func TestNewRejectsMigrationLedgerGapOrWrongName(t *testing.T) {
	for _, test := range []struct {
		name string
		rows string
	}{
		{name: "gap", rows: `INSERT INTO schema_migrations(version, name) VALUES (1, ?), (3, ?)`},
		{name: "wrong name", rows: `INSERT INTO schema_migrations(version, name) VALUES (1, 'not-the-migration')`},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "invalid-ledger.db")
			db, err := sql.Open("sqlite", sqliteDSN(path))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
				t.Fatal(err)
			}
			if test.name == "gap" {
				_, err = db.Exec(test.rows, migrations[0], migrations[2])
			} else {
				_, err = db.Exec(test.rows)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`PRAGMA user_version = 3`); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if opened, err := New(path); err == nil || !strings.Contains(err.Error(), "migration ledger") {
				if opened != nil {
					opened.Close()
				}
				t.Fatalf("error = %v, want invalid migration ledger", err)
			}
		})
	}
}

func TestMigrationFailureRollsBackSchemaAndLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollback.db")
	original := migrations
	migrations = append(append([]string(nil), migrations...), "999_missing.sql")
	t.Cleanup(func() { migrations = original })

	if opened, err := New(path); err == nil {
		opened.Close()
		t.Fatal("missing migration unexpectedly succeeded")
	}
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var tables int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatalf("failed migration left %d application tables", tables)
	}
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 0 {
		t.Fatalf("failed migration left user_version %d", version)
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
