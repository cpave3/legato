package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

func TestSeedWorkspaces_InsertsNew(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = SeedWorkspaces(context.Background(), s, []config.WorkspaceConfig{
		{Name: "Work", Color: "#4A9EEF"},
		{Name: "Personal", Color: "#7BC47F"},
	})
	if err != nil {
		t.Fatal(err)
	}

	ws, err := s.ListWorkspaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(ws))
	}
	if ws[0].Name != "Work" {
		t.Errorf("first workspace = %q, want Work", ws[0].Name)
	}
}

func TestSeedWorkspaces_SkipsExisting(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	cfg := []config.WorkspaceConfig{{Name: "Work", Color: "#111"}}
	SeedWorkspaces(context.Background(), s, cfg)

	// Seed again with updated color
	cfg2 := []config.WorkspaceConfig{{Name: "Work", Color: "#222"}}
	SeedWorkspaces(context.Background(), s, cfg2)

	ws, _ := s.ListWorkspaces(context.Background())
	if len(ws) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(ws))
	}
}

func TestSeedWorkspaces_EmptyConfig(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = SeedWorkspaces(context.Background(), s, nil)
	if err != nil {
		t.Fatal(err)
	}

	ws, _ := s.ListWorkspaces(context.Background())
	if len(ws) != 0 {
		t.Errorf("expected 0 workspaces, got %d", len(ws))
	}
}
