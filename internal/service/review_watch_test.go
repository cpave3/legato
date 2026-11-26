package service

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// watchClock is a clock the test moves.
type watchClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *watchClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *watchClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// fakeWatchGitHub is GitHub for the steward's watch: each pull request's
// head, and the sessions opened, by repository, with the reads each made.
type fakeWatchGitHub struct {
	ReviewGitHub
	mu    sync.Mutex
	heads map[int]string
	// mergeable is each pull request's mergeable, MERGEABLE when unset.
	mergeable map[int]string
	sessions  []*fakeWatchSession
	// limit refuses every watch with it while set.
	limit *GitHubRateLimit
}

type fakeWatchSession struct {
	g        *fakeWatchGitHub
	repo     string
	watches  [][]int
	comments []int
	closed   bool
}

func (g *fakeWatchGitHub) WatchSession(repo string) ReviewWatchSession {
	g.mu.Lock()
	defer g.mu.Unlock()
	s := &fakeWatchSession{g: g, repo: repo}
	g.sessions = append(g.sessions, s)
	return s
}

func (s *fakeWatchSession) Watch(_ context.Context, numbers []int) ([]PRWatch, error) {
	s.g.mu.Lock()
	defer s.g.mu.Unlock()
	s.watches = append(s.watches, numbers)
	if s.g.limit != nil {
		held := *s.g.limit
		return nil, &held
	}
	var out []PRWatch
	for _, n := range numbers {
		mergeable := s.g.mergeable[n]
		if mergeable == "" {
			mergeable = "MERGEABLE"
		}
		out = append(out, PRWatch{Number: n, State: "open", HeadSHA: s.g.heads[n], Mergeable: mergeable})
	}
	return out, nil
}

func (s *fakeWatchSession) PollComments(_ context.Context, number int, _ PollPages) (CommentPoll, error) {
	s.g.mu.Lock()
	defer s.g.mu.Unlock()
	s.comments = append(s.comments, number)
	return CommentPoll{}, nil
}

func (s *fakeWatchSession) Stacked(context.Context, string) ([]ReviewGitHubPR, error) {
	return nil, nil
}

func (s *fakeWatchSession) Close() { s.closed = true }

// tickSessions ticks once and returns the sessions the tick opened.
func (g *fakeWatchGitHub) tickSessions(t *testing.T, svc *ReviewPassService) []*fakeWatchSession {
	t.Helper()
	g.mu.Lock()
	before := len(g.sessions)
	g.mu.Unlock()
	svc.Tick(context.Background())
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.sessions[before:])
}

type stewardWatchFixture struct {
	db     *store.Store
	clock  *watchClock
	github *fakeWatchGitHub
	svc    *ReviewPassService
}

func newStewardWatchFixture(t *testing.T) stewardWatchFixture {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	clock := &watchClock{now: time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)}
	github := &fakeWatchGitHub{heads: map[int]string{12: "a1", 13: "b1", 20: "c1", 30: "d1"}}
	svc := NewReviewPassService(db, ReviewPassOptions{
		Config: func() ReviewPassConfig { return ReviewPassConfig{Enabled: true} },
		GitHub: github, Now: clock.Now,
	})
	return stewardWatchFixture{db: db, clock: clock, github: github, svc: svc}
}

// openLoop opens a run's loop on prs, made now.
func (f stewardWatchFixture) openLoop(t *testing.T, run, repo string, steward bool, prs ...int) string {
	t.Helper()
	createTask(t, f.db, "task-"+run)
	open := store.ReviewLoopOpen{Loop: store.ReviewLoop{OwnerKind: store.ReviewLoopOwnerRun, RunID: run, StepPath: "review",
		HostTask: "task-" + run, Repo: repo, Steward: steward, CreatedAt: f.clock.Now().Format(time.RFC3339)}}
	for _, n := range prs {
		open.PRs = append(open.PRs, store.ReviewLoopPR{Number: n})
	}
	loop, _, err := f.db.OpenReviewLoop(context.Background(), open)
	if err != nil {
		t.Fatal(err)
	}
	return loop.ID
}

// Each due loop gets one session a tick: one token for its watch and its
// comment reads (spec LEG-559 C7, AC 38). Loops without the steward are
// not read.
func TestTheWatchOpensOneSessionPerLoopPerTick(t *testing.T) {
	f := newStewardWatchFixture(t)
	stack := f.openLoop(t, "wr-1", "cpave3/legato", true, 12, 13)
	other := f.openLoop(t, "wr-2", "acme/widgets", true, 30)
	f.openLoop(t, "wr-3", "cpave3/legato", false, 20)

	sessions := f.github.tickSessions(t, f.svc)
	if len(sessions) != 2 {
		t.Fatalf("%d sessions for two steward loops, want 2", len(sessions))
	}
	for _, s := range sessions {
		if !s.closed || len(s.watches) != 1 {
			t.Fatalf("session %+v: want one watch, closed", s)
		}
	}
	slices.SortFunc(sessions, func(a, b *fakeWatchSession) int { return strings.Compare(b.repo, a.repo) }) // cpave3 first
	if !slices.Equal(sessions[0].watches[0], []int{12, 13}) || !slices.Equal(sessions[0].comments, []int{12, 13}) {
		t.Fatalf("the stack's session read %v and comments of %v", sessions[0].watches, sessions[0].comments)
	}
	if sessions[1].repo != "acme/widgets" || !slices.Equal(sessions[1].comments, []int{30}) {
		t.Fatalf("the other loop's session %+v", sessions[1])
	}
	got, ok := f.svc.Watched(stack)
	if !ok || len(got.Layers) != 2 || got.Layers[1].HeadSHA != "b1" || got.Err != nil || !got.ReadAt.Equal(f.clock.Now()) {
		t.Fatalf("the stack's watch %+v", got)
	}
	if _, ok := f.svc.Watched(other); !ok {
		t.Fatal("the other loop was not watched")
	}
}

// Polls run every minute for an hour after a change, then every five
// minutes; a change brings the minute back (AC 38).
func TestTheWatchFollowsItsCadence(t *testing.T) {
	f := newStewardWatchFixture(t)
	f.openLoop(t, "wr-1", "cpave3/legato", true, 12)
	polls := func() int { return len(f.github.tickSessions(t, f.svc)) }

	if polls() != 1 {
		t.Fatal("a new loop is not read on its first tick")
	}
	f.clock.Advance(30 * time.Second)
	if polls() != 0 {
		t.Fatal("read again 30 s later")
	}
	f.clock.Advance(30 * time.Second)
	if polls() != 1 {
		t.Fatal("not read a minute after the last read, within the hour")
	}
	// An hour on from the loop's making with nothing changed: every five
	// minutes.
	f.clock.Advance(time.Hour)
	if polls() != 1 {
		t.Fatal("not read after an hour away")
	}
	f.clock.Advance(2 * time.Minute)
	if polls() != 0 {
		t.Fatal("read two minutes later, past the hour")
	}
	f.clock.Advance(3 * time.Minute)
	if polls() != 1 {
		t.Fatal("not read five minutes later, past the hour")
	}
	// A push: the head moved, so polls are every minute again.
	f.github.mu.Lock()
	f.github.heads[12] = "a2"
	f.github.mu.Unlock()
	f.clock.Advance(5 * time.Minute)
	if polls() != 1 {
		t.Fatal("not read five minutes later")
	}
	f.clock.Advance(time.Minute)
	if polls() != 1 {
		t.Fatal("not read a minute after a change")
	}
	f.clock.Advance(time.Minute)
	if polls() != 1 {
		t.Fatal("not read a minute apart within the hour after a change")
	}
	f.clock.Advance(59 * time.Minute)
	if polls() != 1 {
		t.Fatal("not read an hour after the change")
	}
	f.clock.Advance(time.Minute)
	if polls() != 0 {
		t.Fatal("read a minute apart past the hour after the change")
	}
}

// A layer whose mergeability GitHub has not worked out (UNKNOWN) is read
// again after a minute, even past the hour of minute polls (AC 31).
func TestAnUnknownMergeabilityIsReadAgainAfterAMinute(t *testing.T) {
	f := newStewardWatchFixture(t)
	f.github.mergeable = map[int]string{13: "UNKNOWN"}
	f.openLoop(t, "wr-1", "cpave3/legato", true, 12, 13)
	polls := func() int { return len(f.github.tickSessions(t, f.svc)) }

	if polls() != 1 {
		t.Fatal("a new loop is not read on its first tick")
	}
	f.clock.Advance(time.Hour)
	if polls() != 1 {
		t.Fatal("not read after an hour away")
	}
	f.clock.Advance(30 * time.Second)
	if polls() != 0 {
		t.Fatal("read again 30 s later")
	}
	f.clock.Advance(30 * time.Second)
	if polls() != 1 {
		t.Fatal("UNKNOWN not read again a minute later, past the hour")
	}
	f.clock.Advance(time.Minute)
	if polls() != 1 {
		t.Fatal("still UNKNOWN, not read again a minute later")
	}
}

// A PR status event brings a poll forward, but never closer than the
// gap to the last one (AC 38).
func TestAPRStatusEventBringsThePollForward(t *testing.T) {
	f := newStewardWatchFixture(t)
	bus := events.New()
	stop := f.svc.StartWatchEvents(context.Background(), bus)
	defer stop()
	f.openLoop(t, "wr-1", "cpave3/legato", true, 12)
	polls := func() int { return len(f.github.tickSessions(t, f.svc)) }

	f.clock.Advance(2 * time.Hour) // the slow cadence
	if polls() != 1 {
		t.Fatal("not read")
	}
	f.clock.Advance(10 * time.Second)
	bus.Publish(events.Event{Type: events.EventPRStatusUpdated, At: f.clock.Now()})
	deadline := time.Now().Add(2 * time.Second)
	for !f.svc.watchForwardPending() {
		if time.Now().After(deadline) {
			t.Fatal("the event did not bring the watch forward")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if polls() != 0 {
		t.Fatal("brought forward to 10 s after the last read")
	}
	f.clock.Advance(20 * time.Second)
	if polls() != 1 {
		t.Fatal("not brought forward: 30 s after the last read, at the slow cadence")
	}
	f.clock.Advance(30 * time.Second)
	if polls() != 0 {
		t.Fatal("a poll brought forward stays forward after it ran")
	}
}

// A rate limit holds the loop's polls until the limit's end, and keeps
// what was read before (LEG-564: a failed poll closes nothing).
func TestARateLimitedWatchWaitsForTheLimit(t *testing.T) {
	f := newStewardWatchFixture(t)
	loop := f.openLoop(t, "wr-1", "cpave3/legato", true, 12)
	polls := func() int { return len(f.github.tickSessions(t, f.svc)) }
	if polls() != 1 {
		t.Fatal("not read")
	}
	f.github.limit = &GitHubRateLimit{Installation: "reviewer-app@cpave3", RetryAt: f.clock.Now().Add(10 * time.Minute)}
	f.clock.Advance(time.Minute)
	if polls() != 1 {
		t.Fatal("not read a minute later")
	}
	got, _ := f.svc.Watched(loop)
	if got.Err == nil || len(got.Layers) != 1 || got.Layers[0].HeadSHA != "a1" {
		t.Fatalf("after a limit: %+v", got)
	}
	f.clock.Advance(5 * time.Minute)
	if polls() != 0 {
		t.Fatal("read during the limit")
	}
	f.github.limit = nil
	f.clock.Advance(5 * time.Minute)
	if polls() != 1 {
		t.Fatal("not read after the limit")
	}
	if got, _ := f.svc.Watched(loop); got.Err != nil {
		t.Fatalf("the error stayed: %v", got.Err)
	}
}
