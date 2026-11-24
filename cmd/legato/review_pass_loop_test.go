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
	} {
		if out, code := runCLICaptured(t, args...); code != exitUsage {
			t.Fatalf("%v: exit %d, out %s", args, code, out)
		}
	}
}
