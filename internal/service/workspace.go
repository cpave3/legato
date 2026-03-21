package service

import (
	"context"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

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
