package main

import (
	"context"
	"errors"
	"log"
	"path/filepath"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func openStore(cfg *config.Config) (*store.Store, error) {
	// In Legato's sandbox the store is out of reach by design: everything
	// goes through the broker. Refuse before touching any path.
	if sandboxed() {
		return nil, errSandboxedStore
	}
	database, err := config.ResolveDatabase(cfg)
	if err != nil {
		return nil, err
	}
	db, err := store.Open(store.Options{Driver: database.Driver, Path: database.Path})
	if err != nil {
		return nil, err
	}
	// Every process (the TUI, serve, a short-lived CLI command) reads the
	// effective configuration: the file, then the settings kept in the
	// database for what the file does not pin.
	if err := service.ApplySettings(context.Background(), db, cfg); err != nil {
		log.Printf("settings: %v", err)
	}
	return db, nil
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

var errSandboxedStore = errors.New("this command reads Legato's database, which agents in Legato's sandbox cannot reach")
