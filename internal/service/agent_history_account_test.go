package service

import (
	"context"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// A conversation run several times resumes with the account of its newest
// run that recorded one: a later run that recorded none (an older Legato,
// or a run that never launched) does not reset it to the default.
func TestHistoryConversationTakesTheNewestRunsAccount(t *testing.T) {
	ctx := context.Background()
	f := newRestartFixture(t)
	svc := f.svc.(*agentService)
	cwd, older, newer := t.TempDir(), t.TempDir(), t.TempDir()
	createTask(t, f.store, "history")
	base := time.Now().Add(-3 * time.Hour)
	record := func(id string, start time.Time, identity *store.AgentAccountIdentity) {
		end := start.Add(time.Minute)
		pastRun(t, f.store, id, "history", cwd, start, end)
		if identity != nil {
			if err := f.store.PutExecutionAccount(ctx, id, *identity); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := f.store.RecordRecoveredConversation(ctx, store.UnrecordedExecution{ID: id, TaskID: "history", Status: store.ExecutionLost, StartedAt: start, EndedAt: &end},
			store.RecoveredConversation{AgentKind: "claude-code", ProviderSessionID: "conv", StorageDir: older, CWD: cwd, StartedAt: base}); err != nil {
			t.Fatal(err)
		}
	}
	first := store.AgentAccountIdentity{Provider: "claude", Account: "first", ConfigDir: older, ResolvedDir: older, ConfigEnvSet: true}
	second := store.AgentAccountIdentity{Provider: "claude", Account: "second", ConfigDir: newer, ResolvedDir: newer, ConfigEnvSet: true}
	record("ex-1", base, &first)
	record("ex-2", base.Add(time.Hour), &second)
	record("ex-3", base.Add(2*time.Hour), nil)
	storage := older
	got, err := svc.historyConversation(ctx, "history", "", "claude-code", "conv", &storage)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != "conv" || got.CWD != cwd || got.AccountIdentity == nil || *got.AccountIdentity != second {
		t.Fatalf("resume = %+v", got)
	}
	if none, err := svc.historyConversation(ctx, "history", "", "claude-code", "", &storage); none != nil || err != nil {
		t.Fatalf("no session ID = %+v, %v", none, err)
	}
	if missing, err := svc.historyConversation(ctx, "history", "", "claude-code", "unknown", &storage); missing != nil || err != nil {
		t.Fatalf("unknown session = %+v, %v", missing, err)
	}
}
