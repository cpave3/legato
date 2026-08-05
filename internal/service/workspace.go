package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

// WorkspaceDefaults contains resolved workspace IDs for user task creation and provider imports.
type WorkspaceDefaults struct {
	Task   *int
	Import *int
}

// SeedAndResolveWorkspaceDefaults prepares configured workspaces and resolves placement defaults.
func SeedAndResolveWorkspaceDefaults(ctx context.Context, s *store.Store, cfg *config.Config) (WorkspaceDefaults, error) {
	if err := SeedWorkspaces(ctx, s, cfg.Workspaces); err != nil {
		return WorkspaceDefaults{}, err
	}
	workspaces, err := s.ListWorkspaces(ctx)
	if err != nil {
		return WorkspaceDefaults{}, fmt.Errorf("listing workspaces: %w", err)
	}
	byName := make(map[string]int, len(workspaces))
	for _, workspace := range workspaces {
		byName[strings.ToLower(strings.TrimSpace(workspace.Name))] = workspace.ID
	}
	resolve := func(key, name string) (*int, error) {
		if name == "" {
			return nil, nil
		}
		id, exists := byName[strings.ToLower(strings.TrimSpace(name))]
		if !exists {
			return nil, fmt.Errorf("%s %q does not match a seeded workspace", key, name)
		}
		return &id, nil
	}

	taskDefault, err := resolve("default_workspace", cfg.DefaultWorkspace)
	if err != nil {
		return WorkspaceDefaults{}, err
	}
	importName := cfg.DefaultImportWorkspace
	if importName == "" {
		importName = cfg.DefaultWorkspace
	}
	importDefault, err := resolve("default_import_workspace", importName)
	if err != nil {
		return WorkspaceDefaults{}, err
	}
	return WorkspaceDefaults{Task: taskDefault, Import: importDefault}, nil
}

// SeedWorkspaces ensures all workspaces from config exist in the database.
func SeedWorkspaces(ctx context.Context, s *store.Store, workspaces []config.WorkspaceConfig) error {
	for i, w := range workspaces {
		var color *string
		if w.Color != "" {
			color = &w.Color
		}
		_, err := s.EnsureWorkspace(ctx, store.Workspace{
			Name:      w.Name,
			Color:     color,
			SortOrder: i,
		})
		if err != nil {
			return err
		}
	}
	return nil
}
