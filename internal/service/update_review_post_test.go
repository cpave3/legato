package service

import (
	"context"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// noAgents is an agent list with nobody in it.
type noAgents struct{}

func (noAgents) ListAgents(context.Context) ([]AgentSession, error) { return nil, nil }

// A review pass the reviewer submitted and Legato is posting to GitHub holds
// an automatic update back, as an agent mid-turn does: a restart would cut
// the post in half. Once it is posted, the update installs.
func TestInAutoModeAnUpdateWaitsForAReviewPassMidPost(t *testing.T) {
	f := newUpdater(t, "v1.2.0", func(o *UpdateOptions) { o.Busy = UpdateBusy(o.Store.(*store.Store), noAgents{}) })
	ctx := context.Background()
	createTask(t, f.db, "card-1")
	if err := f.db.SaveHandoffReview(ctx, store.HandoffReview{TaskID: "card-1", Phase: ReviewPhaseReviewing, Pass: 1}); err != nil {
		t.Fatal(err)
	}
	pass := store.HandoffReviewPass{TaskID: "card-1", Pass: 1, State: ReviewPassSubmitted, Verdict: VerdictChanges}
	if err := f.db.InsertHandoffReviewPass(ctx, pass, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SetMode(ctx, UpdateModeAuto); err != nil {
		t.Fatal(err)
	}
	running(t, f)

	eventually(t, "the update to wait", func() bool { return f.svc.Status(ctx).State == UpdateWaiting })
	time.Sleep(20 * time.Millisecond)
	if got := f.releases.installs(); len(got) != 0 {
		t.Fatalf("installed %v while a review pass was being posted", got)
	}

	pass.State = ReviewPassPosted
	if err := f.db.UpdateHandoffReviewPass(ctx, pass); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the install", func() bool { return len(f.releases.installs()) == 1 })
}

// What holds an update back: an agent mid-turn, or a loop's current pass
// submitted and not yet posted. A submitted pass of a loop that is stuck
// (GitHub refused it) or ended is not being posted.
func TestUpdateBusy(t *testing.T) {
	f := newUpdater(t, "v1.2.0")
	ctx := context.Background()
	busy := UpdateBusy(f.db, noAgents{})
	if got, err := busy(ctx); err != nil || got {
		t.Fatalf("nothing running: %v, %v", got, err)
	}
	for _, phase := range []string{ReviewPhaseStuck, ReviewPhaseEnded} {
		task := "card-" + phase
		createTask(t, f.db, task)
		if err := f.db.SaveHandoffReview(ctx, store.HandoffReview{TaskID: task, Phase: phase, Pass: 1}); err != nil {
			t.Fatal(err)
		}
		if err := f.db.InsertHandoffReviewPass(ctx, store.HandoffReviewPass{TaskID: task, Pass: 1, State: ReviewPassSubmitted}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := busy(ctx); err != nil || got {
		t.Fatalf("a stuck or ended loop's submitted pass holds the update: %v, %v", got, err)
	}
	working := UpdateBusy(f.db, agentList{{Status: "running", Activity: store.ActivityWorking}})
	if got, err := working(ctx); err != nil || !got {
		t.Fatalf("an agent mid-turn: %v, %v", got, err)
	}
}

type agentList []AgentSession

func (a agentList) ListAgents(context.Context) ([]AgentSession, error) { return a, nil }
