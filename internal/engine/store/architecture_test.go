package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestProductionCodeUsesStoreBoundary(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	for _, directory := range []string{"cmd", "internal/cli", "internal/service", "internal/setup", "internal/tui", "internal/server"} {
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

func TestEnginePackagesDoNotImportSiblingEnginePackages(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", "..", "internal", "engine"))
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range parsed.Imports {
			value, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			if strings.HasPrefix(value, "github.com/cpave3/legato/internal/engine/") {
				t.Errorf("%s imports sibling engine package %s", path, value)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
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
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			identifier, isIdentifier := selector.X.(*ast.Ident)
			if isIdentifier && identifier.Name == "store" && (selector.Sel.Name == "New" || selector.Sel.Name == "Open") && filepath.Base(path) != "database.go" {
				t.Errorf("%s bypasses openStore", path)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
