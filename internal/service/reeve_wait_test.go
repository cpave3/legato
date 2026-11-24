package service

import (
	"context"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// reeveWaitFixture is a running agent on T-1, idle mid-work, beside a
// running Reeve session.
type reeveWaitFixture struct {
	agents AgentService
	db     *store.Store
	exec   string
	reeve  string
}

func newReeveWaitFixture(t *testing.T) *reeveWaitFixture {
	t.Helper()
	agents, db, _ := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, db, "T-1")
	if err := agents.SpawnAgent(ctx, "T-1", 0, 0, AgentSpawnOptions{AgentKind: "shell", WorkingDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	session, err := db.GetAgentSessionByTaskID(ctx, "T-1")
	if err != nil {
		t.Fatal(err)
	}
	reeve, err := db.CreateReeveTask(ctx, "Reeve")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InsertReeveSession(ctx, store.ReeveSession{TaskID: reeve, State: "running", AgentKind: "claude-code"}); err != nil {
		t.Fatal(err)
	}
	f := &reeveWaitFixture{agents: agents, db: db, exec: session.ExecutionID, reeve: reeve}
	f.activity(t, "")
	return f
}

func (f *reeveWaitFixture) activity(t *testing.T, activity string) {
	t.Helper()
	if err := f.db.UpdateAgentActivity(context.Background(), "T-1", activity); err != nil {
		t.Fatal(err)
	}
}

func (f *reeveWaitFixture) ask(t *testing.T, at time.Time) {
	t.Helper()
	if _, err := MessageReeve(context.Background(), f.db, "T-1", f.exec, "Which shape: one PR or a stack?", at); err != nil {
		t.Fatal(err)
	}
}

func (f *reeveWaitFixture) agent(t *testing.T) AgentSession {
	t.Helper()
	listed, err := f.agents.ListAgents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range listed {
		if a.TaskID == "T-1" {
			return a
		}
	}
	t.Fatal("T-1 is not listed")
	return AgentSession{}
}

func TestAnAgentThatAskedTheReeveAndStoppedWaitsOnTheReeve(t *testing.T) {
	f := newReeveWaitFixture(t)
	asked := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)
	f.ask(t, asked)

	a := f.agent(t)
	if a.Glance() != GlanceWorking || a.WaitingOnReeve() != ReeveWaitOpen || a.NeedsOperator() {
		t.Fatalf("glance %q, waiting %q, needs operator %v", a.Glance(), a.WaitingOnReeve(), a.NeedsOperator())
	}
	if a.ReeveWaitSince == nil || !a.ReeveWaitSince.Equal(asked) {
		t.Fatalf("waiting since %v, want %v", a.ReeveWaitSince, asked)
	}
	// A second question keeps the first's clock.
	f.ask(t, asked.Add(time.Minute))
	if since := f.agent(t).ReeveWaitSince; since == nil || !since.Equal(asked) {
		t.Fatalf("after a second question, since %v", since)
	}
	// At work, it is working for its own reasons, not the wait.
	f.activity(t, store.ActivityWorking)
	if a := f.agent(t); a.Glance() != GlanceWorking || a.WaitingOnReeve() != "" {
		t.Fatalf("at work: glance %q, waiting %q", a.Glance(), a.WaitingOnReeve())
	}
}

func TestAReeveWaitNoOneAnswersNeedsTheOperator(t *testing.T) {
	f := newReeveWaitFixture(t)
	f.ask(t, time.Now().UTC().Add(-ReeveAnswerWait-time.Minute))
	if a := f.agent(t); a.Glance() != GlanceNeedsYou || a.WaitingOnReeve() != ReeveWaitUnanswered {
		t.Fatalf("past the wait: glance %q, waiting %q", a.Glance(), a.WaitingOnReeve())
	}

	g := newReeveWaitFixture(t)
	if err := g.db.SetReeveSessionState(context.Background(), g.reeve, store.ReeveEnded); err != nil {
		t.Fatal(err)
	}
	g.ask(t, time.Now().UTC())
	if a := g.agent(t); a.Glance() != GlanceNeedsYou || a.WaitingOnReeve() != ReeveWaitUnanswered {
		t.Fatalf("with no Reeve: glance %q, waiting %q", a.Glance(), a.WaitingOnReeve())
	}
}

func TestTheOperatorsTurnEndsAReeveWait(t *testing.T) {
	f := newReeveWaitFixture(t)
	ctx := context.Background()
	f.ask(t, time.Now().UTC())
	if err := f.agents.(OperatorTurnRecorder).RecordOperatorMessage(ctx, "T-1", "[legato peer] not the operator"); err != nil {
		t.Fatal(err)
	}
	if f.agent(t).WaitingOnReeve() != ReeveWaitOpen {
		t.Fatal("tagged text ended the wait")
	}
	if err := f.agents.(OperatorTurnRecorder).RecordOperatorMessage(ctx, "T-1", "One PR, please."); err != nil {
		t.Fatal(err)
	}
	if a := f.agent(t); a.WaitingOnReeve() != "" || a.Glance() != GlanceYourTurn {
		t.Fatalf("after the operator's turn: waiting %q, glance %q", a.WaitingOnReeve(), a.Glance())
	}

	f.ask(t, time.Now().UTC())
	if _, err := CloseOperatorItemsForOperatorTurn(ctx, f.db, "T-1", f.exec); err != nil {
		t.Fatal(err)
	}
	if f.agent(t).WaitingOnReeve() != "" {
		t.Fatal("an operator turn from the agent's hooks left the wait")
	}
}

func TestANewExecutionDoesNotInheritAReeveWait(t *testing.T) {
	f := newReeveWaitFixture(t)
	ctx := context.Background()
	f.ask(t, time.Now().UTC())
	if err := f.agents.KillAgent(ctx, "T-1"); err != nil {
		t.Fatal(err)
	}
	if err := f.agents.SpawnAgent(ctx, "T-1", 0, 0, AgentSpawnOptions{AgentKind: "shell", WorkingDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	f.activity(t, "")
	if a := f.agent(t); a.WaitingOnReeve() != "" || a.Glance() != GlanceYourTurn {
		t.Fatalf("a new execution: waiting %q, glance %q", a.WaitingOnReeve(), a.Glance())
	}
}

// The watcher's check tells the lists when a wait passes ReeveAnswerWait,
// since nothing else changes then.
func TestTheWatcherSaysWhenAReeveWaitGoesUnanswered(t *testing.T) {
	f := newWatchFixture(t)
	due := 0
	f.watch = NewReeveWatcher(ReeveWatcherOptions{Store: f.db, Deliver: func(context.Context, string) error { return nil },
		Now: func() time.Time { return f.now }, WaitsDue: func() { due++ }})
	if _, err := MessageReeve(context.Background(), f.db, f.task, "ex-"+f.task, "One PR or two?", f.now); err != nil {
		t.Fatal(err)
	}
	f.after(t, ReeveWatchCheckEvery)
	f.after(t, ReeveAnswerWait-2*ReeveWatchCheckEvery)
	if due != 0 {
		t.Fatalf("told %d times before the wait passed", due)
	}
	f.after(t, 2*ReeveWatchCheckEvery)
	f.after(t, ReeveWatchCheckEvery)
	if due != 1 {
		t.Fatalf("told %d times, want once as the wait passed", due)
	}
}
