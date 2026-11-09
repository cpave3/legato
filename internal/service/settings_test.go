package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

func newSettingsFixture(t *testing.T, file string) (*SettingsService, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(file), 0o600)
	t.Setenv("LEGATO_CONFIG", path)
	db, err := store.New(filepath.Join(dir, "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewSettingsService(db, nil), db
}

func TestASettingSetHereAppliesAndSaysSo(t *testing.T) {
	settings, db := newSettingsFixture(t, "")
	ctx := context.Background()
	if err := settings.Set(ctx, "agents.reviews", json.RawMessage(`false`), "web"); err != nil {
		t.Fatal(err)
	}
	got, err := settings.Get(ctx, "agents.reviews")
	if err != nil || got.Value != false || got.Source != SettingFromDatabase || got.UpdatedBy != "web" {
		t.Fatalf("agents.reviews = %+v, %v", got, err)
	}
	// Every process reads the effective configuration through the store.
	cfg, _ := config.Load()
	if err := ApplySettings(ctx, db, cfg); err != nil || cfg.Agents.Reviews {
		t.Fatalf("applied: reviews = %v, %v", cfg.Agents.Reviews, err)
	}
}

func TestAPinnedSettingCannotBeSetAndKeepsTheFilesValue(t *testing.T) {
	settings, _ := newSettingsFixture(t, "agents:\n  reviews: true\n")
	ctx := context.Background()
	if err := settings.Set(ctx, "agents.reviews", json.RawMessage(`false`), "web"); !errors.Is(err, ErrSettingPinned) {
		t.Fatalf("Set = %v, want pinned", err)
	}
	got, _ := settings.Get(ctx, "agents.reviews")
	if got.Value != true || got.Source != SettingFromFile || !got.Pinned {
		t.Fatalf("agents.reviews = %+v", got)
	}
}

func TestAnUnsetSettingIsItsDefault(t *testing.T) {
	settings, _ := newSettingsFixture(t, "")
	got, err := settings.Get(context.Background(), "agents.peer_awareness")
	if err != nil || got.Value != true || got.Source != SettingFromDefault {
		t.Fatalf("agents.peer_awareness = %+v, %v", got, err)
	}
}

func TestAnInvalidChangeIsRefusedAndNotKept(t *testing.T) {
	settings, db := newSettingsFixture(t, "agents:\n  sandbox:\n    enabled: true\n")
	ctx := context.Background()
	err := settings.Set(ctx, "agents.sandbox.backend", json.RawMessage(`"nspawn"`), "web")
	if err == nil {
		t.Fatal("an invalid backend was taken")
	}
	if _, ok, _ := db.GetSetting(ctx, "agents.sandbox.backend"); ok {
		t.Fatal("the refused value was kept")
	}
	if err := settings.Set(ctx, "icons", json.RawMessage(`"emoji"`), "web"); err == nil {
		t.Fatal("an unknown icon set was taken")
	}
}

func TestResetGoesBackToTheDefault(t *testing.T) {
	settings, _ := newSettingsFixture(t, "")
	ctx := context.Background()
	settings.Set(ctx, "icons", json.RawMessage(`"nerdfonts"`), "web")
	if err := settings.Reset(ctx, "icons"); err != nil {
		t.Fatal(err)
	}
	got, _ := settings.Get(ctx, "icons")
	if got.Source != SettingFromDefault {
		t.Fatalf("icons = %+v", got)
	}
}

func TestTheTintAndUpdateModeMoveOutOfInstanceValuesKeepingThem(t *testing.T) {
	// A database from before the registry kept them as instance values; the
	// migration that made the settings table moved them.
	settings, db := newSettingsFixture(t, "")
	ctx := context.Background()
	if _, ok, _ := db.GetSetting(ctx, "web.tint"); ok {
		t.Fatal("fresh database has a tint")
	}
	tints := NewTintService(StringSetting{Settings: settings})
	if err := tints.SetTint(ctx, "teal"); err != nil {
		t.Fatal(err)
	}
	if got, _ := tints.Tint(ctx); got != "teal" {
		t.Fatalf("tint = %q", got)
	}
	if v, _ := settings.Get(ctx, "web.tint"); v.Source != SettingFromDatabase {
		t.Fatalf("web.tint = %+v", v)
	}
	if raw, _ := db.GetInstanceValue(ctx, "web.tint"); raw != "" {
		t.Fatalf("the tint is still an instance value: %q", raw)
	}
}
