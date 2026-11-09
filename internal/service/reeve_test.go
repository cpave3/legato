package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

type fakeReeveAgents struct {
	spawned []AgentSpawnOptions
	tasks   []string
	killed  []string
}

func (f *fakeReeveAgents) SpawnAgent(_ context.Context, taskID string, _, _ int, opts ...AgentSpawnOptions) error {
	f.tasks = append(f.tasks, taskID)
	f.spawned = append(f.spawned, opts[0])
	return nil
}

func (f *fakeReeveAgents) KillAgent(_ context.Context, taskID string) error {
	f.killed = append(f.killed, taskID)
	return nil
}

func newReeveFixture(t *testing.T) (*ReeveService, *store.Store, *fakeReeveAgents, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.New(filepath.Join(dir, "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	agents := &fakeReeveAgents{}
	folder := filepath.Join(dir, "state", "legato", "reeve")
	svc := NewReeveService(ReeveServiceOptions{Store: db, Agents: agents, Folder: folder,
		Defaults: func() ReeveDefaults {
			return ReeveDefaults{Agent: "claude-code", Model: "opus", LaunchArgs: []string{"--verbose"}}
		}})
	return svc, db, agents, folder
}

func TestTheReeveFolderIsInTheStateFolder(t *testing.T) {
	env := map[string]string{"XDG_STATE_HOME": "/s", "HOME": "/h"}
	if got := ReeveFolder(func(k string) string { return env[k] }); got != "/s/legato/reeve" {
		t.Fatalf("with XDG_STATE_HOME: %s", got)
	}
	delete(env, "XDG_STATE_HOME")
	if got := ReeveFolder(func(k string) string { return env[k] }); got != "/h/.local/state/legato/reeve" {
		t.Fatalf("without: %s", got)
	}
}

func TestAReeveSessionIsAHiddenTaskInTheReevesFolder(t *testing.T) {
	svc, db, agents, folder := newReeveFixture(t)
	ctx := context.Background()
	session, err := svc.Start(ctx, ReeveStart{Model: "sonnet", Brief: "why did the sync fail?"})
	if err != nil {
		t.Fatal(err)
	}
	task, _ := db.GetTask(ctx, session.TaskID)
	if !task.Ephemeral || task.Kind != store.TaskKindReeve {
		t.Fatalf("task = %+v", task)
	}
	opt := agents.spawned[0]
	if opt.WorkingDir != filepath.Join(folder, "work") || opt.AgentKind != "claude-code" || opt.Model != "sonnet" || opt.Unsandboxed || len(opt.LaunchArgs) != 1 {
		t.Fatalf("spawn = %+v", opt)
	}
	if len(opt.Resources) != 1 || opt.Resources[0].Paths[0].Path != folder || opt.Resources[0].Paths[0].Access != "write" {
		t.Fatalf("the folder is not given read-write: %+v", opt.Resources)
	}
	if listed, _ := db.ListTasksByStatus(ctx, task.Status); len(listed) != 0 {
		t.Fatalf("on the board: %+v", listed)
	}
}

func TestOneReeveSessionRunsAtATime(t *testing.T) {
	svc, _, _, _ := newReeveFixture(t)
	ctx := context.Background()
	if _, err := svc.Start(ctx, ReeveStart{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(ctx, ReeveStart{}); !errors.Is(err, ErrReeveRunning) {
		t.Fatalf("a second session: %v", err)
	}
}

func TestEndingRevokesTheTokenAndResumingMakesAnother(t *testing.T) {
	svc, db, agents, _ := newReeveFixture(t)
	ctx := context.Background()
	session, _ := svc.Start(ctx, ReeveStart{})
	first, _ := db.GetReeveSession(ctx, session.TaskID)
	if _, err := svc.Authenticate(ctx, first.APIToken); err != nil {
		t.Fatalf("a running session's token: %v", err)
	}
	if err := svc.End(ctx, session.TaskID); err != nil {
		t.Fatal(err)
	}
	if len(agents.killed) != 1 {
		t.Fatal("the agent was not stopped")
	}
	if _, err := svc.Authenticate(ctx, first.APIToken); err == nil {
		t.Fatal("an ended session's token still works")
	}
	if err := svc.Delete(ctx, "nope"); err == nil {
		t.Fatal("deleted a session that does not exist")
	}

	resumed, err := svc.Resume(ctx, session.TaskID, ReeveStart{})
	if err != nil {
		t.Fatal(err)
	}
	again, _ := db.GetReeveSession(ctx, resumed.TaskID)
	if again.APIToken == "" || again.APIToken == first.APIToken {
		t.Fatal("no new token on resume")
	}
	if agents.spawned[1].SessionPolicy != "resume" {
		t.Fatalf("resume spawn = %+v", agents.spawned[1])
	}
	if err := svc.Delete(ctx, session.TaskID); !errors.Is(err, ErrReeveNotEnded) {
		t.Fatalf("deleting a running session: %v", err)
	}
	svc.End(ctx, session.TaskID)
	if err := svc.Delete(ctx, session.TaskID); err != nil {
		t.Fatal(err)
	}
	if overview, _ := svc.Overview(ctx); len(overview.History) != 0 {
		t.Fatalf("history after delete = %+v", overview.History)
	}
}

func TestResumingWhileAnotherRunsIsRefused(t *testing.T) {
	svc, _, _, _ := newReeveFixture(t)
	ctx := context.Background()
	old, _ := svc.Start(ctx, ReeveStart{})
	svc.End(ctx, old.TaskID)
	svc.Start(ctx, ReeveStart{})
	if _, err := svc.Resume(ctx, old.TaskID, ReeveStart{}); !errors.Is(err, ErrReeveRunning) {
		t.Fatalf("resume while another runs: %v", err)
	}
}

// A reeve task never runs outside the sandbox.
func TestAReeveTaskIsRefusedOutsideTheSandbox(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	taskID, _ := db.CreateReeveTask(ctx, "The Reeve")
	svc := NewAgentService(db, newMockTmux(), t.TempDir())
	if err := svc.SpawnAgent(ctx, taskID, 0, 0, AgentSpawnOptions{Unsandboxed: true}); !errors.Is(err, ErrReeveSandboxed) {
		t.Fatalf("run without sandbox: %v", err)
	}
	if err := svc.SpawnAgent(ctx, taskID, 0, 0, AgentSpawnOptions{}); !errors.Is(err, ErrReeveSandboxed) {
		t.Fatalf("with the sandbox off: %v", err)
	}
}

func TestTheReevesLaunchPromptIsInTheSpecsOrder(t *testing.T) {
	svc, _, agents, _ := newReeveFixture(t)
	svc.opts.Snapshot = func(context.Context) string { return "## This instance\n- Running agents: 2" }
	ctx := context.Background()
	if _, err := svc.Start(ctx, ReeveStart{Brief: "The sync of LEG failed: 401."}); err != nil {
		t.Fatal(err)
	}
	prompt := agents.spawned[0].LaunchPrompt
	order := []string{"without your engram", "## You are the Reeve", "## This instance", "## The brief", "The sync of LEG failed"}
	last := -1
	for _, part := range order {
		i := strings.Index(prompt, part)
		if i < 0 || i < last {
			t.Fatalf("%q missing or out of order in:\n%s", part, prompt)
		}
		last = i
	}
	if agents.spawned[0].Prompt == "" {
		t.Fatal("a session with a brief does not start on it")
	}
}

func TestAResumedReeveSessionIsSentAFreshSnapshotFirst(t *testing.T) {
	svc, _, agents, _ := newReeveFixture(t)
	snap := "first"
	svc.opts.Snapshot = func(context.Context) string { return "## This instance: " + snap }
	ctx := context.Background()
	s, _ := svc.Start(ctx, ReeveStart{})
	svc.End(ctx, s.TaskID)
	snap = "second"
	svc.Resume(ctx, s.TaskID, ReeveStart{})
	if got := agents.spawned[1].Prompt; !strings.HasPrefix(got, "[legato] since you were last here:") || !strings.Contains(got, "second") {
		t.Fatalf("first message on resume = %q", got)
	}
}

type bootedEngram struct{}

func (bootedEngram) Boot(context.Context, string) (string, error) {
	return "## Memories\nthe person likes short answers", nil
}

// The one seam where the engram's boot goes: a booter that works puts its
// packet first.
func TestAnEngramThatBootsLeadsTheLaunchPrompt(t *testing.T) {
	prompt := ReeveLaunchPrompt(context.Background(), bootedEngram{}, "t", "## This instance", "")
	if !strings.HasPrefix(prompt, "## Memories") || strings.Contains(prompt, "without your engram") {
		t.Fatalf("prompt = %s", prompt)
	}
}

func TestAnIdleReeveSessionIsSuspendedAndTheNextMessageResumesIt(t *testing.T) {
	svc, db, agents, _ := newReeveFixture(t)
	ctx := context.Background()
	session, _ := svc.Start(ctx, ReeveStart{})
	row, _ := db.GetReeveSession(ctx, session.TaskID)
	token := row.APIToken

	active, _ := time.Parse("2006-01-02T15:04:05.000Z", row.ActiveAt)
	if n, _ := svc.SuspendIdle(ctx, active.Add(time.Hour), 2*time.Hour); n != 0 {
		t.Fatal("suspended after an hour")
	}
	if n, err := svc.SuspendIdle(ctx, active.Add(3*time.Hour), 2*time.Hour); err != nil || n != 1 {
		t.Fatalf("SuspendIdle = %d, %v", n, err)
	}
	row, _ = db.GetReeveSession(ctx, session.TaskID)
	if row.State != store.ReeveSuspended || len(agents.killed) != 1 {
		t.Fatalf("after idle: %s, killed %v", row.State, agents.killed)
	}
	// It still counts as the running session, and its token stays for resume.
	if _, err := svc.Start(ctx, ReeveStart{}); !errors.Is(err, ErrReeveRunning) {
		t.Fatalf("a start while suspended: %v", err)
	}
	if row.APIToken != token {
		t.Fatal("the token changed on suspend")
	}

	if err := svc.Message(ctx, "is the sync fixed?", "web"); err != nil {
		t.Fatal(err)
	}
	row, _ = db.GetReeveSession(ctx, session.TaskID)
	if row.State != store.ReeveRunning || row.APIToken != token {
		t.Fatalf("after a message: %s, same token %v", row.State, row.APIToken == token)
	}
	last := agents.spawned[len(agents.spawned)-1]
	if last.SessionPolicy != "resume" || !strings.Contains(last.Prompt, "is the sync fixed?") {
		t.Fatalf("resume spawn = %+v", last)
	}
}

func TestAMessageWithNoSessionStartsOne(t *testing.T) {
	svc, db, agents, _ := newReeveFixture(t)
	ctx := context.Background()
	if err := svc.Message(ctx, "pair the new worker", "web"); err != nil {
		t.Fatal(err)
	}
	if current, _ := db.CurrentReeveSession(ctx); current == nil || len(agents.spawned) != 1 || !strings.Contains(agents.spawned[0].LaunchPrompt, "pair the new worker") {
		t.Fatalf("current %+v, spawned %+v", current, agents.spawned)
	}
}

func TestTheOverviewSaysTheInstanceItsRoleAndWhatWaitsForApproval(t *testing.T) {
	svc, db, _, _ := newReeveFixture(t)
	ctx := context.Background()
	s, _ := svc.Start(ctx, ReeveStart{})
	if _, _, err := db.InsertApproval(ctx, store.Approval{ID: "ap-1", Source: "reeve_change_set", SourceKey: "cs-1", TaskID: s.TaskID, Title: "Restart", DetailsJSON: "{}", OptionsJSON: "[]"}); err != nil {
		t.Fatal(err)
	}
	overview, err := svc.Overview(ctx)
	if err != nil || overview.Instance == "" || overview.Role != "standalone" || overview.ToApprove != 1 {
		t.Fatalf("overview = %+v, %v", overview, err)
	}
}

type expiringApprovals struct{ expired []string }

func (e *expiringApprovals) ExpireTask(_ context.Context, taskID string) error {
	e.expired = append(e.expired, taskID)
	return nil
}

func TestPendingApprovalsExpireWhenTheSessionEndsNotWhenItIsSuspended(t *testing.T) {
	svc, db, _, _ := newReeveFixture(t)
	approvals := &expiringApprovals{}
	svc.opts.Approvals = approvals
	ctx := context.Background()
	s, _ := svc.Start(ctx, ReeveStart{})
	row, _ := db.GetReeveSession(ctx, s.TaskID)
	active, _ := time.Parse("2006-01-02T15:04:05.000Z", row.ActiveAt)
	svc.SuspendIdle(ctx, active.Add(3*time.Hour), 2*time.Hour)
	if len(approvals.expired) != 0 {
		t.Fatal("approvals expired on suspend")
	}
	svc.End(ctx, s.TaskID)
	if len(approvals.expired) != 1 || approvals.expired[0] != s.TaskID {
		t.Fatalf("expired = %v", approvals.expired)
	}
}

// Opening the Reeve (the TUI's R) starts a session, goes to the running
// one, or resumes a suspended one.
func TestOpeningTheReeveStartsGoesToOrResumesItsSession(t *testing.T) {
	svc, db, agents, _ := newReeveFixture(t)
	ctx := context.Background()
	first, err := svc.Open(ctx, "tui")
	if err != nil || first == "" || len(agents.spawned) != 1 {
		t.Fatalf("open = %q, %v; spawned %d", first, err, len(agents.spawned))
	}
	again, err := svc.Open(ctx, "tui")
	if err != nil || again != first || len(agents.spawned) != 1 {
		t.Fatalf("open again = %q, %v; spawned %d", again, err, len(agents.spawned))
	}
	row, _ := db.GetReeveSession(ctx, first)
	active, _ := time.Parse("2006-01-02T15:04:05.000Z", row.ActiveAt)
	svc.SuspendIdle(ctx, active.Add(3*time.Hour), 2*time.Hour)
	resumed, err := svc.Open(ctx, "tui")
	if row, _ = db.GetReeveSession(ctx, first); err != nil || resumed != first || row.State != store.ReeveRunning || len(agents.spawned) != 2 {
		t.Fatalf("open suspended = %q, %v; state %s, spawned %d", resumed, err, row.State, len(agents.spawned))
	}
}

// Asking the Reeve (the TUI's a) starts a session with the brief, and says
// which session has it.
func TestAskingTheReeveSaysWhichSessionHasTheBrief(t *testing.T) {
	svc, _, agents, _ := newReeveFixture(t)
	ctx := context.Background()
	taskID, err := svc.Ask(ctx, "why is agent T-1 stuck?", "tui")
	if err != nil || taskID == "" || !strings.Contains(agents.spawned[0].LaunchPrompt, "why is agent T-1 stuck?") {
		t.Fatalf("ask = %q, %v; spawned %+v", taskID, err, agents.spawned)
	}
}
