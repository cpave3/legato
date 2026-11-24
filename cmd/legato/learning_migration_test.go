package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

type learningMigrationFixture struct {
	db          *store.Store
	dir         string
	coordinator *service.WorkflowTriggerCoordinator
	logs        *[]string
}

func newLearningMigrationFixture(t *testing.T) learningMigrationFixture {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	dir := filepath.Join(t.TempDir(), "workflows")
	coordinator := service.NewWorkflowTriggerCoordinator(db, nil, func() workflowengine.DiscoveryResult { return workflowengine.DiscoverPaths(dir) })
	return learningMigrationFixture{db: db, dir: dir, coordinator: coordinator, logs: &[]string{}}
}

func (f learningMigrationFixture) migrate(t *testing.T, cfg *config.Config) {
	t.Helper()
	var mu sync.Mutex
	migrateLearningTriggers(context.Background(), learningMigration{
		Config: cfg, Store: f.db, Triggers: f.coordinator, Dir: f.dir,
		Discover: func() workflowengine.DiscoveryResult { return workflowengine.DiscoverPaths(f.dir) },
		Logf: func(format string, args ...any) {
			mu.Lock()
			defer mu.Unlock()
			*f.logs = append(*f.logs, fmt.Sprintf(format, args...))
		},
	})
}

// installed is each learn workflow in the folder, by name, with its column
// and whether its trigger is on in this Legato.
func (f learningMigrationFixture) installed(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	result := workflowengine.DiscoverPaths(f.dir)
	if len(result.Errors) != 0 {
		t.Fatalf("discovery errors: %+v", result.Errors)
	}
	for _, definition := range result.Definitions {
		on, ok, err := f.coordinator.TriggerSwitch(context.Background(), definition.Name, workflowengine.TriggerTaskEnteredColumn)
		if err != nil {
			t.Fatal(err)
		}
		state := "paused"
		if ok && on {
			state = "on"
		}
		out[definition.Name] = definition.On.TaskEnteredColumn.Column + " " + state
	}
	return out
}

func learningConfig(enabled bool, columns ...string) *config.Config {
	cfg := &config.Config{Board: config.BoardConfig{Columns: config.DefaultColumns()}}
	cfg.Learning.PacketMaxItems, cfg.Learning.PacketMaxBytes = 100, 256*1024
	cfg.Learning.Enabled = enabled
	cfg.Learning.TransitionColumns = columns
	return cfg
}

func TestLearnOnAColumnBecomesTheLearnWorkflowSwitchedOn(t *testing.T) {
	f := newLearningMigrationFixture(t)
	f.migrate(t, learningConfig(true, "Review"))
	if got := f.installed(t); len(got) != 1 || got["learn"] != "Review on" {
		t.Fatalf("installed = %v", got)
	}
	data, err := os.ReadFile(filepath.Join(f.dir, "learn.workflow.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "enabled: false") {
		t.Fatal("the file switches its trigger on; only this Legato's switch may")
	}
}

func TestLearnOnTwoColumnsBecomesAWorkflowForEach(t *testing.T) {
	f := newLearningMigrationFixture(t)
	f.migrate(t, learningConfig(true, "Review", "Done"))
	if got := f.installed(t); len(got) != 2 || got["learn"] != "Review on" || got["learn-done"] != "Done on" {
		t.Fatalf("installed = %v", got)
	}
}

func TestTheCuratorSettingsAreWrittenIntoTheWorkflow(t *testing.T) {
	f := newLearningMigrationFixture(t)
	cfg := learningConfig(true, "Review")
	cfg.Learning.CuratorAdapter, cfg.Learning.CuratorTier, cfg.Learning.CuratorWorkingDir = "codex", "large", "/srv/notes"
	cfg.Learning.PacketMaxItems = 20
	f.migrate(t, cfg)
	definition := workflowengine.DiscoverPaths(f.dir).Definitions[0]
	for _, step := range definition.Steps {
		if step.Type == "agent.run" && (step.With["agent"] != "codex" || step.With["tier"] != "large" || step.With["working_dir"] != "/srv/notes") {
			t.Errorf("%s with = %v", step.ID, step.With)
		}
		if step.Type == "learning.packet.prepare" && step.With["limits"].(map[string]any)["max_items"] != 20 {
			t.Errorf("limits = %v", step.With["limits"])
		}
	}
}

func TestTheDefaultAgentToolRunsTheCuratorWhenNoneWasNamed(t *testing.T) {
	f := newLearningMigrationFixture(t)
	cfg := learningConfig(true, "Review")
	cfg.Agents.DefaultTool = "chimera"
	f.migrate(t, cfg)
	for _, step := range workflowengine.DiscoverPaths(f.dir).Definitions[0].Steps {
		if step.Type == "agent.run" && step.With["agent"] != "chimera" {
			t.Errorf("%s agent = %v", step.ID, step.With["agent"])
		}
	}
}

func TestTheMigrationRunsOnceForThisLegato(t *testing.T) {
	f := newLearningMigrationFixture(t)
	f.migrate(t, learningConfig(true, "Review"))
	if err := f.coordinator.SetTriggerEnabled(context.Background(), "learn", workflowengine.TriggerTaskEnteredColumn, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.dir, "learn.workflow.yaml")); err != nil {
		t.Fatal(err)
	}
	f.migrate(t, learningConfig(true, "Review"))
	if got := f.installed(t); len(got) != 0 {
		t.Fatalf("a second start installed %v", got)
	}
}

func TestTwoProcessesStartingTogetherInstallOneWorkflow(t *testing.T) {
	f := newLearningMigrationFixture(t)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.migrate(t, learningConfig(true, "Review"))
		}()
	}
	wg.Wait()
	if got := f.installed(t); len(got) != 1 || got["learn"] != "Review on" {
		t.Fatalf("installed = %v", got)
	}
}

func TestLearnOffInstallsNothingNowOrLater(t *testing.T) {
	f := newLearningMigrationFixture(t)
	f.migrate(t, learningConfig(false, "Review"))
	if got := f.installed(t); len(got) != 0 {
		t.Fatalf("installed = %v", got)
	}
	// The settings are deprecated: switching Learn on afterwards does nothing.
	f.migrate(t, learningConfig(true, "Review"))
	if got := f.installed(t); len(got) != 0 {
		t.Fatalf("installed after learning.enabled was switched on = %v", got)
	}
	if !strings.Contains(strings.Join(*f.logs, "\n"), "Workflows tab") {
		t.Fatalf("logs = %q, want a word on the deprecated settings", *f.logs)
	}
}

func TestAnOperatorsOwnSwitchIsNeverOverridden(t *testing.T) {
	f := newLearningMigrationFixture(t)
	if err := f.db.SetWorkflowTriggerSwitch(context.Background(), "learn", workflowengine.TriggerTaskEnteredColumn, false); err != nil {
		t.Fatal(err)
	}
	f.migrate(t, learningConfig(true, "Review"))
	if got := f.installed(t); got["learn"] != "Review paused" {
		t.Fatalf("installed = %v", got)
	}
}

func TestAWorkflowOfTheSameNameIsLeftAlone(t *testing.T) {
	f := newLearningMigrationFixture(t)
	own := []byte("schema_version: 1\nname: learn\ntitle: Mine\nsteps:\n  - {id: note, title: Note, type: task.note, with: {task: {id: x}, body: hi}}\n")
	if _, err := workflowengine.CreateFile(f.dir, "learn", own); err != nil {
		t.Fatal(err)
	}
	f.migrate(t, learningConfig(true, "Review"))
	data, _ := os.ReadFile(filepath.Join(f.dir, "learn.workflow.yaml"))
	if string(data) != string(own) {
		t.Fatalf("the operator's learn workflow was changed:\n%s", data)
	}
	if !strings.Contains(strings.Join(*f.logs, "\n"), `"learn"`) {
		t.Fatalf("logs = %q", *f.logs)
	}
}

func TestAColumnTheBoardDoesNotHaveIsSkipped(t *testing.T) {
	f := newLearningMigrationFixture(t)
	f.migrate(t, learningConfig(true, "Shipped", "Review"))
	if got := f.installed(t); len(got) != 1 || got["learn"] != "Review on" {
		t.Fatalf("installed = %v", got)
	}
}

func TestAFailedWriteIsTriedAgainAtTheNextStart(t *testing.T) {
	f := newLearningMigrationFixture(t)
	if err := os.WriteFile(f.dir, []byte("not a folder"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.migrate(t, learningConfig(true, "Review"))
	if err := os.Remove(f.dir); err != nil {
		t.Fatal(err)
	}
	f.migrate(t, learningConfig(true, "Review"))
	if got := f.installed(t); got["learn"] != "Review on" {
		t.Fatalf("installed = %v", got)
	}
}

// The built-in this replaces could be paused by its own name; that pause
// carries over to the workflow that takes its place.
func TestAPausedBuiltInLearnStaysPaused(t *testing.T) {
	f := newLearningMigrationFixture(t)
	ctx := context.Background()
	if err := f.db.SetWorkflowTriggerSwitch(ctx, "legato-learning-review-aff0766a", workflowengine.TriggerTaskEnteredColumn, false); err != nil {
		t.Fatal(err)
	}
	f.migrate(t, learningConfig(true, "Review", "Done"))
	if got := f.installed(t); got["learn"] != "Review paused" || got["learn-done"] != "Done on" {
		t.Fatalf("installed = %v", got)
	}
}

// A start that stopped between claiming and installing leaves its claim; a
// later start takes over a claim older than a few minutes, never a fresh one.
func TestAnAbandonedMigrationIsFinishedByALaterStart(t *testing.T) {
	for name, tc := range map[string]struct {
		age  time.Duration
		want int
	}{
		"abandoned": {age: time.Hour, want: 1},
		"running":   {age: time.Second, want: 0},
	} {
		t.Run(name, func(t *testing.T) {
			f := newLearningMigrationFixture(t)
			if _, err := f.db.ClaimInstanceValue(context.Background(), learningMigratedKey, learningMigrationStarted(time.Now().Add(-tc.age))); err != nil {
				t.Fatal(err)
			}
			f.migrate(t, learningConfig(true, "Review"))
			if got := f.installed(t); len(got) != tc.want {
				t.Fatalf("installed = %v, want %d", got, tc.want)
			}
		})
	}
}
