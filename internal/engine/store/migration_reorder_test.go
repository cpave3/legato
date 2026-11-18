package store

import (
	"bytes"
	"database/sql"
	"log"
	"path/filepath"
	"strings"
	"testing"
)

// v021Migrations is the migration list v0.21.0 shipped: it was tagged before
// 117_agent_docker_host_network.sql was merged, and before 120 and 121.
func v021Migrations(t *testing.T) []string {
	t.Helper()
	var names []string
	for _, name := range migrations {
		if name == "117_agent_docker_host_network.sql" || name > "119_mcp_connections.sql" {
			continue
		}
		names = append(names, name)
	}
	tail := strings.Join(names[len(names)-3:], " ")
	if len(names) != 117 || tail != "116_skill_states.sql 118_oauth_attempts.sql 119_mcp_connections.sql" {
		t.Fatalf("v0.21.0 list has %d migrations ending %s", len(names), tail)
	}
	return names
}

// openAsV021 makes a database as v0.21.0 left it: its own migration code run
// with v0.21.0's list. Not parallel: it swaps the package's migration list.
func openAsV021(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "v021.db")
	current := migrations
	migrations = v021Migrations(t)
	s, err := New(path)
	migrations = current
	if err != nil {
		t.Fatalf("opening as v0.21.0: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func ledgerNames(t *testing.T, s *Store) []string {
	t.Helper()
	var names []string
	if err := s.db.Select(&names, `SELECT name FROM schema_migrations ORDER BY version`); err != nil {
		t.Fatal(err)
	}
	return names
}

func assertCurrentSchema(t *testing.T, s *Store) {
	t.Helper()
	names := ledgerNames(t, s)
	if strings.Join(names, ",") != strings.Join(migrations, ",") {
		t.Fatalf("ledger after open:\n%v\nwant\n%v", names, migrations)
	}
	var userVersion, hostNetwork int
	if err := s.db.Get(&userVersion, `PRAGMA user_version`); err != nil {
		t.Fatal(err)
	}
	if userVersion != len(migrations) {
		t.Fatalf("user_version = %d, want %d", userVersion, len(migrations))
	}
	if err := s.db.Get(&hostNetwork, `SELECT COUNT(*) FROM pragma_table_info('agent_sessions') WHERE name = 'docker_host_network'`); err != nil {
		t.Fatal(err)
	}
	if hostNetwork != 1 {
		t.Fatal("agent_sessions.docker_host_network is missing")
	}
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev); log.SetFlags(flags) })
	return &buf
}

// LEG-249: every database that ran v0.21.0 holds 118 and 119 where this
// binary wants 117 and 118, and v0.22.0 refused to open it.
func TestNewRepairsTheV021MigrationOrder(t *testing.T) {
	path := openAsV021(t)
	logged := captureLog(t)

	s, err := New(path)
	if err != nil {
		t.Fatalf("opening a v0.21.0 database: %v", err)
	}
	defer s.Close()
	assertCurrentSchema(t, s)
	if !strings.Contains(logged.String(), "117_agent_docker_host_network.sql") || !strings.Contains(logged.String(), "v0.21.0") {
		t.Errorf("the repair was not logged: %q", logged.String())
	}
}

// legato-1 had the column added by hand, ledger untouched: the repair must
// not add it again.
func TestNewRepairsTheV021OrderWhenTheColumnExists(t *testing.T) {
	path := openAsV021(t)
	db := rawDB(t, path)
	if _, err := db.Exec(`ALTER TABLE agent_sessions ADD COLUMN docker_host_network INTEGER NOT NULL DEFAULT 0`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := New(path)
	if err != nil {
		t.Fatalf("opening a v0.21.0 database with the column: %v", err)
	}
	defer s.Close()
	assertCurrentSchema(t, s)
}

// legato-1 after its manual repair: column, renumbered ledger, user_version
// 118. Nothing to repair; the newer migrations apply as usual.
func TestNewOpensAV021DatabaseRepairedByHand(t *testing.T) {
	path := openAsV021(t)
	db := rawDB(t, path)
	for _, statement := range []string{
		`ALTER TABLE agent_sessions ADD COLUMN docker_host_network INTEGER NOT NULL DEFAULT 0`,
		`UPDATE schema_migrations SET version = 118 WHERE version = 117`,
		`UPDATE schema_migrations SET version = 117 WHERE version = 116`,
		`INSERT INTO schema_migrations(version, name) VALUES (116, '117_agent_docker_host_network.sql')`,
		`PRAGMA user_version = 118`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	db.Close()

	s, err := New(path)
	if err != nil {
		t.Fatalf("opening a v0.21.0 database repaired by hand: %v", err)
	}
	defer s.Close()
	assertCurrentSchema(t, s)
}

// Only the known reorderings are repaired; any other mismatch fails closed.
func TestNewStillRefusesAnUnknownLedgerMismatch(t *testing.T) {
	cases := map[string][]string{
		"a released order no table names": {
			`UPDATE schema_migrations SET name = '119_mcp_connections.sql' WHERE version = 116`,
			`UPDATE schema_migrations SET name = '118_oauth_attempts.sql' WHERE version = 117`,
		},
		"the v0.21.0 tail with a wrong row before it": {
			`UPDATE schema_migrations SET name = '999_unknown.sql' WHERE version = 100`,
		},
		"the v0.21.0 tail and a row after it": {
			`INSERT INTO schema_migrations(version, name) VALUES (118, '120_mcp_stdio.sql')`,
			`PRAGMA user_version = 118`,
		},
	}
	for name, statements := range cases {
		t.Run(name, func(t *testing.T) {
			path := openAsV021(t)
			db := rawDB(t, path)
			for _, statement := range statements {
				if _, err := db.Exec(statement); err != nil {
					t.Fatalf("%s: %v", statement, err)
				}
			}
			db.Close()

			s, err := New(path)
			if err == nil {
				s.Close()
				t.Fatal("an unknown ledger mismatch was accepted")
			}
			if !strings.Contains(err.Error(), "migration ledger entry") || !strings.Contains(err.Error(), "is invalid") {
				t.Fatalf("error = %v, want today's ledger refusal", err)
			}
		})
	}
}

// Each known reordering must describe this binary's list: the released
// order, rewritten with the migrations it lacked, is the sorted order here.
func TestKnownLedgerReorderingsMatchTheEmbeddedMigrations(t *testing.T) {
	if len(knownLedgerReorderings) == 0 {
		t.Fatal("no known reorderings")
	}
	for _, r := range knownLedgerReorderings {
		want := migrations[r.after : r.after+len(r.released)+len(r.missing)]
		got := sortedNames(append(append([]string{}, r.released...), r.missing...))
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: released plus missing = %v, embedded = %v", r.release, got, want)
		}
	}
}

func rawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	return db
}
