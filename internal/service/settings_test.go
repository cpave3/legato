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
	if err := ApplySettings(ctx, db, cfg, nil); err != nil || cfg.Agents.Reviews {
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

func TestARestartSettingChangedSinceStartWaitsForARestart(t *testing.T) {
	settings, _ := newSettingsFixture(t, "")
	ctx := context.Background()
	settings.MarkStarted(ctx)

	settings.Set(ctx, "agents.reviews", json.RawMessage(`false`), "web")
	settings.Set(ctx, "web.tint", json.RawMessage(`"teal"`), "web") // live: applies at once

	got, _ := settings.Get(ctx, "agents.reviews")
	if !got.PendingRestart {
		t.Fatalf("agents.reviews = %+v, want pending a restart", got)
	}
	if tint, _ := settings.Get(ctx, "web.tint"); tint.PendingRestart {
		t.Fatal("a live setting waits for a restart")
	}
	if pending, _ := settings.PendingRestart(ctx); len(pending) != 1 || pending[0] != "agents.reviews" {
		t.Fatalf("pending = %v", pending)
	}

	// Set back to what this process started with: nothing waits.
	settings.Reset(ctx, "agents.reviews")
	if pending, _ := settings.PendingRestart(ctx); len(pending) != 0 {
		t.Fatalf("pending = %v", pending)
	}
}

func TestASecretPinnedInTheFileDoesNotWaitForARestart(t *testing.T) {
	settings, _ := newSettingsFixture(t, "manticore:\n  url: http://192.168.1.220:3456\n  api_key: mk-secret\njira:\n  api_token: jira-secret\n")
	ctx := context.Background()
	settings.MarkStarted(ctx)

	if pending, err := settings.PendingRestart(ctx); err != nil || len(pending) != 0 {
		t.Fatalf("pending = %v, %v; want nothing waiting", pending, err)
	}
}

func TestChangesWaitingForARestartAreInAttention(t *testing.T) {
	settings, _ := newSettingsFixture(t, "")
	ctx := context.Background()
	settings.MarkStarted(ctx)
	settings.Set(ctx, "agents.reviews", json.RawMessage(`false`), "web")
	settings.Set(ctx, "icons", json.RawMessage(`"nerdfonts"`), "web")
	restarted := false
	adapter := NewSettingsAttentionAdapter(settings, func() { restarted = true })

	items, err := adapter.Snapshot(ctx)
	if err != nil || len(items) != 1 || items[0].Title != "Restart to apply 2 changes" || items[0].SignalKind != AttentionSignalSettingsRestart {
		t.Fatalf("items = %+v, %v", items, err)
	}
	if err := adapter.Execute(ctx, AttentionActionRequest{ActionID: "restart"}); err != nil || !restarted {
		t.Fatalf("restart action: %v, restarted = %v", err, restarted)
	}
}

// A secret setting keeps a secret's name; the process gets the value.
func TestASecretSettingKeepsTheNameAndAppliesTheValue(t *testing.T) {
	settings, db := newSettingsFixture(t, "")
	ctx := context.Background()
	if err := settings.Set(ctx, "jira.api_token", json.RawMessage(`"legato://jira-token"`), "web"); err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(ctx, "jira.api_token", json.RawMessage(`"ATATT3xFfGF0secret"`), "web"); err == nil {
		t.Fatal("a token itself was kept")
	}
	if got, _ := settings.Get(ctx, "jira.api_token"); got.Value != "legato://jira-token" {
		t.Fatalf("listed = %v, want the name", got.Value)
	}
	cfg, _ := config.Load()
	resolve := func(_ context.Context, ref string) (string, error) {
		if ref != "legato://jira-token" {
			return "", errors.New("no such secret")
		}
		return "the-real-token", nil
	}
	if err := ApplySettings(ctx, db, cfg, resolve); err != nil || cfg.Jira.APIToken != "the-real-token" {
		t.Fatalf("token = %q, %v", cfg.Jira.APIToken, err)
	}
}

func TestASecretInTheFileIsNeverListed(t *testing.T) {
	settings, _ := newSettingsFixture(t, "jira:\n  api_token: ATATT3xFfGF0secret\n")
	got, _ := settings.Get(context.Background(), "jira.api_token")
	if got.Value == "ATATT3xFfGF0secret" || !got.Pinned {
		t.Fatalf("listed = %+v", got)
	}
}

func TestAListIsTheFilesEntriesPlusTheDatabases(t *testing.T) {
	settings, db := newSettingsFixture(t, "macros:\n  - name: hi\n    keys: hello\n")
	ctx := context.Background()
	if err := settings.SetEntry(ctx, "macros", "bye", json.RawMessage(`{"keys": "goodbye"}`), "web"); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetEntry(ctx, "macros", "HI", json.RawMessage(`{"keys": "again"}`), "web"); !errors.Is(err, config.ErrEntryPinned) {
		t.Fatalf("an entry of a name the file has: %v", err)
	}
	lists, _ := settings.Lists(ctx)
	var macros SettingList
	for _, l := range lists {
		if l.Key == "macros" {
			macros = l
		}
	}
	if len(macros.Entries) != 2 || macros.Entries[0].Name != "hi" || !macros.Entries[0].Pinned || macros.Entries[1].Name != "bye" || macros.Entries[1].Source != SettingFromDatabase {
		t.Fatalf("macros = %+v", macros.Entries)
	}
	cfg, _ := config.Load()
	if err := ApplySettings(ctx, db, cfg, nil); err != nil || len(cfg.Macros) != 2 || cfg.Macros[1].Keys != "goodbye" {
		t.Fatalf("macros applied = %+v, %v", cfg.Macros, err)
	}
	if err := settings.DeleteEntry(ctx, "macros", "bye"); err != nil {
		t.Fatal(err)
	}
	if err := settings.DeleteEntry(ctx, "macros", "hi"); !errors.Is(err, config.ErrEntryPinned) {
		t.Fatalf("removing the file's entry: %v", err)
	}
}

// A setting may name a list entry kept in the database: the default
// workspace may be a workspace added here.
func TestASettingMayNameAListEntryKeptInTheDatabase(t *testing.T) {
	settings, db := newSettingsFixture(t, "")
	ctx := context.Background()
	if err := settings.SetEntry(ctx, "workspaces", "Personal", json.RawMessage(`{"color":"#ff0000"}`), "web"); err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(ctx, "default_workspace", json.RawMessage(`"Personal"`), "web"); err != nil {
		t.Fatalf("Set default_workspace = %v", err)
	}
	cfg, _ := config.Load()
	if err := ApplySettings(ctx, db, cfg, nil); err != nil || cfg.DefaultWorkspace != "Personal" || len(cfg.Workspaces) != 1 {
		t.Fatalf("applied: default %q, workspaces %v, %v", cfg.DefaultWorkspace, cfg.Workspaces, err)
	}
	if err := settings.Reset(ctx, "agents.reviews"); err != nil {
		t.Fatalf("Reset beside it = %v", err)
	}
}
