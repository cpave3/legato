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

func TestSeedAndResolveWorkspaceDefaults(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	defaults, err := SeedAndResolveWorkspaceDefaults(context.Background(), s, &config.Config{
		Workspaces: []config.WorkspaceConfig{
			{Name: "Work", Color: "#4A9EEF"},
			{Name: "Personal", Color: "#7BC47F"},
		},
		DefaultWorkspace:       "personal",
		DefaultImportWorkspace: "WORK",
	})
	if err != nil {
		t.Fatal(err)
	}
	if defaults.Task == nil || defaults.Import == nil {
		t.Fatalf("defaults = %+v, want both IDs", defaults)
	}
	if *defaults.Task == *defaults.Import {
		t.Fatalf("defaults = %+v, want distinct IDs", defaults)
	}

	workspaces, err := s.ListWorkspaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]int, len(workspaces))
	for _, workspace := range workspaces {
		byName[workspace.Name] = workspace.ID
	}
	if *defaults.Task != byName["Personal"] || *defaults.Import != byName["Work"] {
		t.Fatalf("defaults = %+v, workspaces = %+v", defaults, workspaces)
	}
}

func TestSeedAndResolveWorkspaceDefaultsImportFallsBackToTaskDefault(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	defaults, err := SeedAndResolveWorkspaceDefaults(context.Background(), s, &config.Config{
		Workspaces:       []config.WorkspaceConfig{{Name: "Work"}},
		DefaultWorkspace: "Work",
	})
	if err != nil {
		t.Fatal(err)
	}
	if defaults.Task == nil || defaults.Import == nil || *defaults.Task != *defaults.Import {
		t.Fatalf("defaults = %+v, want import to fall back to task default", defaults)
	}
}

func TestSeedAndResolveWorkspaceDefaultsLeavesUnsetDefaultsNil(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	defaults, err := SeedAndResolveWorkspaceDefaults(context.Background(), s, &config.Config{
		Workspaces: []config.WorkspaceConfig{{Name: "Work"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if defaults.Task != nil || defaults.Import != nil {
		t.Fatalf("defaults = %+v, want nil IDs", defaults)
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
