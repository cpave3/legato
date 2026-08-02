package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenDefaultsToSQLiteAndPreservesPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "configured.db")
	s, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC().Format(time.RFC3339)
	if err := s.CreateTask(context.Background(), Task{ID: "configured", Title: "opened", Status: "Backlog", CreatedAt: created, UpdatedAt: created}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = Open(Options{Driver: "sqlite", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.GetTask(context.Background(), "configured"); err != nil {
		t.Fatalf("explicit sqlite did not open legacy path: %v", err)
	}
}

func TestOpenRejectsUnknownDriver(t *testing.T) {
	s, err := Open(Options{Driver: "mysql", Path: filepath.Join(t.TempDir(), "db")})
	if s != nil {
		s.Close()
		t.Fatal("unknown driver returned a store")
	}
	if err == nil || !strings.Contains(err.Error(), `database driver "mysql" is not supported`) {
		t.Fatalf("error = %v", err)
	}
}
