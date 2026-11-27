package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestExecutionAccountsSurviveReplayAndRepeatOpens(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "accounts.db")
	db, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	createTestTask(t, db, "task-1")
	if err := db.CreateExecution(ctx, Execution{ID: "ex-1", Kind: ExecutionInteractive, Status: ExecutionRunning, TaskID: "task-1", AgentKind: "claude-code", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	want := AgentAccountIdentity{Provider: "claude", Account: "work", ConfigDir: "/home/a/.claude-work", ResolvedDir: "/home/a/.claude-work", ConfigEnvSet: true}
	if err := db.PutExecutionAccount(ctx, "ex-1", want); err != nil {
		t.Fatal(err)
	}
	// The schema is ahead of its ledger: 160_execution_accounts and what
	// follows ran, but the ledger says they did not, as a ledger rebuilt from
	// user_version can. Replaying them must keep the recorded account rather
	// than fail or lose it. Ledger versions are positions, not file prefixes.
	var before int
	if err := db.DB().Get(&before, `SELECT version FROM schema_migrations WHERE name = '160_execution_accounts.sql'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB().Exec(fmt.Sprintf(`DELETE FROM schema_migrations WHERE version >= %d; PRAGMA user_version = %d`, before, before-1)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for open := 0; open < 2; open++ {
		reopened, err := New(path)
		if err != nil {
			t.Fatalf("open %d: %v", open, err)
		}
		got, err := reopened.ExecutionAccount(ctx, "ex-1")
		if err != nil || got == nil || *got != want {
			t.Fatalf("open %d: account = %+v, %v; want %+v", open, got, err, want)
		}
		// The execution keeps exactly one account: replay made no second table
		// or relaxed the key.
		if err := reopened.PutExecutionAccount(ctx, "ex-1", want); err == nil {
			t.Fatalf("open %d: a second account for one execution was accepted", open)
		}
		if err := reopened.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
