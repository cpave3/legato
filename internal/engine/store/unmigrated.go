package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jmoiron/sqlx"
)

// Unmigrated is a database opened without migrating it, for an upgrade
// that must look before it migrates: which migrations are pending, a
// backup of the database as it is, then Migrate. It offers no store
// behaviour; Migrate returns the Store.
type Unmigrated struct {
	db       *sqlx.DB
	path     string
	migrated bool
}

// OpenWithoutMigrating opens the database options name as it is. It refuses
// one this binary cannot migrate (a newer schema, a ledger that disagrees),
// as Open does, but changes nothing: no migration runs and the journal mode
// is left alone. A database that does not exist yet is created empty.
func OpenWithoutMigrating(options Options) (*Unmigrated, error) {
	if err := checkDriver(options.Driver); err != nil {
		return nil, err
	}
	db, err := openChecked(options.Path)
	if err != nil {
		return nil, err
	}
	return &Unmigrated{db: db, path: options.Path}, nil
}

// Close closes the database without migrating it. After Migrate it does
// nothing: the database is the Store's.
func (u *Unmigrated) Close() error {
	if u.migrated {
		return nil
	}
	return u.db.Close()
}

// Migrate runs the pending migrations and returns the Store, which owns the
// database from then on. Take the Backup first. When it fails the database
// is closed, and left as it was: migrations commit together.
func (u *Unmigrated) Migrate(ctx context.Context) (*Store, error) {
	if u.migrated {
		return nil, errors.New("the database has already been migrated")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	u.migrated = true
	return migrateOpened(u.db)
}

// BackupPath is where Backup puts the copy of the database at path taken
// before the migration named: beside it, as
// <path>.before-<migration without .sql>.
func BackupPath(path, migration string) string {
	return path + ".before-" + strings.TrimSuffix(migration, ".sql")
}

// Backup copies the database as it is beside it, named for the newest
// pending migration (BackupPath), and returns the copy's path. The copy is
// consistent (SQLite's VACUUM INTO reads one snapshot), readable only by
// its owner, and appears under its name only once complete; a copy already
// there from an earlier try is replaced. It writes nothing and returns ""
// when no migration is pending, or when none has run (a new database has
// nothing to go back to).
func (u *Unmigrated) Backup(ctx context.Context) (string, error) {
	if u.migrated {
		return "", errors.New("the database has already been migrated")
	}
	pending, err := u.Pending(ctx)
	if err != nil {
		return "", err
	}
	if len(pending) == 0 || len(pending) == len(migrations) {
		return "", nil
	}
	target := BackupPath(u.path, pending[len(pending)-1])
	partial := target + ".partial"
	if err := os.Remove(partial); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("removing an unfinished backup: %w", err)
	}
	if _, err := u.db.ExecContext(ctx, "VACUUM INTO ?", partial); err != nil {
		os.Remove(partial)
		return "", fmt.Errorf("backing up the database: %w", err)
	}
	if err := os.Chmod(partial, 0o600); err != nil {
		os.Remove(partial)
		return "", fmt.Errorf("backing up the database: %w", err)
	}
	if err := os.Rename(partial, target); err != nil {
		os.Remove(partial)
		return "", fmt.Errorf("backing up the database: %w", err)
	}
	return target, nil
}

// Pending lists the migrations this binary would run, by file name, oldest
// first. It is empty when the database is current.
func (u *Unmigrated) Pending(ctx context.Context) ([]string, error) {
	conn, err := u.db.Connx(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
		return nil, err
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")

	applied := map[string]bool{}
	var ledgerExists int
	if err := sqlx.GetContext(ctx, conn, &ledgerExists, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`); err != nil {
		return nil, err
	}
	if ledgerExists != 0 {
		// The ledger names what ran, so a known released reordering, whose
		// missing migrations sit before others that ran, is read rightly.
		rows, err := readLedger(connQueryer{conn: conn})
		if err != nil {
			return nil, fmt.Errorf("reading migration ledger: %w", err)
		}
		for _, row := range rows {
			applied[row.Name] = true
		}
	} else {
		// A database from before the ledger counts by user_version.
		var version int
		if err := sqlx.GetContext(ctx, conn, &version, "PRAGMA user_version"); err != nil {
			return nil, err
		}
		for _, name := range migrations[:version] {
			applied[name] = true
		}
	}

	pending := []string{}
	for _, name := range migrations {
		if !applied[name] {
			pending = append(pending, name)
		}
	}
	return pending, nil
}
