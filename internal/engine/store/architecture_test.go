package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionCodeUsesStoreBoundary(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	for _, directory := range []string{"cmd", "internal/service", "internal/tui", "internal/server"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			source := string(contents)
			if strings.Contains(source, "github.com/jmoiron/sqlx") || strings.Contains(source, ".DB()") {
				t.Errorf("%s bypasses the store boundary", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestProductionCommandsUseSharedStoreOpener(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", "..", "cmd", "legato"))
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(contents), "store.New(") {
			t.Errorf("%s bypasses openStore", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
