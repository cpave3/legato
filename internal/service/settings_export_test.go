package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
)

func TestExportIsTheDatabasesSettingsShapedLikeConfigYAML(t *testing.T) {
	settings, _ := newSettingsFixture(t, "icons: nerdfonts\njira:\n  api_token: ATATT3xFfGF0secret\nmacros:\n  - name: hi\n    keys: hello\n")
	ctx := context.Background()
	settings.Set(ctx, "agents.reviews", json.RawMessage(`false`), "web")
	settings.Set(ctx, "github.token", json.RawMessage(`"legato://gh"`), "web")
	settings.SetEntry(ctx, "workspaces", "Home", json.RawMessage(`{"color": "#00ff00"}`), "web")

	out, err := settings.Export(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	for _, want := range []string{"reviews: false", "token: legato://gh", "name: Home", "color: '#00ff00'"} {
		if !strings.Contains(text, want) {
			t.Errorf("export lacks %q:\n%s", want, text)
		}
	}
	for _, not := range []string{"icons", "hi", "ATATT3"} {
		if strings.Contains(text, not) {
			t.Errorf("export without --pinned has %q:\n%s", not, text)
		}
	}

	pinned, _ := settings.Export(ctx, true)
	if !strings.Contains(string(pinned), "icons: nerdfonts") || !strings.Contains(string(pinned), "name: hi") || strings.Contains(string(pinned), "ATATT3") {
		t.Errorf("export --pinned:\n%s", pinned)
	}

	// The export loads as a config.yaml with the same values.
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, pinned, 0o600)
	t.Setenv("LEGATO_CONFIG", path)
	cfg, err := config.Load()
	if err != nil || cfg.Agents.Reviews || cfg.Icons != "nerdfonts" || len(cfg.Workspaces) != 1 {
		t.Fatalf("loaded export: %+v, %v", cfg.Workspaces, err)
	}
}
