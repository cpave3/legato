package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
)

func TestTurningOnTheWebUIAddsOnlyWebEnabled(t *testing.T) {
	firstRun := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := config.WriteFirstConfig(firstRun); err != nil {
		t.Fatal(err)
	}
	withSettings := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(withSettings, []byte("# mine\nicons: nerdfonts\nweb:\n  port: \"4000\" # my port\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "legato", "config.yaml")
	for name, path := range map[string]string{"first-run file": firstRun, "file with settings": withSettings, "no file": missing} {
		t.Run(name, func(t *testing.T) {
			before, _ := os.ReadFile(path)
			if err := enableWebInConfig(path); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, line := range strings.Split(strings.TrimSpace(string(before)), "\n") {
				if line != "" && !strings.Contains(string(after), line) {
					t.Errorf("lost %q:\n%s", line, after)
				}
			}
			cfg, err := config.LoadBytes(after)
			if err != nil {
				t.Fatal(err)
			}
			if !cfg.Web.Enabled || !cfg.Pinned("web.enabled") {
				t.Fatalf("web.enabled = %v, pinned %v:\n%s", cfg.Web.Enabled, cfg.Pinned("web.enabled"), after)
			}
			if name == "file with settings" && (cfg.Icons != "nerdfonts" || cfg.Web.Port != "4000") {
				t.Errorf("other settings changed:\n%s", after)
			}
		})
	}
}

func TestAWebSettingInTheFileIsPinned(t *testing.T) {
	cfg, err := config.LoadBytes([]byte("web:\n  enabled: false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if on, inFile := (webSetup{cfg: cfg}).State(); on || !inFile {
		t.Fatalf("on %v, in file %v", on, inFile)
	}
	cfg, _ = config.LoadBytes(nil)
	if _, inFile := (webSetup{cfg: cfg}).State(); inFile {
		t.Fatal("no file pins web.enabled")
	}
}
