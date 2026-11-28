package claudecode

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeVersion(t *testing.T, f Folder, version string) {
	t.Helper()
	if err := os.MkdirAll(f.VersionsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.VersionsDir(), version), []byte("#!/bin/sh\necho "+version+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestAnEmptyFolderHasNoCurrentVersion(t *testing.T) {
	f := Folder{Dir: t.TempDir()}
	if _, err := f.Current(); !errors.Is(err, ErrNoVersion) {
		t.Fatalf("err = %v, want ErrNoVersion", err)
	}
	if v, _ := f.LastGood(); v != "" {
		t.Fatalf("LastGood = %q", v)
	}
	if v, _ := f.Held(); v != "" {
		t.Fatalf("Held = %q", v)
	}
}

func TestSetCurrentPointsALinkNamedClaudeAtTheVersion(t *testing.T) {
	f := Folder{Dir: t.TempDir()}
	writeVersion(t, f, "2.1.326")
	writeVersion(t, f, "2.1.330")
	for _, v := range []string{"2.1.326", "2.1.330"} {
		if err := f.SetCurrent(v); err != nil {
			t.Fatal(err)
		}
		if got, err := f.Current(); err != nil || got != v {
			t.Fatalf("Current = %q, %v; want %s", got, err, v)
		}
	}
	if filepath.Base(f.Command()) != "claude" {
		t.Fatalf("Command = %s: the process name must be claude", f.Command())
	}
	// Running the link runs the version it points at.
	out, err := exec.Command(f.Command()).Output()
	if err != nil || strings.TrimSpace(string(out)) != "2.1.330" {
		t.Fatalf("running the link: %q, %v", out, err)
	}
}

func TestSetCurrentRefusesAVersionNotInTheFolder(t *testing.T) {
	f := Folder{Dir: t.TempDir()}
	if err := f.SetCurrent("2.1.326"); err == nil {
		t.Fatal("want an error for a version not on disk")
	}
	if err := f.SetCurrent("../../bin/sh"); err == nil {
		t.Fatal("want an error for something that is not a version")
	}
}

func TestHasSaysWhetherAVersionIsOnDisk(t *testing.T) {
	f := Folder{Dir: t.TempDir()}
	writeVersion(t, f, "2.1.326")
	if !f.Has("2.1.326") || f.Has("2.1.330") || f.Has("../2.1.326") {
		t.Fatal("Has answered wrongly")
	}
}

func TestCurrentStillNamesAVersionWhoseFileWentMissing(t *testing.T) {
	f := Folder{Dir: t.TempDir()}
	writeVersion(t, f, "2.1.326")
	if err := f.SetCurrent("2.1.326"); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(f.VersionsDir(), "2.1.326"))
	if got, err := f.Current(); err != nil || got != "2.1.326" {
		t.Fatalf("Current = %q, %v", got, err)
	}
	if f.Has("2.1.326") {
		t.Fatal("Has says a missing version is there")
	}
}

func TestLastGoodAndHeldAreKept(t *testing.T) {
	f := Folder{Dir: t.TempDir()}
	if err := f.SetLastGood("2.1.326"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetHeld("2.1.332"); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.LastGood(); v != "2.1.326" {
		t.Fatalf("LastGood = %q", v)
	}
	if v, _ := f.Held(); v != "2.1.332" {
		t.Fatalf("Held = %q", v)
	}
	if err := f.SetHeld(""); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.Held(); v != "" {
		t.Fatalf("Held after clearing = %q", v)
	}
	if err := f.SetLastGood("nope"); err == nil {
		t.Fatal("want an error for something that is not a version")
	}
}

func TestAdoptCopiesAnInstalledVersionIntoTheFolder(t *testing.T) {
	src := filepath.Join(t.TempDir(), "2.1.332")
	if err := os.WriteFile(src, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := Folder{Dir: t.TempDir()}
	if err := f.Adopt(src, "2.1.332"); err != nil {
		t.Fatal(err)
	}
	if !f.Has("2.1.332") {
		t.Fatal("not adopted")
	}
	// The operator's file is left where it was.
	if _, err := os.Stat(src); err != nil {
		t.Fatal(err)
	}
	// Removing the operator's copy leaves Legato's.
	os.Remove(src)
	if got, _ := os.ReadFile(filepath.Join(f.VersionsDir(), "2.1.332")); string(got) != "binary" {
		t.Fatalf("adopted = %q", got)
	}
}

func TestInstalledVersionFindsANativeInstall(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "share", "claude", "versions", "2.1.332")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(bin, []byte("x"), 0o755)
	link := filepath.Join(home, "claude")
	if err := os.Symlink(bin, link); err != nil {
		t.Fatal(err)
	}
	path, version, ok := InstalledVersion(link)
	if !ok || version != "2.1.332" || filepath.Base(path) != "2.1.332" {
		t.Fatalf("InstalledVersion = %q %q %v", path, version, ok)
	}
	// An npm install, or a script, is not a native build.
	other := filepath.Join(home, "cli.js")
	os.WriteFile(other, []byte("x"), 0o755)
	if _, _, ok := InstalledVersion(other); ok {
		t.Fatal("a file outside a versions folder is not a native install")
	}
}

func TestLockIsExclusive(t *testing.T) {
	f := Folder{Dir: t.TempDir()}
	unlock, err := f.Lock()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := f.TryLock(); err != nil || ok {
		t.Fatalf("TryLock while held = %v, %v", ok, err)
	}
	unlock()
	release, ok, err := f.TryLock()
	if err != nil || !ok {
		t.Fatalf("TryLock after unlock = %v, %v", ok, err)
	}
	release()
}
