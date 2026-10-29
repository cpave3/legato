package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// brokerFixture is a host serving the broker over a database with one
// running session and its token, as a spawn leaves them.
func brokerFixture(t *testing.T) (db *store.Store, dbPath, socket, token string) {
	t.Helper()
	dbPath = filepath.Join(t.TempDir(), "legato.db")
	db, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "task1", Title: "T", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertAgentSession(ctx, store.AgentSession{TaskID: "task1", TmuxSession: "legato-task1", Command: "claude", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	session, _ := db.GetAgentSessionByTaskID(ctx, "task1")
	if err := db.CreateExecution(ctx, store.Execution{ID: "ex-1", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: "task1", ExecutionTaskID: "task1", AgentSessionID: &session.ID, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	token = "lgb_contract"
	if err := db.SetExecutionBrokerToken(ctx, "ex-1", service.HashBrokerToken(token)); err != nil {
		t.Fatal(err)
	}
	host := startBrokerHost(ctx, db, shortDir(t), brokerHostOptions{})
	t.Cleanup(host.Stop)
	return db, dbPath, host.SocketPath(), token
}

func activityOf(t *testing.T, db *store.Store) string {
	t.Helper()
	s, err := db.GetAgentSessionByTaskID(context.Background(), "task1")
	if err != nil {
		t.Fatal(err)
	}
	return s.Activity
}

func TestAgentStateGoesThroughTheBrokerWithoutReadingConfigOrStore(t *testing.T) {
	db, _, socket, token := brokerFixture(t)
	// A configuration that does not load: broker mode must not read it.
	bad := filepath.Join(t.TempDir(), "config.yaml")
	_ = os.WriteFile(bad, []byte("db: [not a map\n"), 0o600)
	t.Setenv("LEGATO_CONFIG", bad)
	t.Setenv("LEGATO_BROKER_SOCKET", socket)
	t.Setenv("LEGATO_BROKER_TOKEN", token)
	if code := runCLI([]string{"agent", "state", "task1", "--activity", "working", "--execution-id", "ex-1"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := activityOf(t, db); got != "working" {
		t.Fatalf("activity = %q", got)
	}
	// stopped reduces its payload on this side and sends the state.
	stdin(t, `{"background_tasks":[{"type":"shell"}]}`)
	if code := runCLI([]string{"agent", "stopped", "task1", "--execution-id", "ex-1"}); code != 0 {
		t.Fatalf("stopped exit %d", code)
	}
	if got := activityOf(t, db); got != store.ActivityBackground {
		t.Fatalf("after stop = %q", got)
	}
	// Another task is refused by the broker.
	if code := runCLI([]string{"agent", "state", "task2", "--activity", "working"}); code == 0 {
		t.Fatal("a session acted for another task")
	}
}

func TestASandboxedHookWithNoBrokerExitsQuietly(t *testing.T) {
	db, _, _, token := brokerFixture(t)
	bad := filepath.Join(t.TempDir(), "config.yaml")
	_ = os.WriteFile(bad, []byte("db: [not a map\n"), 0o600)
	t.Setenv("LEGATO_CONFIG", bad)
	t.Setenv("LEGATO_BROKER_SOCKET", filepath.Join(shortDir(t), "none.sock"))
	t.Setenv("LEGATO_BROKER_TOKEN", token)
	t.Setenv("LEGATO_SANDBOXED", "1")
	if code := runCLI([]string{"agent", "state", "task1", "--activity", "working", "--execution-id", "ex-1"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := activityOf(t, db); got != "" {
		t.Fatalf("activity = %q", got)
	}
}

func TestAnUnsandboxedHookWithNoBrokerUsesTheStore(t *testing.T) {
	db, dbPath, _, token := brokerFixture(t)
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	_ = os.WriteFile(cfg, []byte("db:\n  path: "+dbPath+"\n"), 0o600)
	t.Setenv("LEGATO_CONFIG", cfg)
	t.Setenv("LEGATO_BROKER_SOCKET", filepath.Join(shortDir(t), "none.sock"))
	t.Setenv("LEGATO_BROKER_TOKEN", token)
	if code := runCLI([]string{"agent", "state", "task1", "--activity", "waiting", "--execution-id", "ex-1"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := activityOf(t, db); got != "waiting" {
		t.Fatalf("activity = %q", got)
	}
}

func TestSessionCreatedGoesThroughTheBroker(t *testing.T) {
	db, _, socket, token := brokerFixture(t)
	t.Setenv("LEGATO_CONFIG", filepath.Join(t.TempDir(), "none.yaml"))
	t.Setenv("LEGATO_BROKER_SOCKET", socket)
	t.Setenv("LEGATO_BROKER_TOKEN", token)
	stdin(t, `{"event":"SessionCreated","session_id":"chm-9","session_name":"task1","cwd":"/r"}`)
	if code := runCLI([]string{"agent", "session-created", "task1", "chm-9", "--hook-payload"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	task, _ := db.GetTask(context.Background(), "task1")
	if task.ChimeraSessionID == nil || *task.ChimeraSessionID != "chm-9" {
		t.Fatalf("chimera session = %v", task.ChimeraSessionID)
	}
}

// stdin replaces standard input for one command.
func stdin(t *testing.T, text string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.WriteString(text)
	_ = w.Close()
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old })
}
