package store

import (
	"database/sql"
	"fmt"
	"log"
	"sort"
	"strings"
)

// ledgerReordering is a migration order a release shipped that is not the
// order of the sorted file names now: a release tagged while a branch held a
// migration whose name sorts before ones the release had. Its databases
// record those migrations at other positions, which validateMigrationLedger
// refuses, so New repairs them before validating (LEG-249).
//
// Only the orders listed in knownLedgerReorderings are repaired. Every other
// mismatch still fails closed.
type ledgerReordering struct {
	release  string   // the release that shipped the order, for the log
	after    int      // ledger rows before released that must match migrations
	released []string // the names at positions after+1… in that release
	missing  []string // migrations that release lacked, in sorted order
}

var knownLedgerReorderings = []ledgerReordering{{
	release:  "v0.21.0",
	after:    114,
	released: []string{"116_skill_states.sql", "118_oauth_attempts.sql", "119_mcp_connections.sql"},
	missing:  []string{"117_agent_docker_host_network.sql"},
}}

type migrationExecer interface {
	migrationQueryer
	Exec(query string, args ...any) (sql.Result, error)
}

type ledgerRow struct {
	Version   int    `db:"version"`
	Name      string `db:"name"`
	AppliedAt string `db:"applied_at"`
}

func readLedger(queryer migrationQueryer) ([]ledgerRow, error) {
	var rows []ledgerRow
	err := queryer.Select(&rows, `SELECT version, name, applied_at FROM schema_migrations ORDER BY version`)
	return rows, err
}

// matchLedgerReordering returns the known reordering a ledger holds exactly:
// its first rows as migrations has them, then the released order, and
// nothing after it.
func matchLedgerReordering(rows []ledgerRow) (ledgerReordering, bool) {
	for _, r := range knownLedgerReorderings {
		if len(rows) != r.after+len(r.released) || r.after+len(r.released)+len(r.missing) > len(migrations) {
			continue
		}
		matches := true
		for i, row := range rows {
			want := ""
			if i < r.after {
				want = migrations[i]
			} else {
				want = r.released[i-r.after]
			}
			if row.Version != i+1 || row.Name != want {
				matches = false
				break
			}
		}
		if matches {
			return r, true
		}
	}
	return ledgerReordering{}, false
}

// repairLedgerReordering, in the caller's transaction, applies the
// migrations a known reordering lacked and renumbers its rows to the sorted
// order, keeping when each was applied. It does nothing to any other ledger.
func repairLedgerReordering(tx migrationExecer) error {
	rows, err := readLedger(tx)
	if err != nil {
		return err
	}
	r, ok := matchLedgerReordering(rows)
	if !ok {
		return nil
	}
	var userVersion int
	if err := tx.Get(&userVersion, "PRAGMA user_version"); err != nil {
		return err
	}
	if userVersion != len(rows) {
		// The schema markers disagree: leave it to the usual refusal.
		return nil
	}
	for _, name := range r.missing {
		data, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", name, err)
		}
		// prepareMigration drops an ADD COLUMN whose column already exists,
		// as on a database someone repaired in part by hand.
		sqlText, err := prepareMigration(tx, name, string(data))
		if err != nil {
			return fmt.Errorf("preparing migration %s: %w", name, err)
		}
		if _, err := tx.Exec(sqlText); err != nil {
			return fmt.Errorf("executing migration %s: %w", name, err)
		}
	}
	applied := map[string]string{}
	for _, row := range rows[r.after:] {
		applied[row.Name] = row.AppliedAt
	}
	if _, err := tx.Exec(`DELETE FROM schema_migrations WHERE version > ?`, r.after); err != nil {
		return fmt.Errorf("renumbering migration ledger: %w", err)
	}
	total := r.after + len(r.released) + len(r.missing)
	for i := r.after; i < total; i++ {
		name := migrations[i]
		if at, ok := applied[name]; ok {
			_, err = tx.Exec(`INSERT INTO schema_migrations(version, name, applied_at) VALUES (?, ?, ?)`, i+1, name, at)
		} else {
			_, err = tx.Exec(`INSERT INTO schema_migrations(version, name) VALUES (?, ?)`, i+1, name)
		}
		if err != nil {
			return fmt.Errorf("renumbering migration ledger: %w", err)
		}
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", total)); err != nil {
		return err
	}
	log.Printf("database: repaired the migration order %s shipped: applied %s and renumbered the ledger from version %d (%s)",
		r.release, strings.Join(r.missing, ", "), r.after+1, strings.Join(migrations[r.after:total], ", "))
	return nil
}

func sortedNames(names []string) []string {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	return sorted
}
