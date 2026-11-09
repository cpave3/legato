package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAFirstRunWritesAFileThatPinsNothingAndPointsToSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legato", "config.yaml")
	wrote, err := WriteFirstConfig(path)
	if err != nil || !wrote {
		t.Fatalf("WriteFirstConfig = %v, %v", wrote, err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "Settings") {
		t.Fatalf("no pointer to Settings:\n%s", data)
	}
	t.Setenv("LEGATO_CONFIG", path)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if pinned := cfg.PinnedKeys(); len(pinned) != 0 {
		t.Fatalf("the first file pins %v", pinned)
	}
	if again, _ := WriteFirstConfig(path); again {
		t.Fatal("wrote over an existing file")
	}
}
