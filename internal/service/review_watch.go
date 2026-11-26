package service

import (
	"context"
	"errors"
	"log"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// ReviewWatchCadence is how often the steward reads a loop's pull requests
// (spec LEG-559 C7): every Fast for FastFor after a change, then every Slow.
// A PR status event brings a poll forward, but no closer than ForwardGap to
// the last one.
type ReviewWatchCadence struct {
	Fast, FastFor, Slow, ForwardGap time.Duration
}

// DefaultReviewWatchCadence is C7's: a minute for an hour after a change,
// then five minutes. The gap matches the pull request waits' (prWaitEventGap).
var DefaultReviewWatchCadence = ReviewWatchCadence{Fast: time.Minute, FastFor: time.Hour, Slow: 5 * time.Minute, ForwardGap: prWaitEventGap}

// ReviewWatchSchedule says which loops' watches are due, keyed by loop ID.
// In memory: a restart reads every loop on its first tick, and takes a
// loop's making for its last change until it sees one.
type ReviewWatchSchedule struct {
	cadence ReviewWatchCadence
	mu      sync.Mutex
	loops   map[string]*watchSlot
}

type watchSlot struct {
	polledAt, changedAt, notBefore time.Time
	// recheckAt is when a read that found something still unsettled
	// (Recheck) is due again, whatever the cadence.
	recheckAt time.Time
	forward   bool
}

// ReviewWatchUnknownRecheck is how soon a layer whose mergeability GitHub
// has not worked out yet (UNKNOWN) is read again (spec LEG-559 § The
// steward).
const ReviewWatchUnknownRecheck = time.Minute

// NewReviewWatchSchedule makes a schedule; a zero field of cadence is the
// default's.
func NewReviewWatchSchedule(cadence ReviewWatchCadence) *ReviewWatchSchedule {
	d := DefaultReviewWatchCadence
	if cadence.Fast <= 0 {
		cadence.Fast = d.Fast
	}
	if cadence.FastFor <= 0 {
		cadence.FastFor = d.FastFor
	}
	if cadence.Slow <= 0 {
		cadence.Slow = d.Slow
	}
	if cadence.ForwardGap <= 0 {
		cadence.ForwardGap = d.ForwardGap
	}
	return &ReviewWatchSchedule{cadence: cadence, loops: map[string]*watchSlot{}}
}

func (s *ReviewWatchSchedule) slot(loopID string) *watchSlot {
	slot, ok := s.loops[loopID]
	if !ok {
		slot = &watchSlot{}
		s.loops[loopID] = slot
	}
	return slot
}

// Due says whether a loop's watch runs now. changedAt is the last change
// the caller knows of (the loop's making, a push it saw); the schedule
// keeps the changes its own reads found, and takes the later.
func (s *ReviewWatchSchedule) Due(loopID string, changedAt, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	slot := s.slot(loopID)
	if now.Before(slot.notBefore) {
		return false
	}
	if slot.polledAt.IsZero() {
		return true
	}
	since := now.Sub(slot.polledAt)
	if slot.forward && since >= s.cadence.ForwardGap {
		return true
	}
	if !slot.recheckAt.IsZero() && !now.Before(slot.recheckAt) {
		return true
	}
	last := changedAt
	if slot.changedAt.After(last) {
		last = slot.changedAt
	}
	interval := s.cadence.Slow
	if now.Sub(last) < s.cadence.FastFor {
		interval = s.cadence.Fast
	}
	return since >= interval
}

// Polled records a loop's watch at now, and whether it found a change.
func (s *ReviewWatchSchedule) Polled(loopID string, now time.Time, changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	slot := s.slot(loopID)
	slot.polledAt, slot.forward, slot.recheckAt = now, false, time.Time{}
	if changed {
		slot.changedAt = now
	}
}

// Changed records a change of a loop the caller saw itself.
func (s *ReviewWatchSchedule) Changed(loopID string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slot := s.slot(loopID); at.After(slot.changedAt) {
		slot.changedAt = at
	}
}

// Recheck makes a loop's next watch due at at, sooner than its cadence
// would: a read found something GitHub had not settled. Until the next
// Polled.
func (s *ReviewWatchSchedule) Recheck(loopID string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.slot(loopID).recheckAt = at
}

// Limited holds a loop's watches until a rate limit's end.
func (s *ReviewWatchSchedule) Limited(loopID string, retryAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.slot(loopID).notBefore = retryAt
}

// BringForward brings every loop's next watch forward (EventPRStatusUpdated
// names no pull request).
func (s *ReviewWatchSchedule) BringForward() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, slot := range s.loops {
		slot.forward = true
	}
}

// forwardPending says whether a loop's watch was brought forward and has
// not run since.
func (s *ReviewWatchSchedule) forwardPending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, slot := range s.loops {
		if slot.forward {
			return true
		}
	}
	return false
}

// Forget forgets a loop's watches: its next one is due at once.
func (s *ReviewWatchSchedule) Forget(loopID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.loops, loopID)
}

// Keep forgets every loop but those named.
func (s *ReviewWatchSchedule) Keep(ids map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.loops {
		if !ids[id] {
			delete(s.loops, id)
		}
	}
}

// ReviewLoopWatch is a loop's last watch: its layers, in stack order, and
// the comments of its open ones.
type ReviewLoopWatch struct {
	LoopID string
	Repo   string
	// ReadAt is when the layers were last read.
	ReadAt time.Time
	Layers []PRWatch
	// Comments are each open layer's comments, by number, as last read.
	Comments map[int]CommentPoll
	// Stacked are the open pull requests onto the top open layer's branch
	// that are not the loop's: layers stacked since it opened (a loop of
	// scope stack only).
	Stacked []ReviewGitHubPR
	// Changed says the last read found a change since the one before.
	Changed bool
	// Err is the last read's error, nil once one succeeds; the layers and
	// comments read before it are kept (a failed poll closes nothing).
	Err error
}

// watchPages keeps the watch's comment pages, apart from the comment
// poll's, in the loop record's poll pages.
type watchPages struct {
	db     *store.Store
	loopID string
	pr     int
}

func (p watchPages) PollPage(ctx context.Context, list string, page int) (string, string, bool) {
	return p.db.ReviewLoopPollPage(ctx, p.loopID, p.pr, "watch/"+list, page)
}

func (p watchPages) SavePollPage(ctx context.Context, list string, page int, etag, body string) error {
	return p.db.SaveReviewLoopPollPage(ctx, p.loopID, p.pr, "watch/"+list, page, etag, body)
}

// Watched is a loop's last watch, false when it has none.
func (s *ReviewPassService) Watched(loopID string) (ReviewLoopWatch, bool) {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	w, ok := s.watched[loopID]
	if !ok {
		return ReviewLoopWatch{}, false
	}
	w.Layers = slices.Clone(w.Layers)
	return w, true
}

// BringWatchesForward brings every loop's next watch forward.
func (s *ReviewPassService) BringWatchesForward() { s.watchSchedule.BringForward() }

func (s *ReviewPassService) watchForwardPending() bool { return s.watchSchedule.forwardPending() }

// StartWatchEvents brings the watches forward on each EventPRStatusUpdated
// until ctx ends or the returned function is called.
func (s *ReviewPassService) StartWatchEvents(ctx context.Context, bus *events.Bus) func() {
	if bus == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	updated := bus.Subscribe(events.EventPRStatusUpdated)
	go func() {
		defer bus.Unsubscribe(updated)
		for {
			select {
			case <-ctx.Done():
				return
			case <-updated:
				s.BringWatchesForward()
			}
		}
	}()
	return cancel
}

// watchLoops reads, as the reviewer App, the pull requests and comments of
// every open steward loop whose watch is due: one session (one token) per
// loop per tick. The steward routes what it read (routeWatch).
func (s *ReviewPassService) watchLoops(ctx context.Context) {
	github, ok := s.opts.GitHub.(ReviewWatchGitHub)
	if !ok || github == nil {
		return
	}
	s.watchRun.Lock()
	defer s.watchRun.Unlock()
	loops, err := s.db.ListOpenReviewLoops(ctx)
	if err != nil {
		log.Printf("review passes: the loops to watch: %v", err)
		return
	}
	keep := map[string]bool{}
	for _, l := range loops {
		if !l.Steward {
			continue
		}
		keep[l.ID] = true
		made, _ := time.Parse(time.RFC3339, l.CreatedAt)
		if !s.watchSchedule.Due(l.ID, made, s.opts.Now()) {
			continue
		}
		s.watchLoop(ctx, github, l)
	}
	s.watchSchedule.Keep(keep)
	s.watchMu.Lock()
	for id := range s.watched {
		if !keep[id] {
			delete(s.watched, id)
		}
	}
	for id := range s.watchRouted {
		if !keep[id] {
			delete(s.watchRouted, id)
		}
	}
	s.watchMu.Unlock()
}

// watchLoop reads one loop with one session.
func (s *ReviewPassService) watchLoop(ctx context.Context, github ReviewWatchGitHub, l store.ReviewLoop) {
	now := s.opts.Now()
	prs, err := s.db.ListReviewLoopPRs(ctx, l.ID)
	if err != nil || len(prs) == 0 {
		return
	}
	numbers := make([]int, 0, len(prs))
	for _, pr := range prs {
		numbers = append(numbers, pr.Number)
	}
	s.watchMu.Lock()
	prev, seen := s.watched[l.ID]
	s.watchMu.Unlock()
	next := prev
	next.LoopID, next.Repo, next.Changed = l.ID, l.Repo, false
	active := false

	session := github.WatchSession(l.Repo)
	defer session.Close()
	err = func() error {
		layers, err := session.Watch(ctx, numbers)
		if err != nil {
			return err
		}
		comments := map[int]CommentPoll{}
		commentsChanged := false
		for _, layer := range layers {
			if layer.State != "open" {
				continue
			}
			poll, err := session.PollComments(ctx, layer.Number, watchPages{db: s.db, loopID: l.ID, pr: layer.Number})
			if err != nil {
				return err
			}
			comments[layer.Number] = poll
			commentsChanged = commentsChanged || poll.Changed
		}
		var stacked []ReviewGitHubPR
		if l.Scope == ReviewScopeStack {
			if stacked, err = readStacked(ctx, session, numbers, layers); err != nil {
				return err
			}
		}
		stackedChanged := !sameStacked(prev.Stacked, stacked)
		next.Changed = seen && !prev.ReadAt.IsZero() && (commentsChanged || !sameWatch(prev.Layers, layers) || stackedChanged)
		// Activity is what the steward's idle end counts: a layer or a
		// comment that differs, which a list's new ETag alone is not, or
		// a layer stacked on the top.
		active = seen && !prev.ReadAt.IsZero() && (!sameWatch(prev.Layers, layers) || !sameComments(prev.Comments, comments) || stackedChanged)
		next.Layers, next.Comments, next.Stacked, next.ReadAt = layers, comments, stacked, now
		return nil
	}()
	next.Err = err
	if err == nil {
		// The record keeps each layer as GitHub reported it, for the run
		// page of any process.
		if err := s.db.SetReviewLoopPRStatus(ctx, l.ID, layerStatuses(next.Layers)); err != nil {
			log.Printf("review passes: %s: keeping the layers read: %v", l.ID, err)
		} else if !seen || !sameWatch(prev.Layers, next.Layers) {
			// What the run page shows of the layers changed: an open page
			// reads it again (a held steward routes nothing that would).
			s.changed(l.HostTask)
		}
	}
	if err != nil {
		var limit *GitHubRateLimit
		if errors.As(err, &limit) {
			s.watchSchedule.Limited(l.ID, limit.RetryAt)
		}
		if prev.Err == nil || prev.Err.Error() != err.Error() {
			log.Printf("review passes: %s: watching %s: %v", l.ID, l.Repo, err)
		}
	}
	s.watchSchedule.Polled(l.ID, now, next.Changed)
	if err == nil && mergeabilityUnknown(next.Layers) {
		s.watchSchedule.Recheck(l.ID, now.Add(ReviewWatchUnknownRecheck))
	}
	s.watchMu.Lock()
	if s.watched == nil {
		s.watched = map[string]ReviewLoopWatch{}
	}
	s.watched[l.ID] = next
	s.watchMu.Unlock()
	if active {
		// A push, a comment, a review or a check change: the steward is
		// not idle (held too, which routes nothing).
		s.stillActive(ctx, l.ID)
	}
}

// sameComments says whether two reads of a loop's comments found the
// same comments on each layer.
func sameComments(a, b map[int]CommentPoll) bool {
	if len(a) != len(b) {
		return false
	}
	for number, x := range a {
		y, ok := b[number]
		if !ok || !reflect.DeepEqual(x.Comments, y.Comments) {
			return false
		}
	}
	return true
}

// sameStacked says whether two reads found the same pull requests stacked
// on the top layer, at the same heads: a layer that is still there (one a
// held loop has not taken, or one not the author's) is no change.
func sameStacked(a, b []ReviewGitHubPR) bool {
	if len(a) != len(b) {
		return false
	}
	heads := map[int]string{}
	for _, pr := range a {
		heads[pr.Number] = pr.HeadSHA
	}
	for _, pr := range b {
		if head, ok := heads[pr.Number]; !ok || head != pr.HeadSHA {
			return false
		}
	}
	return true
}

// readStacked lists the open pull requests onto the top open layer's
// branch that are not the loop's own layers.
func readStacked(ctx context.Context, session ReviewWatchSession, numbers []int, layers []PRWatch) ([]ReviewGitHubPR, error) {
	top := ""
	for _, layer := range layers {
		if layer.State == "open" {
			top = layer.HeadBranch
		}
	}
	if top == "" {
		return nil, nil
	}
	pulls, err := session.Stacked(ctx, top)
	if err != nil {
		return nil, err
	}
	var out []ReviewGitHubPR
	for _, pr := range pulls {
		if !slices.Contains(numbers, pr.Number) {
			out = append(out, pr)
		}
	}
	slices.SortFunc(out, func(a, b ReviewGitHubPR) int { return a.Number - b.Number })
	return out, nil
}

// layerStatuses are a watch read's layers as the loop record keeps them.
func layerStatuses(layers []PRWatch) []store.ReviewLoopPRStatus {
	out := make([]store.ReviewLoopPRStatus, 0, len(layers))
	for _, layer := range layers {
		out = append(out, store.ReviewLoopPRStatus{Number: layer.Number, State: layer.State, Draft: layer.Draft, Mergeable: layerMergeability(layer)})
	}
	return out
}

// layerMergeability is one word for whether a layer can merge, as the
// steward routes it: GitHub's CONFLICTING, UNKNOWN (while it works it out)
// or MERGEABLE, and BEHIND when the merge state says the base requires an
// up-to-date branch.
func layerMergeability(layer PRWatch) string {
	switch {
	case layer.Mergeable == githubConflicting:
		return githubConflicting
	case mergeUnknown(layer):
		return githubMergeUnknown
	case layer.MergeState == githubMergeBehind:
		return githubMergeBehind
	}
	return layer.Mergeable
}

// mergeabilityUnknown says whether GitHub has yet to work out an open
// layer's mergeability.
func mergeabilityUnknown(layers []PRWatch) bool {
	for _, layer := range layers {
		if layer.State == "open" && mergeUnknown(layer) {
			return true
		}
	}
	return false
}

// mergeUnknown says whether GitHub is still working out whether a layer
// can merge.
func mergeUnknown(layer PRWatch) bool {
	return layer.Mergeable == githubMergeUnknown || (layer.Mergeable == "" && layer.MergeState == githubMergeUnknown)
}

// sameWatch says whether two reads of a loop's layers found the same:
// heads, states, merge state, review decision and checks. When GitHub
// last touched them is not a change on its own (a comment is one only
// when the comment read says so).
func sameWatch(a, b []PRWatch) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		x.UpdatedAt, y.UpdatedAt = time.Time{}, time.Time{}
		if !reflect.DeepEqual(x, y) {
			return false
		}
	}
	return true
}
