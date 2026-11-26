package store

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// openCloseFixture opens a store in its own folder, writes once so the
// database has a WAL, and returns the store, the folder and two pool
// connections: c1 given back (idle), c2 still checked out, as a background
// goroutine of a service holds one while a test's cleanup closes the store.
func openCloseFixture(t *testing.T) (*Store, string, func() error, func(string) error) {
	t.Helper()
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO tasks (id, title, status) VALUES ('task-1', 'Task', 'Backlog')`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	c1, err := s.db.Connx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := s.db.Connx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := c1.Close(); err != nil {
		t.Fatal(err)
	}
	query := func(sql string) error {
		var title string
		return c2.GetContext(ctx, &title, sql)
	}
	return s, dir, c2.Close, query
}

func waitUntil(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting until %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func folderNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// A connection still in use when the store closes runs its statement after
// the idle one has checkpointed and deleted the WAL, and SQLite makes the
// -wal and -shm files again. Close waits for it, so its own close removes
// them, and the folder can be removed once Close returns (LEG-654).
func TestCloseWaitsForAConnectionStillInUse(t *testing.T) {
	s, dir, releaseC2, queryC2 := openCloseFixture(t)

	var released atomic.Bool
	type result struct {
		err            error
		releasedByThen bool
	}
	closed := make(chan result, 1)
	go func() {
		err := s.Close()
		closed <- result{err: err, releasedByThen: released.Load()}
	}()

	// Close has begun once the idle connection is gone.
	waitUntil(t, "Close closes the idle connection", func() bool { return s.db.Stats().Idle == 0 })

	if err := queryC2(`SELECT title FROM tasks WHERE id = 'task-1'`); err != nil {
		t.Fatalf("statement on the connection still in use: %v", err)
	}
	released.Store(true)
	if err := releaseC2(); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-closed:
		if got.err != nil {
			t.Fatalf("Close: %v", got.err)
		}
		if !got.releasedByThen {
			t.Fatal("Close returned before the connection in use was given back")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not return after the connection was given back")
	}
	if open := s.db.Stats().OpenConnections; open != 0 {
		t.Fatalf("open connections after Close = %d, want 0", open)
	}
	if names := folderNames(t, dir); len(names) != 1 || names[0] != "test.db" {
		t.Fatalf("folder after Close = %v, want only test.db", names)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("removing the folder after Close: %v", err)
	}
}

// A connection that is never given back must not hang shutdown: Close waits
// for it only as long as closeWait.
func TestCloseGivesUpOnAConnectionNeverReturned(t *testing.T) {
	previous := closeWait
	closeWait = 50 * time.Millisecond
	t.Cleanup(func() { closeWait = previous })

	s, _, releaseC2, _ := openCloseFixture(t)
	t.Cleanup(func() { _ = releaseC2() })

	started := time.Now()
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close waited past its bound for a connection never given back")
	}
	if waited := time.Since(started); waited < closeWait {
		t.Fatalf("Close returned after %v, before its bound of %v", waited, closeWait)
	}
}
