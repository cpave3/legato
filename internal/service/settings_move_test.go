package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

func newMoveFixture(t *testing.T, file string) (*SettingsMover, *store.Store, string) {
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
	settings := NewSettingsService(db, nil)
	return &SettingsMover{Settings: settings, Rollback: &RestartRollback{Store: db, Settings: settings}}, db, path
}

const pinnedFile = `# Mine.
icons: nerdfonts
agents:
  reviews: false
jira:
  api_token: ${JIRA_TOKEN}
  base_url: ${JIRA_URL}
macros:
  - name: hi
    keys: hello
`

func TestMovableSettingsAreThePinnedOnesThatHoldNoSecretOrVariable(t *testing.T) {
	mover, _, _ := newMoveFixture(t, pinnedFile)
	got, err := mover.Movable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Settings, ",") != "agents.reviews,icons" {
		t.Fatalf("settings = %v", got.Settings)
	}
	if len(got.Entries) != 1 || got.Entries[0] != (EntryRef{List: "macros", Name: "hi"}) {
		t.Fatalf("entries = %v", got.Entries)
	}
}

func TestAMoveCopiesTheValuesRewritesTheFileAndRecordsTheRestart(t *testing.T) {
	mover, db, path := newMoveFixture(t, pinnedFile)
	ctx := context.Background()
	rec, err := mover.Move(ctx, SettingsMove{Settings: []string{"agents.reviews"}, Entries: []EntryRef{{List: "macros", Name: "hi"}}}, "web")
	if err != nil {
		t.Fatal(err)
	}
	file, _ := os.ReadFile(path)
	if strings.Contains(string(file), "reviews") || strings.Contains(string(file), "macros") || !strings.Contains(string(file), "# Mine.") || !strings.Contains(string(file), "icons: nerdfonts") {
		t.Fatalf("config.yaml = %s", file)
	}
	if v, _, _ := db.GetSetting(ctx, "agents.reviews"); string(v) != "false" {
		t.Fatalf("agents.reviews in the database = %s", v)
	}
	cfg, _ := config.Load()
	if err := ApplySettings(ctx, db, cfg, nil); err != nil || cfg.Agents.Reviews || len(cfg.Macros) != 1 || cfg.Macros[0].Keys != "hello" {
		t.Fatalf("effective: reviews %v, macros %+v, %v", cfg.Agents.Reviews, cfg.Macros, err)
	}
	if rec.State != store.RestartPending || rec.ConfigBackup == "" {
		t.Fatalf("record = %+v", rec)
	}
	if old, _ := os.ReadFile(rec.ConfigBackup); string(old) != pinnedFile {
		t.Fatalf("backup = %s", old)
	}

	// The start after it is not healthy: file and database go back.
	if _, err := mover.Rollback.Settle(ctx, path, func(context.Context) error { return os.ErrDeadlineExceeded }, 0, 0); err != nil {
		t.Fatal(err)
	}
	if file, _ := os.ReadFile(path); string(file) != pinnedFile {
		t.Fatalf("config.yaml after rollback = %s", file)
	}
	if _, had, _ := db.GetSetting(ctx, "agents.reviews"); had {
		t.Fatal("the moved value is still in the database")
	}
	if entries, _ := db.SettingEntries(ctx); len(entries["macros"]) != 0 {
		t.Fatalf("the moved entry is still in the database: %v", entries)
	}
}

func TestASettingThatReadsAVariableDoesNotMove(t *testing.T) {
	mover, _, _ := newMoveFixture(t, pinnedFile)
	if _, err := mover.Move(context.Background(), SettingsMove{Settings: []string{"jira.base_url"}}, "web"); err == nil {
		t.Fatal("moved a setting that reads ${JIRA_URL}")
	}
}
func TestTheOfferToMoveIsOneAttentionItem(t *testing.T) {
	mover, _, _ := newMoveFixture(t, pinnedFile)
	items, err := NewSettingsMoveAttentionAdapter(mover).Snapshot(context.Background())
	if err != nil || len(items) != 1 || items[0].Title != "Move 3 settings from config.yaml into the database" || items[0].Navigation.Target != "move" {
		t.Fatalf("items = %+v, %v", items, err)
	}
	empty, _, _ := newMoveFixture(t, "")
	if items, _ := NewSettingsMoveAttentionAdapter(empty).Snapshot(context.Background()); len(items) != 0 {
		t.Fatalf("an offer with nothing to move: %+v", items)
	}
}
