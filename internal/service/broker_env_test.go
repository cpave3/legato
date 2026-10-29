package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestASessionGetsTheBrokerSocketAndATokenOnlyItsHashIsKeptOf(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legato.db")
	db, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.CreateExecution(ctx, store.Execution{ID: "ex-1", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: "t1", ExecutionTaskID: "t1", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	env, err := NewBrokerSessionEnv(db, "/run/user/1000/legato/broker.sock").SessionEnv(ctx, "t1", "ex-1")
	if err != nil {
		t.Fatal(err)
	}
	token := env["LEGATO_BROKER_TOKEN"]
	if env["LEGATO_BROKER_SOCKET"] != "/run/user/1000/legato/broker.sock" || len(token) < 40 {
		t.Fatalf("env = %v", env)
	}
	sum := sha256.Sum256([]byte(token))
	got, err := db.ExecutionByBrokerToken(ctx, hex.EncodeToString(sum[:]))
	if err != nil || got.ID != "ex-1" {
		t.Fatalf("the hash does not resolve: %+v %v", got, err)
	}
	// Each session gets a token of its own.
	again, _ := NewBrokerSessionEnv(db, "/s").SessionEnv(ctx, "t1", "ex-1")
	if again["LEGATO_BROKER_TOKEN"] == token {
		t.Fatal("a token was reused")
	}
	_ = db.Close()
	for _, f := range []string{dbPath, dbPath + "-wal"} {
		raw, _ := os.ReadFile(f)
		if bytes.Contains(raw, []byte(token)) {
			t.Fatalf("%s holds the token itself", filepath.Base(f))
		}
	}
}

func TestASpawnedSessionGetsTheBrokerBesideTheChannels(t *testing.T) {
	_, db, _ := newApprovalFixture(t)
	createTask(t, db, "task-1")
	tmux := newMockTmux()
	agents := NewAgentService(db, tmux, t.TempDir())
	agents.(interface{ SetSessionEnv(SessionEnvProvider) }).SetSessionEnv(fixedEnv{"SSH_AUTH_SOCK": "/run/x/ssh.sock"})
	agents.(interface{ SetBrokerEnv(SessionEnvProvider) }).SetBrokerEnv(NewBrokerSessionEnv(db, "/run/x/legato/broker.sock"))
	if err := agents.SpawnAgent(context.Background(), "task-1", 80, 24); err != nil {
		t.Fatal(err)
	}
	env := tmux.envVars["legato-task-1"]
	if env["SSH_AUTH_SOCK"] != "/run/x/ssh.sock" || env["LEGATO_BROKER_SOCKET"] != "/run/x/legato/broker.sock" || env["LEGATO_BROKER_TOKEN"] == "" {
		t.Fatalf("env = %v", env)
	}
	got, err := db.ExecutionByBrokerToken(context.Background(), HashBrokerToken(env["LEGATO_BROKER_TOKEN"]))
	if err != nil || got.ID != env["LEGATO_EXECUTION_ID"] {
		t.Fatalf("the token does not name the session's execution: %+v %v", got, err)
	}
}
