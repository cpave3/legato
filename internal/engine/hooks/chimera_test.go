package hooks_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
)

// chimeraEvents lists the (event-dir, script-name, expected --activity) tuples
// for every hook the adapter installs. Kept here so tests verify the behavior
// the adapter exposes, not the constants it uses internally.
var chimeraEvents = []struct {
	dir      string
	script   string
	activity string // "" means clear
}{
	{"UserPromptSubmit", "legato-prompt-submit.sh", "working"},
	{"PostToolUse", "legato-post-tool-use.sh", "working"},
	{"PermissionRequest", "legato-permission.sh", "waiting"},
	{"Stop", "legato-stop.sh", ""},
	{"SessionEnd", "legato-session-end.sh", ""},
}

func TestChimeraAdapter_Name(t *testing.T) {
	adapter := hooks.NewChimeraAdapter("/usr/bin/legato")
	if adapter.Name() != "chimera" {
		t.Errorf("Name() = %q, want %q", adapter.Name(), "chimera")
	}
}

func TestChimeraAdapter_EnvVars(t *testing.T) {
	adapter := hooks.NewChimeraAdapter("/usr/bin/legato")
	vars := adapter.EnvVars("task123", "/tmp/legato.sock")
	if len(vars) > 0 {
		t.Errorf("EnvVars should be nil/empty, got %v", vars)
	}
}

func TestChimeraAdapter_InstallHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	adapter := hooks.NewChimeraAdapter("/usr/bin/legato")
	if err := adapter.InstallHooks(""); err != nil {
		t.Fatal(err)
	}

	for _, ev := range chimeraEvents {
		path := filepath.Join(home, ".chimera", "hooks", ev.dir, ev.script)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("hook script not found for %s: %v", ev.dir, err)
		}
		content := string(data)

		if !strings.HasPrefix(content, "#!/bin/sh\n") {
			t.Errorf("%s: missing shebang", ev.dir)
		}
		if !strings.Contains(content, "[ -z \"$LEGATO_TASK_ID\" ] && exit 0") {
			t.Errorf("%s: missing LEGATO_TASK_ID guard", ev.dir)
		}
		wantCmd := "/usr/bin/legato agent state \"$LEGATO_TASK_ID\" --activity " + ev.activity
		if !strings.Contains(content, wantCmd) {
			t.Errorf("%s: expected %q in script, got:\n%s", ev.dir, wantCmd, content)
		}

		info, _ := os.Stat(path)
		if info.Mode().Perm()&0o100 == 0 {
			t.Errorf("%s: script should be executable, mode=%v", ev.dir, info.Mode())
		}
	}
}

func TestChimeraAdapter_UninstallHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	adapter := hooks.NewChimeraAdapter("/usr/bin/legato")
	if err := adapter.InstallHooks(""); err != nil {
		t.Fatal(err)
	}
	if err := adapter.UninstallHooks(""); err != nil {
		t.Fatal(err)
	}

	for _, ev := range chimeraEvents {
		path := filepath.Join(home, ".chimera", "hooks", ev.dir, ev.script)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s: script should have been removed", ev.dir)
		}
	}
}

func TestChimeraAdapter_UninstallNonExistent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	adapter := hooks.NewChimeraAdapter("/usr/bin/legato")
	if err := adapter.UninstallHooks(""); err != nil {
		t.Errorf("uninstall of non-existent hooks should not error: %v", err)
	}
}
