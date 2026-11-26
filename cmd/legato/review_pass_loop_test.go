package main

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// outsideASession clears every variable that makes a command a session's.
func outsideASession(t *testing.T) {
	t.Helper()
	for _, name := range []string{"LEGATO_TASK_ID", "LEGATO_EXECUTION_ID", "LEGATO_BROKER_SOCKET", "LEGATO_BROKER_TOKEN", "LEGATO_SANDBOXED"} {
		t.Setenv(name, "")
	}
}

func hostedLoopID(t *testing.T, db *store.Store, taskID string) string {
	t.Helper()
	l, err := db.CurrentReviewLoop(context.Background(), taskID)
	if err != nil || l == nil {
		t.Fatalf("no loop on %s: %v", taskID, err)
	}
	return l.ID
}

// recordedMessages is the agents the steering commands message, in tests.
type recordedMessages struct{ sent map[string]string }

func (r *recordedMessages) QueueMessage(_ context.Context, taskID, message string) error {
	r.sent[taskID] = message
	return nil
}

func recordMessages(t *testing.T) *recordedMessages {
	t.Helper()
	recorded := &recordedMessages{sent: map[string]string{}}
	old := reviewLoopAgents
	reviewLoopAgents = func(*config.Config, *store.Store) (service.ReviewLoopMessenger, error) { return recorded, nil }
	t.Cleanup(func() { reviewLoopAgents = old })
	return recorded
}

// On the host, outside any session, --loop reads the loop's feedback and
// answers it, and steers it: Another pass and Message the author (story 24).
func TestOnTheHostLoopReadsAndActsOnTheLoop(t *testing.T) {
	_, db := reviewPassStore(t)
	reviewedTask(t, db, "task-1", store.SlotMain, "")
	outsideASession(t)
	loop := hostedLoopID(t, db, "task-1")
	recorded := recordMessages(t)

	out, code := runCLICaptured(t, "review-pass", "feedback", "--loop", loop, "--json")
	if code != 0 || !strings.Contains(out, `"f-1"`) || !strings.Contains(out, `"task_id":"task-1"`) {
		t.Fatalf("feedback: exit %d, out %s", code, out)
	}
	out, code = runCLICaptured(t, "review-pass", "answer", "f-1", "--fixed", "--note", "bounded", "--loop", loop, "--json")
	if code != 0 || !strings.Contains(out, `"task_id":"task-1"`) {
		t.Fatalf("answer: exit %d, out %s", code, out)
	}
	if item := answerOf(t, db, "task-1"); item.Answer != "fixed" || item.AnswerNote != "bounded" {
		t.Fatalf("item = %+v", item)
	}

	out, code = runCLICaptured(t, "review-pass", "message", "--loop", loop, "--text", "Rebase first.", "--json")
	if code != 0 || recorded.sent["task-1"] != "[legato review pass] From the operator: Rebase first." {
		t.Fatalf("message: exit %d, out %s, sent %v", code, out, recorded.sent)
	}

	// The loop waits for its author and is a handoff's: not stuck.
	out, code = runCLICaptured(t, "review-pass", "another-pass", "--loop", loop, "--json")
	if code == 0 || !strings.Contains(out, "not stuck") {
		t.Fatalf("another pass of a loop that is not stuck: exit %d, out %s", code, out)
	}
	if err := db.SaveHandoffReview(context.Background(), store.HandoffReview{TaskID: "task-1", Phase: service.ReviewPhaseStuck, Pass: 1, UpdatedAt: "now"}); err != nil {
		t.Fatal(err)
	}
	out, code = runCLICaptured(t, "review-pass", "another-pass", "--loop", loop, "--json")
	if code != 0 {
		t.Fatalf("another pass: exit %d, out %s", code, out)
	}
	if l, _ := db.GetHandoffReview(context.Background(), "task-1"); l.Phase != service.ReviewPhaseReady {
		t.Fatalf("after another pass: %+v", l)
	}

	out, code = runCLICaptured(t, "review-pass", "feedback", "--loop", "rl-000000000000", "--json")
	if code != exitNotFound || !strings.Contains(out, `"loop_not_found"`) {
		t.Fatalf("unknown loop: exit %d, out %s", code, out)
	}
}

// Inside a session, --loop is refused, as --task is: an agent acts only on
// the loop its own task authors. The steering commands are the operator's.
func TestInsideASessionLoopIsRefused(t *testing.T) {
	_, db := reviewPassStore(t)
	execution := reviewedTask(t, db, "task-1", store.SlotMain, "")
	loop := hostedLoopID(t, db, "task-1")
	recorded := recordMessages(t)
	t.Setenv("LEGATO_TASK_ID", "task-1")
	t.Setenv("LEGATO_EXECUTION_ID", execution)

	for _, args := range [][]string{
		{"review-pass", "feedback", "--loop", loop, "--json"},
		{"review-pass", "answer", "f-1", "--fixed", "--loop", loop, "--json"},
		{"review-pass", "another-pass", "--loop", loop, "--json"},
		{"review-pass", "message", "--loop", loop, "--text", "hi", "--json"},
		{"review-pass", "stop-watching", "--loop", loop, "--json"},
		{"review-pass", "watch-again", "--loop", loop, "--json"},
		{"review-pass", "end", "--loop", loop, "--json"},
	} {
		out, code := runCLICaptured(t, args...)
		if code != exitForbidden || !strings.Contains(out, `"loop_in_session"`) {
			t.Fatalf("%v: exit %d, out %s", args, code, out)
		}
	}
	if item := answerOf(t, db, "task-1"); item.Answer != "" {
		t.Fatalf("a session answered by --loop: %+v", item)
	}
	if len(recorded.sent) != 0 {
		t.Fatalf("a session messaged by --loop: %v", recorded.sent)
	}
}

// --loop and --task name the loop twice; the steering commands need --loop.
func TestOnTheHostLoopAndTaskAreExclusive(t *testing.T) {
	_, db := reviewPassStore(t)
	reviewedTask(t, db, "task-1", store.SlotMain, "")
	outsideASession(t)
	loop := hostedLoopID(t, db, "task-1")
	for _, args := range [][]string{
		{"review-pass", "feedback", "--loop", loop, "--task", "task-1", "--json"},
		{"review-pass", "another-pass", "--json"},
		{"review-pass", "message", "--text", "hi", "--json"},
		{"review-pass", "stop-watching", "--json"},
		{"review-pass", "watch-again", "--json"},
		{"review-pass", "end", "--json"},
	} {
		if out, code := runCLICaptured(t, args...); code != exitUsage {
			t.Fatalf("%v: exit %d, out %s", args, code, out)
		}
	}
}

// stewardedLoop is a run's review loop whose steward watches an approved
// #12 at abc1234: the run runs, the loop is clean and confirmed.
func stewardedLoop(t *testing.T, db *store.Store) string {
	t.Helper()
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "host-1", Title: "Review loop", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateWorkflowRun(ctx, store.WorkflowRun{ID: "wr-1", WorkflowName: "review-loop", WorkflowTitle: "Review loop", DefinitionJSON: "{}",
		InputsJSON: "{}", Status: "running", CurrentStepID: "steward", TaskID: "host-1"}, nil); err != nil {
		t.Fatal(err)
	}
	loop, _, err := db.OpenReviewLoop(ctx, store.ReviewLoopOpen{Loop: store.ReviewLoop{
		OwnerKind: store.ReviewLoopOwnerRun, RunID: "wr-1", StepPath: "open", AttemptID: "a1", HostTask: "host-1", Repo: "acme/widget",
		Scope: "stack", AgentAuthor: true, Steward: true, MaxPasses: 3, MaxReentries: 5, CreatedAt: "2026-11-24T10:00:00Z",
	}, PRs: []store.ReviewLoopPR{{Number: 12, Branch: "feature", Base: "main"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpdateReviewLoop(ctx, loop.ID, loop.Rev, func(tx *store.ReviewLoopTx) error {
		tx.Loop.Phase, tx.Loop.Pass, tx.Loop.CleanPass = service.ReviewPhaseClean, 1, 1
		tx.Loop.ConfirmedAt, tx.Loop.IdleDeadline = "2026-11-24T11:00:00Z", "2026-12-01T11:00:00Z"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return loop.ID
}

// On the host, --loop steers a steward as the run page does: Stop
// watching, Watch again and End (outcome ended), which the engine's tick
// then follows.
func TestOnTheHostLoopStopsWatchingWatchesAgainAndEndsASteward(t *testing.T) {
	_, db := reviewPassStore(t)
	outsideASession(t)
	loop := stewardedLoop(t, db)
	ctx := context.Background()

	out, code := runCLICaptured(t, "review-pass", "watch-again", "--loop", loop, "--json")
	if code == 0 || !strings.Contains(out, `"steward_not_held"`) {
		t.Fatalf("watch again on a watching steward: exit %d, out %s", code, out)
	}
	out, code = runCLICaptured(t, "review-pass", "stop-watching", "--loop", loop, "--json")
	if code != 0 || !strings.Contains(out, `"action":"stop_watching"`) {
		t.Fatalf("stop watching: exit %d, out %s", code, out)
	}
	if rec, _ := db.GetReviewLoop(ctx, loop); !rec.Held {
		t.Fatal("stop-watching did not hold the steward")
	}
	out, code = runCLICaptured(t, "review-pass", "stop-watching", "--loop", loop, "--json")
	if code == 0 || !strings.Contains(out, `"steward_not_watching"`) {
		t.Fatalf("stop watching a held steward: exit %d, out %s", code, out)
	}
	out, code = runCLICaptured(t, "review-pass", "watch-again", "--loop", loop, "--json")
	if code != 0 || !strings.Contains(out, `"action":"watch_again"`) {
		t.Fatalf("watch again: exit %d, out %s", code, out)
	}
	if rec, _ := db.GetReviewLoop(ctx, loop); rec.Held {
		t.Fatal("watch-again left the steward held")
	}

	out, code = runCLICaptured(t, "review-pass", "end", "--loop", loop, "--json")
	if code != 0 || !strings.Contains(out, `"action":"end"`) {
		t.Fatalf("end: exit %d, out %s", code, out)
	}
	if rec, _ := db.GetReviewLoop(ctx, loop); rec.State != store.ReviewLoopClosedState || rec.Outcome != service.ReviewOutcomeEnded {
		t.Fatalf("after end: %s, outcome %q", rec.State, rec.Outcome)
	}
	out, code = runCLICaptured(t, "review-pass", "end", "--loop", loop, "--json")
	if code != exitNotFound || !strings.Contains(out, `"loop_closed"`) {
		t.Fatalf("end of an ended loop: exit %d, out %s", code, out)
	}
}

// A loop with no steward takes neither Stop watching nor Watch again, and
// End only when it is stuck; a handoff's loop ends with its handoff.
func TestOnTheHostLoopRefusesStewardActionsToALoopWithNoSteward(t *testing.T) {
	_, db := reviewPassStore(t)
	reviewedTask(t, db, "task-1", store.SlotMain, "")
	outsideASession(t)
	loop := hostedLoopID(t, db, "task-1")
	for _, args := range [][]string{
		{"review-pass", "stop-watching", "--loop", loop, "--json"},
		{"review-pass", "watch-again", "--loop", loop, "--json"},
		{"review-pass", "end", "--loop", loop, "--json"},
	} {
		if out, code := runCLICaptured(t, args...); code == 0 {
			t.Fatalf("%v: exit %d, out %s", args, code, out)
		}
	}
	if rec, _ := db.GetReviewLoop(context.Background(), loop); rec.State != store.ReviewLoopOpenState || rec.Held {
		t.Fatalf("the loop moved: %+v", rec)
	}
}
