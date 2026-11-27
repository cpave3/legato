package config

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// config is standalone: it imports no Legato package (CLAUDE.md,
// docs/adr/0002-config-owns-the-macro-model.md). Engine packages may not
// import config either; internal/engine/store's architecture test holds
// them to the standard library, third-party modules and internal/leaf.
func TestConfigImportsNoLegatoPackage(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		checked++
		parsed, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range parsed.Imports {
			value, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(value, "github.com/cpave3/legato/") {
				t.Errorf("%s imports %s; config imports no Legato package", name, value)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no config source files found")
	}
}
