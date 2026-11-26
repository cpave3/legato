package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestTmuxOptionEntryKeepsItsStringValue(t *testing.T) {
	settings, _ := newSettingsFixture(t, "")
	ctx := context.Background()
	if err := settings.SetEntry(ctx, "agents.tmux_options", "history-limit", json.RawMessage(`"50000"`), "web"); err != nil {
		t.Fatal(err)
	}
	cfg, err := settings.Effective(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Agents.TmuxOptions["history-limit"]; got != "50000" {
		t.Fatalf("history-limit = %q, want 50000", got)
	}
	if err := settings.SetEntry(ctx, "agents.tmux_options", "history-limit", json.RawMessage(`{"value":"1"}`), "web"); err == nil {
		t.Fatal("accepted an object in place of a tmux option string")
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
	// The built-in macros come first; then the file's, then the database's.
	if len(macros.Entries) == len(BuiltinMacros())+2 {
		macros.Entries = macros.Entries[len(BuiltinMacros()):]
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

// A setting carries the value it has when unset, so the web can say what
// Reset goes back to.
func TestASettingSaysItsDefault(t *testing.T) {
	settings, _ := newSettingsFixture(t, "")
	ctx := context.Background()
	settings.Set(ctx, "jira.sync_interval_seconds", json.RawMessage(`120`), "web")
	got, _ := settings.Get(ctx, "jira.sync_interval_seconds")
	if got.Value != 120 || got.Default != 60 {
		t.Fatalf("jira.sync_interval_seconds = %v, default %v; want 120, default 60", got.Value, got.Default)
	}
}

// A setting that waits for a restart says the value Legato still uses.
func TestASettingWaitingForARestartSaysTheValueInUse(t *testing.T) {
	settings, _ := newSettingsFixture(t, "")
	ctx := context.Background()
	settings.MarkStarted(ctx)
	settings.Set(ctx, "swarm.max_concurrent_agents", json.RawMessage(`6`), "web")
	got, _ := settings.Get(ctx, "swarm.max_concurrent_agents")
	if !got.PendingRestart || got.StartedValue != 0 || !got.HasStartedValue() {
		t.Fatalf("got %+v, want pending with the started value 0", got)
	}
	still, _ := settings.Get(ctx, "jira.sync_interval_seconds")
	if still.HasStartedValue() {
		t.Fatalf("a setting that does not wait has a started value: %+v", still)
	}
}

// The file-only settings are shown read-only with their values, but never
// a secret's: those say only whether they are set.
func TestFileOnlySettingsAreShownWithTheirValues(t *testing.T) {
	settings, _ := newSettingsFixture(t, "web:\n  port: \"4080\"\n  public_url: https://legato.example\nsecrets:\n  master_key: file\n")
	got, err := settings.FileOnlyValues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got["web.port"] != "4080" || got["web.public_url"] != "https://legato.example" {
		t.Fatalf("values = %v", got)
	}
	if got["secrets.master_key"] != "file" {
		t.Fatalf("secrets.master_key = %q, want the store's kind", got["secrets.master_key"])
	}
	for key := range got {
		if !config.IsFileOnly(key) {
			t.Errorf("%s is not file-only", key)
		}
	}
}

// A secret config.yaml gave is never shown as the value Legato started
// with, after the file changes.
func TestASecretsStartedValueIsNeverShown(t *testing.T) {
	settings, _ := newSettingsFixture(t, "jira:\n  api_token: https://ATATT-the-old-token\n")
	ctx := context.Background()
	settings.MarkStarted(ctx)
	os.WriteFile(os.Getenv("LEGATO_CONFIG"), []byte("jira:\n  api_token: ATATT-the-new-token\n"), 0o600)
	got, err := settings.Get(ctx, "jira.api_token")
	if err != nil {
		t.Fatal(err)
	}
	if !got.PendingRestart {
		t.Fatalf("got %+v, want it waiting for a restart", got)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "ATATT") {
		t.Fatalf("a secret is shown: %s", raw)
	}
}

// An entry of a restart list added, changed or removed since start waits
// for a restart, by name.
func TestListEntriesChangedSinceStartWaitForARestart(t *testing.T) {
	settings, _ := newSettingsFixture(t, "")
	ctx := context.Background()
	settings.SetEntry(ctx, "workspaces", "infra", json.RawMessage(`{"color":"blue"}`), "web")
	settings.MarkStarted(ctx)
	settings.SetEntry(ctx, "workspaces", "web", json.RawMessage(`{"color":"green"}`), "web")
	settings.DeleteEntry(ctx, "workspaces", "infra")
	lists, err := settings.Lists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lists {
		if l.Key != "workspaces" {
			if len(l.PendingEntries) != 0 {
				t.Errorf("%s waits: %v", l.Key, l.PendingEntries)
			}
			continue
		}
		if got := strings.Join(l.PendingEntries, ","); got != "infra,web" {
			t.Fatalf("pending entries = %q, want infra,web", got)
		}
	}
}

// A secret setting says whether Legato can read the secret it names, and
// never the secret.
func TestASecretSettingSaysWhetherItsSecretCanBeRead(t *testing.T) {
	settings, _ := newSettingsFixture(t, "")
	ctx := context.Background()
	settings.SetSecretResolver(func(_ context.Context, ref string) (string, error) {
		if ref == "legato://jira-token" {
			return "ATATT-the-token", nil
		}
		return "", errors.New("no such secret")
	})
	settings.Set(ctx, "jira.api_token", json.RawMessage(`"legato://jira-token"`), "web")
	settings.Set(ctx, "github.token", json.RawMessage(`"legato://gone"`), "web")
	all, err := settings.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]string{}
	for _, s := range all {
		status[s.Key] = s.SecretStatus
	}
	if status["jira.api_token"] != SecretStored || status["github.token"] != SecretMissing || status["manticore.api_key"] != "" {
		t.Fatalf("statuses = jira %q, github %q, manticore %q", status["jira.api_token"], status["github.token"], status["manticore.api_key"])
	}
	raw, _ := json.Marshal(all)
	if strings.Contains(string(raw), "ATATT") {
		t.Fatal("the secret's value is in the list")
	}
}

// The Reeve's rules are listed in the order they apply: the file's, then
// those kept here by name, as config.applyEntries adds them, whatever
// order they were saved in.
func TestAnOrderedListIsListedInTheOrderItApplies(t *testing.T) {
	settings, db := newSettingsFixture(t, "reeve:\n  rules:\n    - pattern: z.file\n      level: approve\n")
	ctx := context.Background()
	for _, name := range []string{"POST /api/tasks/*", "POST /api/tasks*"} {
		if err := db.SetSettingEntry(ctx, "reeve.rules", name, json.RawMessage(`{"level":"auto"}`), "web"); err != nil {
			t.Fatal(err)
		}
	}
	lists, err := settings.Lists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := settings.Effective(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var applied []string
	for _, r := range cfg.Reeve.Rules {
		applied = append(applied, r.Pattern)
	}
	for _, l := range lists {
		if l.Key != "reeve.rules" {
			continue
		}
		var listed []string
		for _, e := range l.Entries {
			listed = append(listed, e.Name)
		}
		if strings.Join(listed, "|") != strings.Join(applied, "|") {
			t.Fatalf("listed %q, applied %q", listed, applied)
		}
		return
	}
	t.Fatal("reeve.rules not listed")
}

func TestUsagePolicySettingsRejectRemovingReferencedOrActiveProfiles(t *testing.T) {
	settings, db := newSettingsFixture(t, "")
	ctx := context.Background()
	if err := settings.SetEntry(ctx, "usage.profiles", "save", json.RawMessage(`{"agent":"chimera"}`), "test"); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetEntry(ctx, "usage.rules", "limit", json.RawMessage(`{"order":1,"meter":"claude.weekly","at_least":90,"profile":"save"}`), "test"); err != nil {
		t.Fatal(err)
	}
	if err := settings.DeleteEntry(ctx, "usage.profiles", "save"); err == nil {
		t.Fatal("deleted a referenced profile")
	}
	if err := settings.RenameEntry(ctx, "usage.profiles", "save", "renamed", "test"); err == nil {
		t.Fatal("renamed a referenced profile")
	}
	if err := settings.DeleteEntry(ctx, "usage.rules", "limit"); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(UsageProfileState{Profile: config.UsageProfile{Name: "save", Agent: "chimera"}, Initialized: true})
	if err := db.SaveUsageProfile(ctx, string(data), nil); err != nil {
		t.Fatal(err)
	}
	if err := settings.DeleteEntry(ctx, "usage.profiles", "save"); err == nil {
		t.Fatal("deleted the active profile")
	}
}
