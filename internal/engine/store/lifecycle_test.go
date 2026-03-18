package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestClosePreventsFurtherOperations(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = s.GetTicket(context.Background(), "REX-1")
	if err == nil {
		t.Error("expected error after Close, got nil")
	}
}

func TestMigrationIdempotency(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	// Open and close — runs migration
	s1, err := New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	s1.Close()

	// Reopen — migration should be a no-op
	s2, err := New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	// Verify tables still work
	var count int
	if err := s2.db.Get(&count, "SELECT COUNT(*) FROM tickets"); err != nil {
		t.Fatal(err)
	}
}
