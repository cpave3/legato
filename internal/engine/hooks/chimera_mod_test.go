package hooks_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/hooks"
)

func TestInstallChimeraMod(t *testing.T) {
	root := t.TempDir()
	dir, err := hooks.InstallChimeraMod(root)
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(root, "legato") {
		t.Fatalf("mod dir = %q", dir)
	}
	var manifest struct {
		Name, Version, Module string
		APIVersion            int `json:"apiVersion"`
	}
	readJSON(t, filepath.Join(dir, "chimera-mod.json"), &manifest)
	if manifest.Name != "legato" || manifest.Version == "" || manifest.APIVersion != 1 || manifest.Module != "index.mjs" {
		t.Fatalf("manifest = %+v", manifest)
	}
	module := filepath.Join(dir, manifest.Module)
	original, err := os.ReadFile(module)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(module, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := hooks.InstallChimeraMod(root); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(module)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(old) {
		t.Fatal("unchanged module rewritten")
	}
	if _, err := os.Stat(filepath.Join(dir, "tests")); !os.IsNotExist(err) {
		t.Fatalf("mod tests installed: %v", err)
	}
	if err := os.WriteFile(module, []byte("outdated"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := hooks.InstallChimeraMod(root); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(module)
	if err != nil || string(restored) != string(original) {
		t.Fatalf("outdated module not restored: %v", err)
	}
}

func TestInstallChimeraModReportsFilesystemErrors(t *testing.T) {
	root := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(root, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := hooks.InstallChimeraMod(root); err == nil {
		t.Fatal("expected installation error")
	}
}

func TestChimeraModNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	cmd := exec.Command(node, "--test", "chimeramod/legato/tests/index.test.mjs")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("node mod tests: %v\n%s", err, out)
	}
}

func TestChimeraModNative(t *testing.T) {
	chimera, err := exec.LookPath("chimera")
	if err != nil {
		t.Skip("chimera not installed")
	}
	help, err := exec.Command(chimera, "--help").CombinedOutput()
	if err != nil {
		t.Fatalf("chimera help: %v\n%s", err, help)
	}
	if !strings.Contains(string(help), "mod ") && !strings.Contains(string(help), "mod\n") {
		t.Skip("installed Chimera predates mod support")
	}
	cmd := exec.Command(chimera, "mod", "test", "chimeramod/legato")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("chimera mod test: %v\n%s", err, out)
	}
}
