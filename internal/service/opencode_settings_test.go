package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
)

func TestOpenCodeSecretSetupResolvesOnlyForRuntimeNotReadOrExport(t *testing.T) {
	settings, db := newSettingsFixture(t, "")
	ctx := context.Background()
	const secret = "opencode-resolved-SENTINEL"
	resolve := func(_ context.Context, ref string) (string, error) {
		if ref != "legato://opencode-go" {
			t.Fatalf("unexpected reference %q", ref)
		}
		return secret, nil
	}
	settings.SetSecretResolver(resolve)
	for key, value := range map[string]string{"usage.opencode.enabled": "true", "usage.opencode.api_key": `"legato://opencode-go"`} {
		if err := settings.Set(ctx, key, json.RawMessage(value), "web"); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Defaults()
	if err := ApplySettings(ctx, db, cfg, resolve); err != nil {
		t.Fatal(err)
	}
	if !cfg.Usage.OpenCode.IsEnabled() || cfg.Usage.OpenCode.APIKey != secret {
		t.Fatal("setup did not reach runtime")
	}
	got, err := settings.Get(ctx, "usage.opencode.api_key")
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != "legato://opencode-go" {
		t.Fatal("setting did not retain reference")
	}
	listed, err := settings.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(listed)
	if err != nil {
		t.Fatal(err)
	}
	exported, err := settings.Export(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{encoded, exported} {
		if strings.Contains(string(data), secret) {
			t.Fatal("resolved secret disclosed")
		}
	}
	if !strings.Contains(string(encoded), "stored") || !strings.Contains(string(exported), "legato://opencode-go") {
		t.Fatal("configured state/export lost secret reference")
	}
}
