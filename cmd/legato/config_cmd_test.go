package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

// configCmdInstance is an instance whose file pins icons and whose database
// keeps agents.reviews.
func configCmdInstance(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legato.db")
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte("db:\n  path: "+dbPath+"\nicons: nerdfonts\n"), 0o600)
	t.Setenv("LEGATO_CONFIG", cfgPath)
	db, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	db.SetSetting(context.Background(), "agents.reviews", json.RawMessage(`false`), "web")
	db.Close()
}

func TestConfigShowPrintsTheEffectiveSettings(t *testing.T) {
	configCmdInstance(t)
	out := captureStdout(t, func() int { return runCLI([]string{"config", "show"}) })
	for _, want := range []string{"agents.reviews: false\n", "icons: nerdfonts\n", "agents.peer_awareness: true\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "(") {
		t.Errorf("sources shown without --sources:\n%s", out)
	}
}

func TestConfigShowSourcesSaysWhereEachComesFrom(t *testing.T) {
	configCmdInstance(t)
	out := captureStdout(t, func() int { return runCLI([]string{"config", "show", "--sources"}) })
	for _, want := range []string{"agents.reviews: false  (set here)", "icons: nerdfonts  (config.yaml, pinned)", "agents.peer_awareness: true  (default)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestConfigShowJSON(t *testing.T) {
	configCmdInstance(t)
	out := captureStdout(t, func() int { return runCLI([]string{"config", "show", "--json"}) })
	var envelope struct {
		Data struct {
			Settings []struct {
				Key    string `json:"key"`
				Source string `json:"source"`
			} `json:"settings"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || len(envelope.Data.Settings) != len(config.Settings()) {
		t.Fatalf("output = %s, %v", out, err)
	}
}

func TestConfigExportWritesTheDatabasesSettingsAsYAML(t *testing.T) {
	configCmdInstance(t)
	out := captureStdout(t, func() int { return runCLI([]string{"config", "export"}) })
	if !strings.Contains(out, "reviews: false") || strings.Contains(out, "icons") {
		t.Fatalf("export:\n%s", out)
	}
	pinned := captureStdout(t, func() int { return runCLI([]string{"config", "export", "--pinned"}) })
	if !strings.Contains(pinned, "icons: nerdfonts") {
		t.Fatalf("export --pinned:\n%s", pinned)
	}
}

func TestConfigImportProposesOneChangeSet(t *testing.T) {
	configCmdInstance(t)
	file := filepath.Join(t.TempDir(), "settings.yaml")
	os.WriteFile(file, []byte("agents:\n  peer_awareness: false\ndata_dir: /elsewhere\n"), 0o600)
	out := captureStdout(t, func() int { return runCLI([]string{"config", "import", file}) })
	if !strings.Contains(out, "1 changes wait for your approval") || !strings.Contains(out, "Left out, since they live only in config.yaml: data_dir") {
		t.Fatalf("output:\n%s", out)
	}
}

// Plain `config show --sources` is read by people and scripts: the
// registry's labels and layout fields never change it.
func TestConfigShowSourcesOutputIsUnchanged(t *testing.T) {
	configCmdInstance(t)
	out := captureStdout(t, func() int { return runCLI([]string{"config", "show", "--sources"}) })
	out = strings.ReplaceAll(out, os.Getenv("HOME"), "~")
	golden := filepath.Join("testdata", "config_show_sources.golden")
	if os.Getenv("LEGATO_UPDATE_GOLDEN") != "" {
		os.MkdirAll("testdata", 0o755)
		os.WriteFile(golden, []byte(out), 0o644)
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (record it with LEGATO_UPDATE_GOLDEN=1)", err)
	}
	if out != string(want) {
		t.Fatalf("config show --sources changed:\n%s", out)
	}
}
