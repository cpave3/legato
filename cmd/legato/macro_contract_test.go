package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type macroRunSpy struct {
	running bool
	spawned []service.AgentSpawnOptions
	queued  []string
}

func (m *macroRunSpy) CaptureOutput(context.Context, string) (string, error) {
	if m.running {
		return "", nil
	}
	return "", errors.New("none")
}
func (m *macroRunSpy) QueueMessage(_ context.Context, _, message string) error {
	m.queued = append(m.queued, message)
	return nil
}
func (m *macroRunSpy) SpawnAgent(_ context.Context, _ string, _, _ int, opts ...service.AgentSpawnOptions) error {
	m.spawned = append(m.spawned, opts...)
	return nil
}
func (m *macroRunSpy) NewWorkDefaults(context.Context) (service.NewWorkDefaults, error) {
	return service.NewWorkDefaults{Agent: "claude-code"}, nil
}
func (m *macroRunSpy) GetTaskResumeSession(context.Context, string, string) (*service.TaskResumeSession, error) {
	return nil, nil
}

func macroRunFixture(t *testing.T) *macroRunSpy {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legato.db")
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("db:\n  path: "+dbPath+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", configPath)
	db, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "T1", Title: "Fix login", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.TrackTaskBranches(ctx, "T1", []store.BranchRef{{Repo: "acme/web", Branch: "fix"}}, "test"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetTaskBranchPR(ctx, "T1", "acme/web", "fix", &store.PRMeta{PRNumber: 12, PRURL: "https://github.com/acme/web/pull/12", State: "OPEN"}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	spy := &macroRunSpy{}
	old := newMacroAgents
	newMacroAgents = func(*config.Config, *store.Store) (service.MacroAgents, error) { return spy, nil }
	t.Cleanup(func() { newMacroAgents = old })
	return spy
}

func TestMacroRunStartsTheTasksAgent(t *testing.T) {
	spy := macroRunFixture(t)
	out := captureStdout(t, func() int { return runCLI([]string{"macro", "run", "plan", "T1", "--json"}) })
	var envelope struct {
		OK   bool                     `json:"ok"`
		Data service.MacroStartResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if !envelope.OK || !envelope.Data.Started || len(spy.spawned) != 1 || !strings.HasPrefix(spy.spawned[0].Prompt, "Task T1: Fix login\n\nPlan this task") {
		t.Fatalf("out = %s, spawned = %+v", out, spy.spawned)
	}
	spy.running = true
	out = captureStdout(t, func() int { return runCLI([]string{"macro", "run", "Review", "T1", "--pr", "12"}) })
	if !strings.Contains(out, "queued") || len(spy.queued) != 1 || !strings.Contains(spy.queued[0], "Pull request acme/web#12: https://github.com/acme/web/pull/12") {
		t.Fatalf("out = %q, queued = %v", out, spy.queued)
	}
}

func TestMacroRunRefusesWhatItCannotFind(t *testing.T) {
	macroRunFixture(t)
	for args, code := range map[string]string{
		"macro run nope T1 --json":          "macro_not_found",
		"macro run Plan missing --json":     "task_not_found",
		"macro run Review T1 --pr 9 --json": "pr_not_found",
		"macro run Plan --json":             "missing_argument",
	} {
		var exit int
		out := captureStderr(t, func() { exit = runCLI(strings.Fields(args)) })
		if exit == 0 || !strings.Contains(out, `"code":"`+code+`"`) {
			t.Errorf("%s: exit %d, out %s", args, exit, out)
		}
	}
}

func TestMacroRunIsForwardedFromTheSandbox(t *testing.T) {
	if allowed := forwardedCommands["macro"]; len(allowed) != 1 || allowed[0] != "run" {
		t.Fatalf("forwarded macro = %v", allowed)
	}
}

func captureStderr(t *testing.T, run func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	done := make(chan []byte)
	go func() { data, _ := io.ReadAll(r); done <- data }()
	run()
	w.Close()
	os.Stderr = old
	return string(<-done)
}
