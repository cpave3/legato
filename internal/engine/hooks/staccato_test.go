package hooks_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
)

func TestStaccatoAdapter_Name(t *testing.T) {
	adapter := hooks.NewStaccatoAdapter("/usr/bin/legato")
	if adapter.Name() != "staccato" {
		t.Errorf("Name() = %q, want %q", adapter.Name(), "staccato")
	}
}

func TestStaccatoAdapter_EnvVars(t *testing.T) {
	adapter := hooks.NewStaccatoAdapter("/usr/bin/legato")
	vars := adapter.EnvVars("task123", "/tmp/legato.sock")
	if vars != nil && len(vars) > 0 {
		t.Errorf("EnvVars should be nil/empty, got %v", vars)
	}
}

func TestStaccatoAdapter_InstallHooks(t *testing.T) {
	// Override XDG_CONFIG_HOME so we write to a temp dir.
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	adapter := hooks.NewStaccatoAdapter("/usr/bin/legato")
	if err := adapter.InstallHooks(""); err != nil {
		t.Fatal(err)
	}

	// Check hook scripts were created for both events.
	for _, event := range []string{"post-pr-create", "post-pr-view"} {
		scriptPath := filepath.Join(configDir, "staccato", "hooks", event, "legato-pr-link.sh")
		data, err := os.ReadFile(scriptPath)
		if err != nil {
			t.Fatalf("hook script not found for %s: %v", event, err)
		}

		content := string(data)

		if !strings.Contains(content, "#!/bin/sh") {
			t.Errorf("%s: script missing shebang", event)
		}
		if !strings.Contains(content, "LEGATO_TASK_ID") {
			t.Errorf("%s: script should check LEGATO_TASK_ID", event)
		}
		if !strings.Contains(content, "ST_REPO_PATH") {
			t.Errorf("%s: script should use ST_REPO_PATH", event)
		}
		if !strings.Contains(content, "ST_BRANCH") {
			t.Errorf("%s: script should use ST_BRANCH", event)
		}
		if !strings.Contains(content, "/usr/bin/legato task link") {
			t.Errorf("%s: script should call legato task link", event)
		}
		if !strings.Contains(content, "--repo") {
			t.Errorf("%s: script should pass --repo flag", event)
		}

		info, _ := os.Stat(scriptPath)
		if info.Mode()&0o111 == 0 {
			t.Errorf("%s: script should be executable", event)
		}
	}
}

func TestStaccatoAdapter_UninstallHooks(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	adapter := hooks.NewStaccatoAdapter("/usr/bin/legato")

	// Install first.
	if err := adapter.InstallHooks(""); err != nil {
		t.Fatal(err)
	}

	// Uninstall.
	if err := adapter.UninstallHooks(""); err != nil {
		t.Fatal(err)
	}

	for _, event := range []string{"post-pr-create", "post-pr-view"} {
		scriptPath := filepath.Join(configDir, "staccato", "hooks", event, "legato-pr-link.sh")
		if _, err := os.Stat(scriptPath); !os.IsNotExist(err) {
			t.Errorf("hook script should have been removed for %s", event)
		}
	}
}

func TestStaccatoAdapter_UninstallNonExistent(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	adapter := hooks.NewStaccatoAdapter("/usr/bin/legato")

	// Uninstall without install — should not error.
	if err := adapter.UninstallHooks(""); err != nil {
		t.Errorf("uninstall of non-existent hooks should not error: %v", err)
	}
}
