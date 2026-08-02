package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewOpensSQLitePathContainingURICharacters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legato ?#% data.db")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC().Format(time.RFC3339)
	if err := s.CreateTask(context.Background(), Task{ID: "uri-path", Title: "preserved", Status: "Backlog", CreatedAt: created, UpdatedAt: created}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("configured database path was not created: %v", err)
	}

	s, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.GetTask(context.Background(), "uri-path"); err != nil {
		t.Fatalf("reopen configured path: %v", err)
	}
}
