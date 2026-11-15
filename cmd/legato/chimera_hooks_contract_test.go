package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
)

func TestChimeraHooksInstallRetiresManagedScriptsUnlessOptedOut(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "mod-default", true: "legacy-opt-out"}[legacy], func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("LEGATO_SANDBOXED", "")
			configPath := filepath.Join(home, "config.yaml")
			configYAML := "data_dir: " + filepath.Join(home, "data") + "\n"
			if legacy {
				configYAML += "adapters:\n  chimera:\n    native_bridge: false\n"
			}
			if err := os.WriteFile(configPath, []byte(configYAML), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("LEGATO_CONFIG", configPath)
			adapter := hooks.NewChimeraAdapter("/opt/legato")
			if err := adapter.InstallHooks(""); err != nil {
				t.Fatal(err)
			}
			managed := filepath.Join(home, ".chimera/hooks/Stop/legato-stop.sh")
			own := filepath.Join(home, ".chimera/hooks/Stop/my-hook.sh")
			if err := os.WriteFile(own, []byte("my hook"), 0700); err != nil {
				t.Fatal(err)
			}
			out := captureStdout(t, func() int { return runHooksContract([]string{"install", "--tool", "chimera", "--json"}) })
			_, err := os.Stat(managed)
			if legacy && err != nil {
				t.Fatalf("legacy hook missing: %v", err)
			}
			if !legacy && (!os.IsNotExist(err) || !strings.Contains(out, "removed")) {
				t.Fatalf("managed hook still installed or removal not reported: err=%v output=%s", err, out)
			}
			if b, err := os.ReadFile(own); err != nil || string(b) != "my hook" {
				t.Fatalf("operator hook changed: %q %v", b, err)
			}
		})
	}
}
