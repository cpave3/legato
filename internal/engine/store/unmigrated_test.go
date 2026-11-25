package store

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

// databaseBehind makes a database whose newest applied migration is the one
// the given number of places before the binary's newest, with one task in
// it, and returns its path.
func databaseBehind(t *testing.T, behind int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legato.db")
	s := newStoreAtMigration(t, path, migrations[len(migrations)-1-behind])
	created := time.Now().UTC().Format(time.RFC3339)
	if err := s.CreateTask(context.Background(), Task{ID: "kept", Title: "before the upgrade", Status: "Backlog", CreatedAt: created, UpdatedAt: created}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func openWithoutMigrating(t *testing.T, path string) *Unmigrated {
	t.Helper()
	u, err := OpenWithoutMigrating(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { u.Close() })
	return u
}

func TestADatabaseAtAnEarlierMigrationOpensWithoutMigratingAndReportsWhatIsPending(t *testing.T) {
	path := databaseBehind(t, 2)
	want := append([]string(nil), migrations[len(migrations)-2:]...)

	u := openWithoutMigrating(t, path)
	pending, err := u.Pending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(pending, want) {
		t.Fatalf("pending = %v, want %v", pending, want)
	}
	if err := u.Close(); err != nil {
		t.Fatal(err)
	}

	again := openWithoutMigrating(t, path)
	pending, err = again.Pending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(pending, want) {
		t.Fatalf("after reopening, pending = %v, want %v: opening migrated the database", pending, want)
	}
}

func TestTheBackupIsACopyBesideTheDatabaseNamedForTheNewestPendingMigration(t *testing.T) {
	path := databaseBehind(t, 2)
	newest := migrations[len(migrations)-1]
	before := append([]string(nil), migrations[len(migrations)-2:]...)

	u := openWithoutMigrating(t, path)
	backup, err := u.Backup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := path + ".before-" + strings.TrimSuffix(newest, ".sql")
	if backup != want {
		t.Fatalf("backup = %q, want %q", backup, want)
	}
	info, err := os.Stat(backup)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("backup mode = %v, want 0600", mode)
	}

	s, err := u.Migrate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	status, err := s.SchemaStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Current != status.Supported {
		t.Fatalf("after Migrate, schema = %+v", status)
	}

	copy := openWithoutMigrating(t, backup)
	pending, err := copy.Pending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(pending, before) {
		t.Fatalf("the backup's pending = %v, want %v: it is not at the earlier schema", pending, before)
	}
	restored, err := copy.Migrate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	task, err := restored.GetTask(context.Background(), "kept")
	if err != nil || task.Title != "before the upgrade" {
		t.Fatalf("the backup's task = %+v, err %v", task, err)
	}
}

func TestNoBackupIsWrittenWhenNothingIsPending(t *testing.T) {
	path := databaseBehind(t, 0)

	u := openWithoutMigrating(t, path)
	backup, err := u.Backup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if backup != "" {
		t.Fatalf("backup = %q, want none", backup)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "before") {
			t.Fatalf("a backup was written: %s", entry.Name())
		}
	}
}

func TestTheBackupHoldsWritesStillInTheWriteAheadLog(t *testing.T) {
	path := databaseBehind(t, 1)
	// Another process holds the database open with a commit not yet
	// checkpointed into the file: a copy of the file alone would miss it.
	writer, err := sqlx.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.Exec(`PRAGMA wal_autocheckpoint = 0`); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec(`INSERT INTO tasks (id, title, status, created_at, updated_at) VALUES ('in-wal', 'only in the log', 'Backlog', '2026-11-24T00:00:00Z', '2026-11-24T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	u := openWithoutMigrating(t, path)
	backup, err := u.Backup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	copy := openWithoutMigrating(t, backup)
	restored, err := copy.Migrate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if _, err := restored.GetTask(context.Background(), "in-wal"); err != nil {
		t.Fatalf("the backup lacks a committed write: %v", err)
	}
}

func TestANewDatabaseIsNotBackedUp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legato.db")

	u := openWithoutMigrating(t, path)
	backup, err := u.Backup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if backup != "" {
		t.Fatalf("backup = %q, want none for a database with nothing in it", backup)
	}
}

func TestABackupLeftByAnEarlierTryIsReplaced(t *testing.T) {
	path := databaseBehind(t, 1)
	stale := BackupPath(path, migrations[len(migrations)-1])
	if err := os.WriteFile(stale, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}

	u := openWithoutMigrating(t, path)
	backup, err := u.Backup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if backup != stale {
		t.Fatalf("backup = %q, want %q", backup, stale)
	}
	copy := openWithoutMigrating(t, backup)
	if _, err := copy.Pending(context.Background()); err != nil {
		t.Fatalf("the earlier try's file was kept: %v", err)
	}
	if _, err := os.Stat(backup + ".partial"); !os.IsNotExist(err) {
		t.Fatalf("an unfinished copy was left: %v", err)
	}
}
