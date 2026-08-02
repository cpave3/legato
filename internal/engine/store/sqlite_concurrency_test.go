package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func TestNewConfiguresEveryPooledSQLiteConnection(t *testing.T) {
	s := newTestStore(t)
	s.db.SetMaxOpenConns(2)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	first, err := s.db.Connx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := s.db.Connx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	for i, connection := range []*sqlx.Conn{first, second} {
		var timeout, foreignKeys int
		if err := connection.GetContext(ctx, &timeout, "PRAGMA busy_timeout"); err != nil {
			t.Fatal(err)
		}
		if err := connection.GetContext(ctx, &foreignKeys, "PRAGMA foreign_keys"); err != nil {
			t.Fatal(err)
		}
		if timeout != sqliteBusyTimeoutMS || foreignKeys != 1 {
			t.Fatalf("connection %d settings: busy_timeout=%d foreign_keys=%d", i, timeout, foreignKeys)
		}
	}
}

func TestNewWaitsForSQLiteLockBeforeEnablingWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal-lock.db")
	initial, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := initial.db.Exec(`PRAGMA journal_mode=DELETE`); err != nil {
		t.Fatal(err)
	}
	if err := initial.Close(); err != nil {
		t.Fatal(err)
	}

	locker, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Close()
	tx, err := locker.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE tasks SET updated_at = updated_at`); err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() {
		s, err := New(path)
		if err == nil {
			var mode string
			err = s.db.Get(&mode, `PRAGMA journal_mode`)
			if err == nil && mode != "wal" {
				err = fmt.Errorf("journal mode = %q", mode)
			}
			s.Close()
		}
		result <- err
	}()
	time.Sleep(50 * time.Millisecond)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("open after lock release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for opener")
	}
}

func TestConcurrentOpenersMigrateOneSQLiteDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent.db")
	const openers = 8
	start := make(chan struct{})
	errs := make(chan error, openers)
	var wg sync.WaitGroup
	for range openers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			s, err := New(path)
			if err == nil {
				err = s.Close()
			}
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent open: %v", err)
		}
	}

	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var version int
	if err := s.db.Get(&version, "PRAGMA user_version"); err != nil {
		t.Fatal(err)
	}
	if version != len(migrations) {
		t.Fatalf("schema version = %d, want %d", version, len(migrations))
	}
}
