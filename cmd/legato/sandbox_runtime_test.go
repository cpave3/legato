package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAToolLinkedFromACheckoutIsFoundThroughItsLauncher(t *testing.T) {
	root := t.TempDir()
	checkout := filepath.Join(root, "Projects", "tool")
	_ = os.MkdirAll(filepath.Join(checkout, ".git"), 0o755)
	_ = os.MkdirAll(filepath.Join(checkout, "packages", "cli", "dist"), 0o755)
	_ = os.WriteFile(filepath.Join(checkout, "packages", "cli", "dist", "bin.js"), []byte("//"), 0o644)
	bin := filepath.Join(root, "bin")
	_ = os.MkdirAll(bin, 0o755)
	launcher := filepath.Join(bin, "tool")
	script := "#!/bin/sh\nbasedir=x\nexec node \"$basedir/../../../" + checkout + "/packages/cli/dist/bin.js\" \"$@\"\n"
	_ = os.WriteFile(launcher, []byte(script), 0o755)
	t.Setenv("PATH", bin)

	got := toolInstallPaths([]string{"tool", "missing-tool"})
	if len(got) != 1 || got[0] != checkout {
		t.Fatalf("paths = %v, want [%s]", got, checkout)
	}
}

func TestAToolThatIsALinkIsFoundWhereItLeads(t *testing.T) {
	root := t.TempDir()
	install := filepath.Join(root, "share", "tool", "versions", "1.0")
	_ = os.MkdirAll(install, 0o755)
	_ = os.WriteFile(filepath.Join(install, "tool"), []byte("\x7fELF"), 0o755)
	bin := filepath.Join(root, "bin")
	_ = os.MkdirAll(bin, 0o755)
	_ = os.Symlink(filepath.Join(install, "tool"), filepath.Join(bin, "tool"))
	t.Setenv("PATH", bin)

	got := toolInstallPaths([]string{"tool"})
	if len(got) != 1 || got[0] != install {
		t.Fatalf("paths = %v, want [%s]", got, install)
	}
}
