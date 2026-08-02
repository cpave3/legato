package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestNewRejectsSchemaNewerThanBinaryWithoutWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	future := len(migrations) + 1
	if _, err := db.Exec("PRAGMA user_version = " + migrationVersionString(future)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	opened, err := New(path)
	if opened != nil {
		opened.Close()
		t.Fatal("future-schema database was opened")
	}
	if err == nil || !strings.Contains(err.Error(), "newer than this Legato binary") {
		t.Fatalf("error = %v, want future-schema refusal", err)
	}

	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != future {
		t.Fatalf("user_version = %d, want unchanged %d", version, future)
	}
	var applicationTables int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'table'").Scan(&applicationTables); err != nil {
		t.Fatal(err)
	}
	if applicationTables != 0 {
		t.Fatalf("future database was mutated: found %d tables", applicationTables)
	}
}

func TestNewReopensCurrentDatabaseWithoutChangingPersistedTask(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.db")
	first, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC().Format(time.RFC3339)
	if err := first.CreateTask(context.Background(), Task{ID: "reopen", Title: "preserved", Status: "Backlog", CreatedAt: created, UpdatedAt: created}); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	got, err := second.GetTask(context.Background(), "reopen")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "preserved" || got.CreatedAt != created {
		t.Fatalf("reopened task = %#v", got)
	}
}

func migrationVersionString(version int) string {
	return strconv.Itoa(version)
}
