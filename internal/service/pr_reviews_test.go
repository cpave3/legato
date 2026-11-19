package service

import (
	"context"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
)

func TestNewChangesRequestedReviewsFireOnce(t *testing.T) {
	gh := newFakeGitHub(t)
	before := "2020-01-01T00:00:00Z"
	later := func() string { return time.Now().Add(time.Minute).UTC().Format(time.RFC3339) }
	reviews := []fakeReview{{ID: 1, State: "CHANGES_REQUESTED", Login: "alice", Submitted: before}}
	// Older comments push the reviews past a page.
	for i := 0; i < 120; i++ {
		reviews = append(reviews, fakeReview{ID: int64(1000 + i), State: "COMMENTED", Login: "carol", Submitted: before})
	}
	pr := fakePR{Number: 7, Head: "feature/task-1", Reviews: reviews}
	gh.set("acme/widget", pr)
	s, svc, bus := newBranchTracking(t, gh)
	svc.SetGitHubAppLogins([]string{"legato-reviewer[bot]"})
	ctx := context.Background()
	createPRTask(t, s, "task-1")
	setRepoContext(t, s, "task-1", "acme/widget", "feature/task-1")
	fired := bus.Subscribe(events.EventPRChangesRequested)
	drain := func() []events.PRChangesRequestedPayload {
		var out []events.PRChangesRequestedPayload
		for {
			select {
			case e := <-fired:
				out = append(out, e.Payload.(events.PRChangesRequestedPayload))
			case <-time.After(50 * time.Millisecond):
				return out
			}
		}
	}
	refresh := func() {
		t.Helper()
		if err := svc.PollAll(ctx); err != nil {
			t.Fatal(err)
		}
		if err := svc.RefreshPR(ctx, "task-1"); err != nil {
			t.Fatal(err)
		}
	}
	refresh()
	if got := drain(); len(got) != 0 {
		t.Fatalf("a review from before the task tracked the branch fired: %+v", got)
	}

	add := func(r fakeReview) {
		pr.Reviews = append(pr.Reviews, r)
		gh.set("acme/widget", pr)
	}
	add(fakeReview{ID: 2, State: "CHANGES_REQUESTED", Login: "alice", Submitted: later()})
	refresh()
	got := drain()
	if len(got) != 1 || got[0].ReviewID != 2 || got[0].TaskID != "task-1" || got[0].Repo != "acme/widget" || got[0].Number != 7 || got[0].Reviewer != "alice" {
		t.Fatalf("first changes-requested = %+v", got)
	}
	refresh()
	if got := drain(); len(got) != 0 {
		t.Fatalf("a seen review fired again: %+v", got)
	}
	add(fakeReview{ID: 3, State: "CHANGES_REQUESTED", Login: "bob", Submitted: later()})
	add(fakeReview{ID: 4, State: "CHANGES_REQUESTED", Login: "alice", Submitted: later()})
	add(fakeReview{ID: 5, State: "CHANGES_REQUESTED", Login: "legato-reviewer[bot]", Submitted: later()})
	add(fakeReview{ID: 6, State: "APPROVED", Login: "bob", Submitted: later()})
	if err := s.RecordGitHubAppLogin(ctx, "legato-author[bot]"); err != nil {
		t.Fatal(err)
	}
	add(fakeReview{ID: 7, State: "CHANGES_REQUESTED", Login: "legato-author[bot]", Submitted: later()})
	refresh()
	got = drain()
	if len(got) != 2 || got[0].ReviewID != 3 || got[1].ReviewID != 4 {
		t.Fatalf("second reviewer and a repeat fire, Legato's Apps do not: %+v", got)
	}

	// A restart remembers what was seen.
	restarted := NewPRTrackingService(s, bus, gh.client(), time.Minute, time.Minute).(*prTrackingService)
	if err := restarted.RefreshPR(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}
	if got := drain(); len(got) != 0 {
		t.Fatalf("after a restart: %+v", got)
	}
}
