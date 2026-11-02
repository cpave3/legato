package config

import (
	"os"
	"path/filepath"
	"testing"
)

// The example VM configuration in the docs must keep loading as it is.
func TestTheExampleVMConfigurationLoads(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "docs", "examples", "cloud-vm", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	writeTestConfig(t, string(data))
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.RepositoryCache.Enabled || len(cfg.Repositories) == 0 || cfg.Repositories[0].PrimaryDir != "" {
		t.Fatalf("repository cache %+v, repositories %+v", cfg.RepositoryCache, cfg.Repositories)
	}
	if len(cfg.Credentials) != 1 || cfg.Credentials[0].Profile != "github" || len(cfg.Credentials[0].Settings.Repositories) == 0 {
		t.Fatalf("credentials %+v", cfg.Credentials)
	}
}
