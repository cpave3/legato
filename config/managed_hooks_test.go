package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheManagedHooksFolderDefaultsToManagedHooksInLegatosHome(t *testing.T) {
	cfg, err := LoadBytes([]byte(""))
	if err != nil {
		t.Fatal(err)
	}
	if got := ResolveManagedHooksDir(cfg, "/home/me/.legato"); got != "/home/me/.legato/managed-hooks" {
		t.Fatalf("ResolveManagedHooksDir = %q", got)
	}
}

func TestTheManagedHooksFolderCanBeMovedWithATildePath(t *testing.T) {
	home, _ := os.UserHomeDir()
	cfg, err := LoadBytes([]byte("managed_hooks:\n  dir: ~/gates\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := ResolveManagedHooksDir(cfg, "/home/me/.legato"); got != filepath.Join(home, "gates") {
		t.Fatalf("ResolveManagedHooksDir = %q", got)
	}
	if _, err := LoadBytes([]byte("managed_hooks:\n  dir: relative/gates\n")); err == nil || !strings.Contains(err.Error(), "managed_hooks.dir") {
		t.Fatalf("a relative managed_hooks.dir loaded: %v", err)
	}
}

func TestTheManagedHooksFolderIsARiskySettingTakenAtRestart(t *testing.T) {
	s, ok := LookupSetting("managed_hooks.dir")
	if !ok || s.Kind != KindString || s.Class != ClassRisky || s.Mode != ModeRestart {
		t.Fatalf("managed_hooks.dir = %+v, %v", s, ok)
	}
}
