package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

type watchFixture struct {
	db      *store.Store
	watch   *ReeveWatcher
	reeve   string
	task    string
	told    []string
	toldTo  []string
	session int
}

// newWatchFixture has a running Reeve session and a task with a running
// agent, which the Reeve watches.
func newWatchFixture(t *testing.T) *watchFixture {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	f := &watchFixture{db: db}
	f.reeve, err = db.CreateReeveTask(ctx, "Reeve")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InsertReeveSession(ctx, store.ReeveSession{TaskID: f.reeve, State: "running", AgentKind: "claude-code"}); err != nil {
		t.Fatal(err)
	}
	f.task = "T-1"
	if err := db.CreateTask(ctx, store.Task{ID: f.task, Title: "Docker in the sandbox", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	f.session, err = db.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: f.task, TmuxSession: "legato-T-1", Command: "claude", AgentKind: "claude-code", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateExecution(ctx, store.Execution{ID: "ex-1", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: f.task, AgentSessionID: &f.session, StartedAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	f.watch = NewReeveWatcher(ReeveWatcherOptions{Store: db, Notify: func(_ context.Context, reeveTaskID, text string) {
		f.toldTo = append(f.toldTo, reeveTaskID)
		f.told = append(f.told, text)
	}})
	return f
}

// check runs one check and returns what the Reeve was told.
func (f *watchFixture) check(t *testing.T) string {
	t.Helper()
	f.told = nil
	f.watch.Check(context.Background())
	return strings.Join(f.told, "\n---\n")
}

func TestTheReeveIsToldWhatAnAgentItStartedDoes(t *testing.T) {
	f := newWatchFixture(t)
	ctx := context.Background()
	if _, err := AddTaskNote(ctx, f.db, f.task, "an earlier note", "agent"); err != nil {
		t.Fatal(err)
	}
	if err := f.watch.Watch(ctx, f.reeve, f.task); err != nil {
		t.Fatal(err)
	}
	if got := f.check(t); got != "" {
		t.Fatalf("told before anything happened: %q", got)
	}

	// A note, and a turn of work ended.
	if _, err := AddTaskNote(ctx, f.db, f.task, "Opened the PR; tests pass.", "agent"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	f.db.RecordExecutionActivityAt(ctx, "ex-1", store.ActivityWorking, "", now)
	f.db.RecordExecutionActivityAt(ctx, "ex-1", "", "", now.Add(time.Millisecond))
	got := f.check(t)
	for _, want := range []string{"[legato agent]", "T-1", "Docker in the sandbox", "Opened the PR; tests pass.", "finished its turn"} {
		if !strings.Contains(got, want) {
			t.Errorf("the message lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "an earlier note") {
		t.Errorf("told of a note from before it watched:\n%s", got)
	}
	if len(f.toldTo) != 1 || f.toldTo[0] != f.reeve {
		t.Fatalf("told %v", f.toldTo)
	}
	if got := f.check(t); got != "" {
		t.Fatalf("told again: %q", got)
	}

	// A pull request opened.
	if _, err := f.db.TrackTaskBranches(ctx, f.task, []store.BranchRef{{Repo: "cpave3/legato", Branch: "legato/t-1"}}, "api"); err != nil {
		t.Fatal(err)
	}
	if err := f.db.SetTaskBranchPR(ctx, f.task, "cpave3/legato", "legato/t-1", &store.PRMeta{PRNumber: 60, PRURL: "https://github.com/cpave3/legato/pull/60", State: "OPEN", CheckStatus: "pending"}); err != nil {
		t.Fatal(err)
	}
	if got := f.check(t); !strings.Contains(got, "pull request #60") {
		t.Fatalf("not told of the pull request: %q", got)
	}

	// Waiting for the operator.
	f.db.UpdateAgentActivity(ctx, f.task, store.ActivityWaiting)
	if got := f.check(t); !strings.Contains(got, "waiting") {
		t.Fatalf("not told it waits: %q", got)
	}

	// It ends: told once, and no longer watched.
	if err := f.db.UpdateAgentSessionStatus(ctx, f.task, "dead"); err != nil {
		t.Fatal(err)
	}
	if got := f.check(t); !strings.Contains(got, "ended") {
		t.Fatalf("not told it ended: %q", got)
	}
	if watches, _ := f.db.ListReeveWatches(ctx); len(watches) != 0 {
		t.Fatalf("still watched: %+v", watches)
	}
}

// A watch of a Reeve session that is no longer the current one is dropped
// without a word.
func TestAWatchOfAnEndedSessionIsDropped(t *testing.T) {
	f := newWatchFixture(t)
	ctx := context.Background()
	if err := f.watch.Watch(ctx, "R-old", f.task); err != nil {
		t.Fatal(err)
	}
	AddTaskNote(ctx, f.db, f.task, "news", "agent")
	if got := f.check(t); got != "" {
		t.Fatalf("an old session was told: %q", got)
	}
	if watches, _ := f.db.ListReeveWatches(ctx); len(watches) != 0 {
		t.Fatalf("still watched: %+v", watches)
	}
}

// A spawn reports the task it made, so the caller can address its agent.
func TestAnEphemeralSpawnReportsItsTask(t *testing.T) {
	svc, s, _ := newTestAgentService(t)
	ctx := context.Background()
	if err := s.CreateColumnMapping(ctx, store.ColumnMapping{ColumnName: "Backlog"}); err != nil {
		t.Fatal(err)
	}
	var report SpawnReport
	if err := svc.SpawnEphemeralAgent(WithSpawnReport(ctx, &report), "look into it", 0, 0); err != nil {
		t.Fatal(err)
	}
	agents, _ := svc.ListAgents(ctx)
	if len(agents) != 1 || report.TaskID != agents[0].TaskID {
		t.Fatalf("report %+v, agents %+v", report, agents)
	}
}

// An agent a change set starts or messages is watched for the session.
func TestAnAgentAChangeSetStartsOrMessagesIsWatched(t *testing.T) {
	for _, tc := range []struct {
		call ChangeCall
		body string
		want string
	}{
		{ChangeCall{Kind: CallAPI, Method: "POST", Path: "/api/tasks/T-9/agent"}, `{"status":"ok"}`, "T-9"},
		{ChangeCall{Kind: CallAPI, Method: "POST", Path: "/api/agents/spawn"}, `{"status":"ok","task_id":"eph-1"}`, "eph-1"},
		{ChangeCall{Kind: CallAPI, Method: "POST", Path: "/api/agents/T-8/chimera/send"}, `{}`, "T-8"},
		{ChangeCall{Kind: CallAPI, Method: "POST", Path: "/api/agents/T-7/restart"}, `{}`, "T-7"},
		{ChangeCall{Kind: CallLegato, Args: []string{"agent", "message", "T-6", "hi"}}, "", "T-6"},
		{ChangeCall{Kind: CallAPI, Method: "PATCH", Path: "/api/tasks/T-5"}, `{}`, ""},
		{ChangeCall{Kind: CallAPI, Method: "POST", Path: "/api/agents/kill"}, `{}`, ""},
	} {
		if got := agentStartedBy(tc.call, tc.body); got != tc.want {
			t.Errorf("%+v = %q, want %q", tc.call, got, tc.want)
		}
	}
}

func TestAnApprovedSetThatStartsAnAgentWatchesIt(t *testing.T) {
	f := newChangeSetFixture(t)
	var watched []string
	f.svc.opts.Watch = func(_ context.Context, reeve, task string) { watched = append(watched, reeve+" "+task) }
	ctx := context.Background()
	out, _ := f.svc.Propose(ctx, f.reeve, "start it", []ChangeCall{{Kind: CallAPI, Method: "POST", Path: "/api/tasks/T-9/agent"}})
	f.decide(t, out.ApprovalID, ApprovalAllow, "")
	f.svc.RunQueued(ctx)
	if len(watched) != 1 || watched[0] != f.reeve+" T-9" {
		t.Fatalf("watched %v", watched)
	}
}

// An agent still being prepared (its clone being made) has no session yet:
// it is not reported as ended, and the watch waits for it a while.
func TestAnAgentNotStartedYetIsNotReportedAsEnded(t *testing.T) {
	f := newWatchFixture(t)
	ctx := context.Background()
	now := time.Now()
	f.watch.opts.Now = func() time.Time { return now }
	if err := f.db.CreateTask(ctx, store.Task{ID: "T-2", Title: "preparing", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := f.watch.Watch(ctx, f.reeve, "T-2"); err != nil {
		t.Fatal(err)
	}
	if got := f.check(t); got != "" {
		t.Fatalf("told of an agent not started yet: %q", got)
	}
	if watches, _ := f.db.ListReeveWatches(ctx); len(watches) != 1 {
		t.Fatalf("watches = %+v", watches)
	}
	// One that never starts is dropped without a word.
	now = now.Add(reeveWatchStartWait + time.Minute)
	if got := f.check(t); got != "" {
		t.Fatalf("told: %q", got)
	}
	if watches, _ := f.db.ListReeveWatches(ctx); len(watches) != 0 {
		t.Fatalf("still watched: %+v", watches)
	}
}
