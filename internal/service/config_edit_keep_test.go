package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
)

// LEG-164: a config edit that takes pinned settings out of config.yaml (the
// Reeve "moving" them) let older database values take over. The file's
// values are kept in the database instead, and the edit says so.
func TestAConfigEditThatRemovesPinnedSettingsKeepsTheFilesValues(t *testing.T) {
	mover, path, mountA, mountB := staleFixture(t)
	db := mover.Settings.db
	ctx := context.Background()
	edits := []ConfigEdit{
		{Key: "swarm", Remove: true},
		{Key: "adapters", Remove: true},
		{Key: "agents.sandbox.mounts", Remove: true},
	}

	keep, err := ConfigEditKeeps(ctx, db, path, edits)
	if err != nil {
		t.Fatal(err)
	}
	var said []string
	for _, k := range keep.Keep {
		said = append(said, k.String())
	}
	joined := strings.Join(said, "\n")
	for _, want := range []string{`swarm.worker.agent: "claude-code" (the database had "chimera")`, "adapters chimera", "agents.sandbox.mounts " + mountA} {
		if !strings.Contains(joined, want) {
			t.Errorf("the check does not say %q:\n%s", want, joined)
		}
	}

	_, kept, err := WriteConfigEditKeeping(ctx, db, path, edits, "reeve")
	if err != nil {
		t.Fatal(err)
	}
	if len(kept.Keep) != len(keep.Keep) {
		t.Errorf("kept %d, checked %d", len(kept.Keep), len(keep.Keep))
	}
	if file, _ := os.ReadFile(path); strings.Contains(string(file), "claude-code") || strings.Contains(string(file), "--inline") {
		t.Fatalf("config.yaml still has them:\n%s", file)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplySettings(ctx, db, cfg, nil); err != nil {
		t.Fatal(err)
	}
	if cfg.Swarm.Worker.Agent != "claude-code" {
		t.Errorf("swarm.worker.agent = %q, want claude-code", cfg.Swarm.Worker.Agent)
	}
	if got := cfg.Adapters["chimera"].LaunchArgs; !slices.Equal(got, []string{"--inline"}) {
		t.Errorf("adapters.chimera.launch_args = %v, want [--inline]", got)
	}
	var paths []string
	for _, m := range cfg.Agents.Sandbox.Mounts {
		paths = append(paths, m.Path)
	}
	if !slices.Equal(paths, []string{mountA, mountB}) {
		t.Errorf("mounts = %v, want %s and %s", paths, mountA, mountB)
	}
}

// Taking one field out of an entry is not a move: the entry that is left
// empty is not put back from the file.
func TestRemovingAFieldOfAnEntryDoesNotKeepTheEntry(t *testing.T) {
	mover, path, _, _ := staleFixture(t)
	keep, err := ConfigEditKeeps(context.Background(), mover.Settings.db, path, []ConfigEdit{{Key: "adapters.chimera.launch_args", Remove: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(keep.Keep) != 0 {
		t.Fatalf("kept %+v", keep.Keep)
	}
}

// A secret is never written into the database from the file: the edit
// says the file's value is not kept.
func TestASecretRemovedFromTheFileIsNotKept(t *testing.T) {
	mover, _, path := newMoveFixture(t, manticoreFile)
	keep, err := ConfigEditKeeps(context.Background(), mover.Settings.db, path, []ConfigEdit{{Key: "manticore", Remove: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(keep.Lost) != 1 || keep.Lost[0] != "manticore.api_key" {
		t.Fatalf("lost = %v", keep.Lost)
	}
	for _, k := range keep.Keep {
		if k.Key == "manticore.api_key" {
			t.Fatalf("kept the secret: %+v", k)
		}
	}
}

// A Reeve's config edit that takes a pinned setting out of the file shows
// the operator the value it keeps in the database, keeps it, and says so in
// its result.
func TestAReeveConfigEditThatRemovesAPinShowsAndKeepsTheFilesValue(t *testing.T) {
	f := newChangeSetFixture(t)
	ctx := context.Background()
	path := os.Getenv("LEGATO_CONFIG")
	os.WriteFile(path, []byte("swarm:\n  worker:\n    agent: claude-code\n"), 0o600)
	db := f.devices.db
	if err := db.SetSetting(ctx, "swarm.worker.agent", []byte(`"chimera"`), "web"); err != nil {
		t.Fatal(err)
	}
	out, err := f.svc.Propose(ctx, f.reeve, "move it", []ChangeCall{{Kind: CallConfigEdit, Edits: []ConfigEdit{{Key: "swarm", Remove: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	approval, _ := f.devices.approvals.Get(ctx, out.ApprovalID)
	var details ChangeSetDetails
	json.Unmarshal(approval.Details, &details)
	if got := details.Calls[0].Keeps; len(got) != 1 || got[0] != `kept in the database: swarm.worker.agent: "claude-code" (the database had "chimera")` {
		t.Fatalf("keeps = %q", got)
	}
	f.decide(t, out.ApprovalID, ApprovalAllow, "")
	f.svc.RunQueued(ctx)
	if v, _, _ := db.GetSetting(ctx, "swarm.worker.agent"); string(v) != `"claude-code"` {
		t.Fatalf("swarm.worker.agent in the database = %s", v)
	}
	if len(f.notes) == 0 || !strings.Contains(f.notes[0], "kept config.yaml's values in the database: swarm.worker.agent") || strings.Contains(f.notes[0], "chimera") {
		t.Fatalf("notes = %q", f.notes)
	}
}

const rulesFile = `reeve:
  rules:
    - { pattern: "POST /api/*", level: auto }
    - { pattern: "POST /api/agents/*", level: approve }
`

// The Reeve's rules are ordered: the last that matches decides, and the
// database's are added after the file's. Removing one from the file
// removes it; it is never moved behind the rules left in the file, where
// it would win (review of #81).
func TestRemovingAReeveRuleDoesNotMoveItBehindTheOthers(t *testing.T) {
	mover, db, path := newMoveFixture(t, rulesFile)
	ctx := context.Background()
	keep, err := ConfigEditKeeps(ctx, db, path, []ConfigEdit{{Key: "reeve.rules[0]", Remove: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(keep.Keep) != 0 {
		t.Fatalf("kept %+v", keep.Keep)
	}
	if _, _, err := WriteConfigEditKeeping(ctx, db, path, []ConfigEdit{{Key: "reeve.rules[0]", Remove: true}}, "reeve"); err != nil {
		t.Fatal(err)
	}
	cfg, err := mover.Settings.Effective(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Reeve.Rules) != 1 || cfg.Reeve.Rules[0].Level != "approve" {
		t.Fatalf("rules = %+v", cfg.Reeve.Rules)
	}
}

// Nor does the move offer them: moved, they would come after the file's
// and in another order.
func TestTheMoveDoesNotOfferTheReeveRules(t *testing.T) {
	mover, _, _ := newMoveFixture(t, rulesFile+"icons: nerdfonts\n")
	got, err := mover.Movable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range got.Entries {
		if e.List == "reeve.rules" {
			t.Fatalf("offered %+v", e)
		}
	}
}

// A file that cannot be written leaves the database as it was, a row that
// already held the file's value included (review of #81).
func TestAFailedConfigEditWriteLeavesTheDatabaseAsItWas(t *testing.T) {
	mover, path, _, _ := staleFixture(t)
	db := mover.Settings.db
	ctx := context.Background()
	if err := db.SetSetting(ctx, "swarm.worker.agent", []byte(`"claude-code"`), "web"); err != nil {
		t.Fatal(err)
	}
	// A folder it cannot write in makes the write fail.
	dir := filepath.Dir(path)
	os.Chmod(dir, 0o500)
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	if _, _, err := WriteConfigEditKeeping(ctx, db, path, []ConfigEdit{{Key: "swarm", Remove: true}, {Key: "adapters", Remove: true}}, "reeve"); err == nil {
		t.Fatal("the write did not fail")
	}
	if v, had, _ := db.GetSetting(ctx, "swarm.worker.agent"); !had || string(v) != `"claude-code"` {
		t.Fatalf("swarm.worker.agent = %s (had %v), want the row kept", v, had)
	}
	if entries, _ := db.SettingEntries(ctx); string(entries["adapters"]["chimera"]) != `{"launch_args":[]}` {
		t.Fatalf("adapters chimera = %s, want the row as it was", entries["adapters"]["chimera"])
	}
}

// What the Reeve is told after the edit names what was kept, never the
// database's older values, which it could not read before.
func TestTheReeveIsToldWhatWasKeptWithoutTheOlderValues(t *testing.T) {
	keep := ConfigEditKeep{Keep: []KeptSetting{{Key: "swarm.worker.agent", Value: []byte(`"claude-code"`), Replaced: []byte(`"chimera"`)}}}
	got := keptReport("config.yaml.bak", keep)
	if !strings.Contains(got, "swarm.worker.agent") || strings.Contains(got, "chimera") {
		t.Fatalf("report = %q", got)
	}
}
