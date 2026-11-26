package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
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
	if kind != "" && kind != "chimera" {
		return nil, nil
	}
	return r.saved, nil
}
func (r *resumeAgents) SpawnAgent(_ context.Context, _ string, _, _ int, opts ...AgentSpawnOptions) error {
	r.spawned = append(r.spawned, opts[0])
	return nil
}

func TestAChimeraAuthorIsResumedWithItsSelector(t *testing.T) {
	for _, requested := range []string{"", "chimera"} {
		t.Run("agent="+requested, func(t *testing.T) {
			ctx := context.Background()
			db, err := store.New(filepath.Join(t.TempDir(), "w.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			if err := db.CreateHandoffCard(ctx, store.Task{ID: "card-1", Title: "x", Status: "Doing"}, nil, store.Handoff{
				MainInstanceID: "srv-main", HandoffID: "ho-1", MainName: "workstation", MainTaskID: "T1", TaskID: "card-1",
				Purpose: HandoffMakeChange, Kind: HandoffNewBranch, Repo: "acme/widget", AgentKind: requested, AgentTier: "captured-tier", State: store.HandoffInProgress,
			}); err != nil {
				t.Fatal(err)
			}
			agents := &resumeAgents{saved: &TaskResumeSession{AgentKind: "chimera", ID: "chimera-author"}}
			svc := NewHandoffWorkerService(db, HandoffWorkerOptions{Agents: agents})

			if err := svc.ResumeAuthor(ctx, "card-1"); err != nil {
				t.Fatal(err)
			}

			if len(agents.spawned) != 1 {
				t.Fatalf("spawned %+v", agents.spawned)
			}
			got := agents.spawned[0]
			if got.AgentKind != "chimera" || got.Tier != "captured-tier" || got.SessionPolicy != "resume" || got.ResumeSessionID != "chimera-author" || !got.Unattended || got.Repo != "acme/widget" || got.Slot != "" {
				t.Fatalf("resume options = %+v", got)
			}
		})
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
	agents := &resumeAgents{saved: &TaskResumeSession{AgentKind: "chimera", ID: "chimera-author"}}
	svc := NewHandoffWorkerService(db, HandoffWorkerOptions{Agents: agents})

	handled, err := svc.ResumeInterrupted(ctx, "card-1")
	if err != nil || !handled || len(agents.spawned) != 1 || !agents.spawned[0].Unattended {
		t.Fatalf("resume = %v, %v, spawned %+v; want the handoff's unattended author", handled, err, agents.spawned)
	}
	if handled, err := svc.ResumeInterrupted(ctx, "another"); handled || err != nil {
		t.Fatalf("another card: %v, %v; want it not handled", handled, err)
	}
}

type capturedHandoffAgents struct {
	AgentService
	defaults NewWorkDefaults
	calls    int
	spawned  []AgentSpawnOptions
}

func (a *capturedHandoffAgents) NewWorkDefaults(context.Context) (NewWorkDefaults, error) {
	a.calls++
	return a.defaults, nil
}
func (a *capturedHandoffAgents) GetAgentSession(context.Context, string) (*store.AgentSession, error) {
	return nil, store.ErrNotFound
}
func (a *capturedHandoffAgents) SpawnAgent(_ context.Context, _ string, _, _ int, opts ...AgentSpawnOptions) error {
	a.spawned = append(a.spawned, opts[0])
	return nil
}

type captureHandoffRepos struct{}

func (captureHandoffRepos) CloneProblem(string) string { return "" }
func (captureHandoffRepos) PushedBranch(context.Context, string, string) (string, error) {
	return "main", nil
}
func (captureHandoffRepos) Refresh(context.Context, string) (string, error) { return "", nil }

func TestHandoffCapturesNewWorkAgentAndTierAtAcceptance(t *testing.T) {
	for _, explicit := range []string{"", "claude-code"} {
		t.Run("agent="+explicit, func(t *testing.T) {
			ctx := context.Background()
			db, err := store.New(filepath.Join(t.TempDir(), "handoff.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err := db.CreateColumnMapping(ctx, store.ColumnMapping{ColumnName: "Doing"}); err != nil {
				t.Fatal(err)
			}
			claude := hooks.NewClaudeCodeAdapter("legato")
			codex := hooks.NewCodexAdapter("legato")
			agents := &capturedHandoffAgents{AgentService: NewAgentService(db, newMockTmux(), t.TempDir(), AgentServiceOptions{Adapter: claude, Adapters: map[string]AIToolAdapter{"claude-code": claude, "codex": codex}}), defaults: NewWorkDefaults{Agent: "codex", Tier: "profile-tier"}}
			var pending func()
			svc := NewHandoffWorkerService(db, HandoffWorkerOptions{Agents: agents, Repositories: captureHandoffRepos{}, Sandbox: func() bool { return true }, Run: func(f func()) { pending = f }})
			call := HandoffCall{HandoffID: "h-1", MainTaskID: "T1", Title: "Do work", Purpose: HandoffMakeChange, Kind: HandoffNewBranch, Repo: "acme/widget", Branch: "legato/T1", AgentKind: explicit}
			accepted, created, err := svc.Accept(ctx, HandoffMain{InstanceID: "main", Name: "main"}, call)
			if err != nil || !created {
				t.Fatalf("accept = %+v, %v, %v", accepted, created, err)
			}
			wantKind, wantTier := "codex", "profile-tier"
			if explicit != "" {
				wantKind, wantTier = explicit, ""
				if agents.calls != 0 {
					t.Fatal("explicit handoff read profile")
				}
			}
			saved, err := db.GetHandoff(ctx, "main", "h-1")
			if err != nil || saved.AgentKind != wantKind || saved.AgentTier != wantTier {
				t.Fatalf("captured = %+v, %v", saved, err)
			}
			agents.defaults = NewWorkDefaults{Agent: "claude-code", Tier: "switched"}
			// A retry returns the accepted identity without evaluating the new profile.
			calls := agents.calls
			if _, created, err := svc.Accept(ctx, HandoffMain{InstanceID: "main"}, call); err != nil || created || agents.calls != calls {
				t.Fatalf("retry = %v, %v, defaults calls %d", created, err, agents.calls)
			}
			pending()
			if len(agents.spawned) != 1 || agents.spawned[0].AgentKind != wantKind || agents.spawned[0].Tier != wantTier {
				t.Fatalf("launch = %+v", agents.spawned)
			}
		})
	}
}
