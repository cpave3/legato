package main

import (
	"os"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// configuredDirectoryService is what the spawn dialogs of the TUI and the web
// UI ask where an agent can start: the repositories of the configuration as
// presets, the directories the person saved, and the directories that
// complete a typed path. Both runtimes build it here, so neither can drift.
func configuredDirectoryService(cfg *config.Config, db *store.Store) *service.DirectoryService {
	configured := make([]service.DirectoryPreset, 0, len(cfg.Repositories))
	for _, repository := range cfg.Repositories {
		if repository.PrimaryDir != "" {
			configured = append(configured, service.DirectoryPreset{Name: repository.Name, Path: repository.PrimaryDir})
		}
	}
	// Without a home directory a tilde stands for nothing, and paths are
	// written in full.
	home, _ := os.UserHomeDir()
	return service.NewDirectoryService(db, configured, home)
}
