package main

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// `legato review-pass list` prints the Shipping page's list (LEG-706):
// every review loop with its phase, as GET /api/shipping gives it.
func TestReviewPassListPrintsEveryLoopWithItsPhase(t *testing.T) {
	_, db := reviewPassStore(t)
	outsideASession(t)
	loop := stewardedLoop(t, db)

	out, code := runCLICaptured(t, "review-pass", "list", "--json")
	if code != 0 || !strings.Contains(out, `"loop_id":"`+loop+`"`) || !strings.Contains(out, `"phase":"watching"`) {
		t.Fatalf("list --json: exit %d, out %s", code, out)
	}
	out, code = runCLICaptured(t, "review-pass", "list")
	if code != 0 || !strings.Contains(out, "acme/widget#12") || !strings.Contains(out, "watching") || !strings.Contains(out, loop) {
		t.Fatalf("list: exit %d, out %s", code, out)
	}
}

// The command has no agent service, yet reads what the author's agent is
// doing from its session's record, as the web UI's list does.
func TestReviewPassListSaysWhatTheAuthorsAgentIsDoing(t *testing.T) {
	_, db := reviewPassStore(t)
	outsideASession(t)
	loop := stewardedLoop(t, db)
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "author-1", Title: "Greeting", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	rec, err := db.GetReviewLoop(ctx, loop)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpdateReviewLoop(ctx, loop, rec.Rev, func(tx *store.ReviewLoopTx) error { tx.Loop.AuthorTask = "author-1"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertAgentSession(ctx, store.AgentSession{TaskID: "author-1", TmuxSession: "legato-author-1", Command: "claude", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateAgentActivity(ctx, "author-1", store.ActivityWorking); err != nil {
		t.Fatal(err)
	}

	out, code := runCLICaptured(t, "review-pass", "list", "--json")
	if code != 0 || !strings.Contains(out, `"author":{"task":"author-1","title":"Greeting","state":"working"}`) {
		t.Fatalf("list --json: exit %d, out %s", code, out)
	}
}
