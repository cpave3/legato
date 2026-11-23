package service

import (
	"context"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// What the author of a run's loop waiting for it is doing, as the loop reads
// it (LEG-543): whether the wait is the operator's or the author agent's.
const (
	// runAuthorPerson: the branch's owner, a person, is the author.
	runAuthorPerson = "person"
	// runAuthorFixing: the agent author is working or in the background.
	runAuthorFixing = "fixing"
	// runAuthorWaiting: the agent author waits on the operator.
	runAuthorWaiting = "waiting"
	// runAuthorStopped: the agent author is idle or failed with findings
	// still unanswered after it was told of them, or it ended.
	runAuthorStopped = "stopped"
	// runAuthorSettled: the agent author is idle with nothing left to
	// answer, or not yet told of the pass: the loop moves on by itself.
	runAuthorSettled = "settled"
)

// runAuthorStops remembers when each loop's agent author was first seen
// stopped, and whether the loop said so once the quiet time passed. In
// memory: after a restart the quiet time starts again.
type runAuthorStops struct {
	mu    sync.Mutex
	since map[string]time.Time
	told  map[string]bool
}

// authorState is what the author of a loop waiting for it is doing; "" when
// the loop does not wait for its author.
func (c *ReviewRunCaller) authorState(ctx context.Context, run *reviewRun, loop *store.HandoffReview) string {
	if loop == nil || loop.Phase != ReviewPhaseWaitingForAuthor {
		return ""
	}
	if run.AuthorTask == "" {
		return runAuthorPerson
	}
	session, err := c.engine.opts.Agents.GetAgentSession(ctx, run.AuthorTask)
	if err != nil || session == nil {
		return runAuthorStopped
	}
	switch session.Activity {
	case store.ActivityWorking, store.ActivityBackground:
		return runAuthorFixing
	case store.ActivityWaiting:
		return runAuthorWaiting
	}
	if loop.DeliveredPass < loop.Pass {
		return runAuthorSettled
	}
	items, err := currentItems(ctx, c.engine.db, loop)
	if err != nil {
		return runAuthorSettled
	}
	for _, item := range items {
		if item.Kind != ItemNit && item.Answer == "" {
			return runAuthorStopped
		}
	}
	return runAuthorSettled
}

// authorNeedsYou says the loop's wait for its author is the operator's: a
// person author, an agent author waiting on the operator, or one stopped
// for longer than the quiet time, which covers the gap between its turns
// and between a push and the next pass.
func (c *ReviewRunCaller) authorNeedsYou(taskID, state string) bool {
	switch state {
	case runAuthorPerson, runAuthorWaiting:
		return true
	case runAuthorStopped:
		c.stops.mu.Lock()
		defer c.stops.mu.Unlock()
		since, ok := c.stops.since[taskID]
		return ok && c.engine.opts.Now().Sub(since) >= c.engine.config().QuietTime
	}
	return false
}

// noteAuthor keeps when a loop's agent author was first seen stopped, and
// says the run changed once the quiet time has passed, so its page and the
// rail show that it needs the operator. Each tick calls it.
func (c *ReviewRunCaller) noteAuthor(ctx context.Context, taskID string, run *reviewRun) {
	loop, err := c.engine.db.GetHandoffReview(ctx, taskID)
	if err != nil {
		return
	}
	stopped := c.authorState(ctx, run, loop) == runAuthorStopped
	c.stops.mu.Lock()
	if c.stops.since == nil {
		c.stops.since, c.stops.told = map[string]time.Time{}, map[string]bool{}
	}
	if !stopped {
		delete(c.stops.since, taskID)
		delete(c.stops.told, taskID)
		c.stops.mu.Unlock()
		return
	}
	now := c.engine.opts.Now()
	since, ok := c.stops.since[taskID]
	if !ok {
		c.stops.since[taskID] = now
		c.stops.mu.Unlock()
		return
	}
	tell := !c.stops.told[taskID] && now.Sub(since) >= c.engine.config().QuietTime
	if tell {
		c.stops.told[taskID] = true
	}
	c.stops.mu.Unlock()
	if tell {
		c.Changed(taskID)
	}
}

// authorReason is why a loop's wait for its author needs the operator.
func authorReason(state string) string {
	switch state {
	case runAuthorPerson:
		return "Review waits on the branch's author"
	case runAuthorWaiting:
		return "Review waits on its author agent, which waits for you"
	}
	return "Review waits on its author agent, which stopped with findings unanswered"
}
