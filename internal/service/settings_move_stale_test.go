package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
)

// LEG-164: the database held older values of settings config.yaml pinned.
// Moving them out of the file must keep the file's values, the ones
// Legato was running with.
func staleFixture(t *testing.T) (*SettingsMover, string, string, string) {
	t.Helper()
	mountA, mountB := t.TempDir(), t.TempDir()
	file := fmt.Sprintf(`swarm:
  worker:
    agent: claude-code
adapters:
  chimera:
    launch_args: ["--inline"]
agents:
  sandbox:
    mounts:
      - path: %s
        access: read
      - path: %s
        access: read
`, mountA, mountB)
	mover, db, path := newMoveFixture(t, file)
	ctx := context.Background()
	// Older values, saved before the file changed them.
	if err := db.SetSetting(ctx, "swarm.worker.agent", []byte(`"chimera"`), "web"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSettingEntry(ctx, "adapters", "chimera", []byte(`{"launch_args":[]}`), "web"); err != nil {
		t.Fatal(err)
	}
	return mover, path, mountA, mountB
}

func TestMovingPinnedSettingsKeepsTheFilesValuesOverOlderDatabaseOnes(t *testing.T) {
	mover, path, mountA, mountB := staleFixture(t)
	ctx := context.Background()
	movable, err := mover.Movable(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mover.Move(ctx, movable, "web"); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplySettings(ctx, mover.Settings.db, cfg, nil); err != nil {
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
	_ = path
}

// The restart record says what moved, the value kept, and the database's
// older value it replaced.
func TestAMoveRecordsTheValueItKeptAndTheOneItReplaced(t *testing.T) {
	mover, _, _, _ := staleFixture(t)
	ctx := context.Background()
	rec, err := mover.Move(ctx, SettingsMove{Settings: []string{"swarm.worker.agent"}, Entries: []EntryRef{{List: "adapters", Name: "chimera"}}}, "web")
	if err != nil {
		t.Fatal(err)
	}
	var settings []RestartSetting
	if err := json.Unmarshal([]byte(rec.Settings), &settings); err != nil {
		t.Fatal(err)
	}
	if len(settings) != 2 {
		t.Fatalf("record settings = %s", rec.Settings)
	}
	if s := settings[0]; s.Key != "swarm.worker.agent" || string(s.Old) != `"chimera"` || string(s.New) != `"claude-code"` {
		t.Errorf("setting recorded = %+v (old %s, new %s)", s, s.Old, s.New)
	}
	if s := settings[1]; s.List != "adapters" || s.Key != "chimera" || !strings.Contains(string(s.New), "--inline") || string(s.Old) != `{"launch_args":[]}` {
		t.Errorf("entry recorded = %+v (old %s, new %s)", s, s.Old, s.New)
	}
}
