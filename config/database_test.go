package config

import (
	"path/filepath"
	"testing"
)

func TestResolveDatabaseDefaultsDriverAndPreservesLegacyPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	resolved, err := ResolveDatabase(&Config{DB: DBConfig{Path: path}})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Driver != "sqlite" || resolved.Path != path {
		t.Fatalf("resolved database = %#v", resolved)
	}
}

func TestResolveDatabaseRejectsUnknownDriver(t *testing.T) {
	_, err := ResolveDatabase(&Config{DB: DBConfig{Driver: "mysql"}})
	if err == nil {
		t.Fatal("unknown driver accepted")
	}
}

func TestResolveDataDirDefaultsToDatabaseParentAndAllowsOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "legato.db")
	cfg := &Config{DB: DBConfig{Path: path}}
	if got := ResolveDataDir(cfg); got != filepath.Dir(path) {
		t.Fatalf("legacy data dir = %q, want %q", got, filepath.Dir(path))
	}

	override := filepath.Join(t.TempDir(), "data")
	cfg.DataDir = override
	if got := ResolveDataDir(cfg); got != override {
		t.Fatalf("explicit data dir = %q, want %q", got, override)
	}
}
