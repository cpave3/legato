package service

import (
	"context"
	"encoding/json"
	"os"
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
	if len(f.notes) == 0 || !strings.Contains(f.notes[0], `kept in the database: swarm.worker.agent: "claude-code"`) {
		t.Fatalf("notes = %q", f.notes)
	}
}
