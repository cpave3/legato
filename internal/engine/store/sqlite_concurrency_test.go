package store

import (
	"context"
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
