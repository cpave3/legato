package swarm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegatoHomeOverridden(t *testing.T) {
	override := t.TempDir()
	t.Setenv("LEGATO_HOME", override)
	got, err := LegatoHome()
	if err != nil {
		t.Fatal(err)
	}
	if got != override {
		t.Errorf("LegatoHome() = %q, want %q", got, override)
	}
}

func TestLegatoHomeFallback(t *testing.T) {
	// LEGATO_HOME unset; on CI there may be no HOME, so we can't assert
	// strongly. Just validate behavior when HOME *is* set.
	t.Setenv("LEGATO_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	got, err := LegatoHome()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".legato")
	if got != want {
		t.Errorf("LegatoHome() = %q, want %q", got, want)
	}
}

func TestPlansDirCreatesDirectory(t *testing.T) {
	t.Setenv("LEGATO_HOME", t.TempDir())
	dir, err := PlansDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(dir, filepath.Join("plans")) {
		t.Errorf("PlansDir() = %q, expected suffix .../plans", dir)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("plans dir not on disk: %v", err)
	}
	if !info.IsDir() {
		t.Error("plans path is not a directory")
	}
}

func TestAgentDirRejectsEmptyID(t *testing.T) {
	_, err := AgentDir("")
	if err == nil {
		t.Error("expected error for empty taskID")
	}
}

func TestAgentDirCreatesDirectory(t *testing.T) {
	t.Setenv("LEGATO_HOME", t.TempDir())
	dir, err := AgentDir("abc123")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(dir, filepath.Join("agents", "abc123")) {
		t.Errorf("AgentDir() = %q, expected suffix .../agents/abc123", dir)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("agent dir not on disk: %v", err)
	}
	if !info.IsDir() {
		t.Error("agent path is not a directory")
	}
}
