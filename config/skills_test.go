package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheSkillsFolderDefaultsToSkillsInLegatosHome(t *testing.T) {
	cfg, err := LoadBytes([]byte(""))
	if err != nil {
		t.Fatal(err)
	}
	if got := ResolveSkillsDir(cfg, "/home/me/.legato"); got != "/home/me/.legato/skills" {
		t.Fatalf("ResolveSkillsDir = %q", got)
	}
}

func TestTheSkillsFolderCanBeMovedWithATildePath(t *testing.T) {
	home, _ := os.UserHomeDir()
	cfg, err := LoadBytes([]byte("skills:\n  dir: ~/agent-skills\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := ResolveSkillsDir(cfg, "/home/me/.legato"); got != filepath.Join(home, "agent-skills") {
		t.Fatalf("ResolveSkillsDir = %q", got)
	}
	if _, err := LoadBytes([]byte("skills:\n  dir: relative/skills\n")); err == nil || !strings.Contains(err.Error(), "skills.dir") {
		t.Fatalf("a relative skills.dir loaded: %v", err)
	}
}

func TestTheSkillsFolderIsARiskySettingTakenAtRestart(t *testing.T) {
	s, ok := LookupSetting("skills.dir")
	if !ok || s.Kind != KindString || s.Class != ClassRisky || s.Mode != ModeRestart {
		t.Fatalf("skills.dir = %+v, %v", s, ok)
	}
}
