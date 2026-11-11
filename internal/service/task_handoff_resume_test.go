package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// resumeAgents records what a handoff's resume asks of the agent service.
type resumeAgents struct {
	HandoffAgents
	saved   *TaskResumeSession
	spawned []AgentSpawnOptions
}

func (r *resumeAgents) ReconcileSessions(context.Context) error { return nil }
func (r *resumeAgents) GetAgentSession(context.Context, string) (*store.AgentSession, error) {
	return nil, store.ErrNotFound
}
func (r *resumeAgents) GetTaskResumeSession(_ context.Context, _, kind string) (*TaskResumeSession, error) {
	if kind != "chimera" {
		return nil, nil
	}
	return r.saved, nil
}
func (r *resumeAgents) SpawnAgent(_ context.Context, _ string, _, _ int, opts ...AgentSpawnOptions) error {
	r.spawned = append(r.spawned, opts[0])
	return nil
}

func TestAChimeraAuthorIsResumedWithItsSelector(t *testing.T) {
	ctx := context.Background()
	db, err := store.New(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.CreateHandoffCard(ctx, store.Task{ID: "card-1", Title: "x", Status: "Doing"}, nil, store.Handoff{
		MainInstanceID: "srv-main", HandoffID: "ho-1", MainName: "workstation", MainTaskID: "T1", TaskID: "card-1",
		Purpose: HandoffMakeChange, Kind: HandoffNewBranch, Repo: "acme/widget", AgentKind: "chimera", State: store.HandoffInProgress,
	}); err != nil {
		t.Fatal(err)
	}
	agents := &resumeAgents{saved: &TaskResumeSession{ID: "chimera-author"}}
	svc := NewHandoffWorkerService(db, HandoffWorkerOptions{Agents: agents})

	if err := svc.ResumeAuthor(ctx, "card-1"); err != nil {
		t.Fatal(err)
	}

	if len(agents.spawned) != 1 {
		t.Fatalf("spawned %+v", agents.spawned)
	}
	got := agents.spawned[0]
	if got.SessionPolicy != "resume" || got.ResumeSessionID != "chimera-author" || !got.Unattended || got.Repo != "acme/widget" || got.Slot != "" {
		t.Fatalf("resume options = %+v", got)
	}
}

func handoffCardFixture(t *testing.T) (*store.Store, context.Context) {
	t.Helper()
	ctx := context.Background()
	db, err := store.New(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.CreateHandoffCard(ctx, store.Task{ID: "card-1", Title: "x", Status: "Doing"}, nil, store.Handoff{
		MainInstanceID: "srv-main", HandoffID: "ho-1", MainName: "workstation", MainTaskID: "T1", TaskID: "card-1",
		Purpose: HandoffMakeChange, Kind: HandoffNewBranch, Repo: "acme/widget", AgentKind: "chimera", State: store.HandoffInProgress,
	}); err != nil {
		t.Fatal(err)
	}
	return db, ctx
}

// The main sees that a crash on the worker interrupted the agent of a card
// it handed off.
func TestTheMainSeesAnInterruptedAuthor(t *testing.T) {
	db, ctx := handoffCardFixture(t)
	if err := db.InsertAgentSession(ctx, store.AgentSession{TaskID: "card-1", TmuxSession: "legato-card-1", AgentKind: "chimera", Status: store.AgentSessionInterrupted}); err != nil {
		t.Fatal(err)
	}
	svc := NewHandoffWorkerService(db, HandoffWorkerOptions{Agents: &resumeAgents{}})

	pull, err := svc.Pull(ctx, "srv-main")

	if err != nil || len(pull.Handoffs) != 1 || pull.Handoffs[0].Activity != HandoffActivityInterrupted {
		t.Fatalf("pull = %+v, %v; want the author interrupted", pull, err)
	}
}

// Resuming an interrupted author of a handed-off card goes through its
// handoff, which starts it unattended in its clone; another card is left
// to the agent service.
func TestAnInterruptedAuthorResumesThroughItsHandoff(t *testing.T) {
	db, ctx := handoffCardFixture(t)
	agents := &resumeAgents{saved: &TaskResumeSession{ID: "chimera-author"}}
	svc := NewHandoffWorkerService(db, HandoffWorkerOptions{Agents: agents})

	handled, err := svc.ResumeInterrupted(ctx, "card-1")
	if err != nil || !handled || len(agents.spawned) != 1 || !agents.spawned[0].Unattended {
		t.Fatalf("resume = %v, %v, spawned %+v; want the handoff's unattended author", handled, err, agents.spawned)
	}
	if handled, err := svc.ResumeInterrupted(ctx, "another"); handled || err != nil {
		t.Fatalf("another card: %v, %v; want it not handled", handled, err)
	}
}
