package service

import (
	"context"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestFreshReeveUsesConfiguredAccount(t *testing.T) {
	for _, override := range []string{"", "session-account"} {
		t.Run("override="+override, func(t *testing.T) {
			svc, _, agents, _ := newReeveFixture(t)
			svc.opts.Defaults = func() ReeveDefaults { return ReeveDefaults{Agent: "claude-code", Account: "reeve-account"} }
			if _, err := svc.Start(context.Background(), ReeveStart{Account: override}); err != nil {
				t.Fatal(err)
			}
			want := "reeve-account"
			if override != "" {
				want = override
			}
			if got := agents.spawned[0].Account; got != want {
				t.Fatalf("account = %q, want %q", got, want)
			}
		})
	}
}

func TestReeveResumeLeavesAccountToSavedConversation(t *testing.T) {
	svc, _, agents, _ := newReeveFixture(t)
	ctx := context.Background()
	session, err := svc.Start(ctx, ReeveStart{})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.End(ctx, session.TaskID); err != nil {
		t.Fatal(err)
	}
	svc.opts.Defaults = func() ReeveDefaults { return ReeveDefaults{Agent: "claude-code", Account: "changed-default"} }
	if _, err := svc.Resume(ctx, session.TaskID, ReeveStart{}); err != nil {
		t.Fatal(err)
	}
	got := agents.spawned[1]
	if got.Account != "" || got.SessionPolicy != "resume" {
		t.Fatalf("resume = %+v", got)
	}
}

type accountReeveAgents struct {
	*fakeReeveAgents
	stop func(context.Context, string) error
}

func (a *accountReeveAgents) KillAgent(ctx context.Context, task string) error {
	return a.stop(ctx, task)
}

func TestGivingReeveVoiceKeepsRunningAccountIdentity(t *testing.T) {
	svc, db, agents, _ := newReeveFixture(t)
	ctx := context.Background()
	session, err := svc.Start(ctx, ReeveStart{})
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: session.TaskID, TmuxSession: "reeve", AgentKind: "claude-code", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateExecution(ctx, store.Execution{ID: "ex-reeve-account", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, ExecutionTaskID: session.TaskID, AgentSessionID: &id, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	identity := store.AgentAccountIdentity{Provider: "claude", Account: "original", ConfigDir: "/original", ResolvedDir: "/original", ConfigEnvSet: true}
	if err := db.PutExecutionAccount(ctx, "ex-reeve-account", identity); err != nil {
		t.Fatal(err)
	}
	svc.opts.Agents = &accountReeveAgents{fakeReeveAgents: agents, stop: func(ctx context.Context, _ string) error {
		return db.DeleteAgentSession(ctx, id)
	}}
	svc.opts.Defaults = func() ReeveDefaults {
		return ReeveDefaults{Agent: "claude-code", Account: "changed-default", Voice: true}
	}
	if err := svc.GiveVoice(ctx, session.TaskID); err != nil {
		t.Fatal(err)
	}
	got := agents.spawned[1]
	if got.AccountIdentity == nil || *got.AccountIdentity != identity || got.Account != "" || got.SessionPolicy != "resume" {
		t.Fatalf("resume = %+v", got)
	}
}

func TestReeveToolOverrideDoesNotInheritAnotherToolsAccount(t *testing.T) {
	svc, _, agents, _ := newReeveFixture(t)
	svc.opts.Defaults = func() ReeveDefaults { return ReeveDefaults{Agent: "claude-code", Account: "work"} }
	if _, err := svc.Start(context.Background(), ReeveStart{Agent: "chimera"}); err != nil {
		t.Fatal(err)
	}
	if got := agents.spawned[0]; got.Account != "" || got.AgentKind != "chimera" {
		t.Fatalf("spawn=%+v", got)
	}
}
