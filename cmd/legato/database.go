package main

import (
	"path/filepath"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

func openStore(cfg *config.Config) (*store.Store, error) {
	database, err := config.ResolveDatabase(cfg)
	if err != nil {
		return nil, err
	}
	return store.Open(store.Options{Driver: database.Driver, Path: database.Path})
}

func attachmentRoot(cfg *config.Config) string {
	return filepath.Join(config.ResolveDataDir(cfg), "attachments")
}

func workflowArtifactRoot(cfg *config.Config) string {
	return filepath.Join(config.ResolveDataDir(cfg), "artifacts")
}

// agentUIImageRoot is where Legato keeps its copies of the image files
// agents show: written by `legato mcp serve`, given out by the web server.
func agentUIImageRoot(cfg *config.Config) string {
	return filepath.Join(config.ResolveDataDir(cfg), "agent-ui-images")
}

// agentUIVideoRoot is the same for the video files agents show.
func agentUIVideoRoot(cfg *config.Config) string {
	return filepath.Join(config.ResolveDataDir(cfg), "agent-ui-videos")
}
