package service

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// A main instance's live connection to its workers: while a worker holds
// tasks away from this main (or is owed a call), the main keeps a WebSocket
// open to the worker's Events route. On every connect it first makes the
// calls it owes the worker, then asks for a full pull, then applies the
// summary rows the worker sends as they change. A drop is retried with
// backoff (1s doubling to 60s, with jitter); a ping every 15s, and two
// missed pongs drop the connection. The worker's cards are marked
// unreachable only once it has been down 30 seconds. The main never takes a
// task back by itself. See docs/ai/integrations.md § Live away state.

// What the main shows of an away task (AwayTask.View): the worker's state
// and its agent's activity, or the main's own view of the worker.
const (
	AwayViewStarting = "starting"
	AwayViewWorking  = "working"
	AwayViewNeedsYou = "needs_you"
	// AwayViewInReview: a review pass runs, waits for a reviewer, or waits
	// for the author's answers.
	AwayViewInReview = "in_review"
	AwayViewIdle     = "idle"
	AwayViewEnded    = "ended"
	// AwayViewInterrupted: a crash on the worker took the agent; it can be
	// resumed there.
	AwayViewInterrupted = "interrupted"
	AwayViewDone        = "done"
	AwayViewFailed      = "failed"
	AwayViewComingBack  = "coming_back"
	// AwayViewUnreachable: the worker has been down longer than the grace.
	AwayViewUnreachable = "unreachable"
	// AwayViewLost: the worker no longer has the task; only Take back is
	// left.
	AwayViewLost = "lost"
	// AwayViewNotAccepted: the worker no longer accepts this main's token;
	// pair again.
	AwayViewNotAccepted = "not_accepted"
)

// awayReach adds to an away task what the main itself knows: that the
// worker lost it, is unreachable or refuses this main, and the view.
func awayReach(away *AwayTask, a store.AwayTask) {
	if a.LostAt != nil {
		away.LostAt, away.Lost = *a.LostAt, a.LostReason
		if away.Lost == "" {
			away.Lost = "the worker no longer has this task"
		}
	}
	if a.WorkerUnreachableSince != nil {
		away.WorkerUnreachableSince = *a.WorkerUnreachableSince
	}
	if a.WorkerRefusedAt != nil {
		away.WorkerRefusedAt = *a.WorkerRefusedAt
	}
	away.View = awayView(*away)
}

func awayView(a AwayTask) string {
	switch {
	case a.LostAt != "":
		return AwayViewLost
	case a.WorkerRefusedAt != "":
		return AwayViewNotAccepted
	case a.WorkerUnreachableSince != "":
		return AwayViewUnreachable
	}
	switch a.State {
	case "returning":
		return AwayViewComingBack
	case store.HandoffFailed:
		return AwayViewFailed
	case store.HandoffDone:
		return AwayViewDone
	case store.HandoffPending, "":
		return AwayViewStarting
	}
	switch {
	case a.Review.Stuck():
		return AwayViewNeedsYou
	case a.Review.InReview():
		return AwayViewInReview
	}
	switch a.Activity {
	case HandoffActivityWorking:
		return AwayViewWorking
	case HandoffActivityWaiting:
		return AwayViewNeedsYou
	case HandoffActivityEnded:
		return AwayViewEnded
	case HandoffActivityInterrupted:
		return AwayViewInterrupted
	}
	return AwayViewIdle
}

// MarkUnreachable marks a worker unreachable now, unless it is marked
// already: a Handoff, Return or Recall call it did not answer marks it at
// once, without the grace a dropped connection gets.
func (s *WorkerService) MarkUnreachable(ctx context.Context, instanceID string) {
	marked, err := s.db.SetWorkerUnreachable(ctx, instanceID, s.opts.Now().UTC().Format(time.RFC3339))
	if err != nil {
		log.Printf("worker %s: marking it unreachable: %v", instanceID, err)
		return
	}
	if marked && s.opts.Bus != nil {
		s.opts.Bus.Publish(events.Event{Type: events.EventCardsRefreshed, At: time.Now()})
	}
}

// WorkerOwedCalls are the calls a main owes a worker and makes on every
// connect, before the pull: Drop for tasks taken back, Leave for a removed
// worker. Owes keeps the connection wanted while the worker holds no away
// task.
type WorkerOwedCalls interface {
	Owes(ctx context.Context, workerInstanceID string) (bool, error)
	SendOwed(ctx context.Context, client *WorkerClient) error
}

// WorkerConnectionsOptions configure a main's worker connections. Zero
// durations take the defaults the spec gives.
type WorkerConnectionsOptions struct {
	// Workers reaches the paired workers.
	Workers *WorkerService
	// Bus hears the board change as rows arrive (EventCardUpdated) and
	// workers change (EventCardsRefreshed); its EventCardsRefreshed also
	// tells the connections to look again at which workers they need.
	Bus *events.Bus
	// Now is the clock of the unreachable grace and the times recorded;
	// time.Now when nil.
	Now func() time.Time
	// MinBackoff and MaxBackoff bound the wait before a reconnect: 1s
	// doubling to 60s.
	MinBackoff time.Duration
	MaxBackoff time.Duration
	// Jitter spreads a wait; by default a random time between half the
	// wait and the wait.
	Jitter func(time.Duration) time.Duration
	// PingEvery is how often the connection is pinged (15s); two pings
	// without a pong drop it.
	PingEvery time.Duration
	// UnreachableAfter is how long a worker may be down before its cards
	// are marked unreachable (30s).
	UnreachableAfter time.Duration
	// CheckEvery is how often the grace and the workers needed are looked
	// at (1s).
	CheckEvery time.Duration
	// Owed are the calls owed to workers; nil for none.
	Owed WorkerOwedCalls
	// PRs is the main's PR tracking, which finds the PRs of the branches
	// rows bring; nil records the branches only. See away_prs.go.
	PRs PRBranchActions
}

// WorkerConnections keeps a main's live connection to each worker that
// holds its away tasks.
type WorkerConnections struct {
	db   *store.Store
	opts WorkerConnectionsOptions

	mu    sync.Mutex
	ctx   context.Context
	conns map[string]*workerConn
	kick  chan struct{}
	wg    sync.WaitGroup
	// prs tracks the branches rows bring and finds their PRs.
	prs *awayPRs
}

// workerConn is one worker's connection loop.
type workerConn struct {
	cancel    context.CancelFunc
	reconnect chan struct{}

	// reach makes a pull's "connected, then heard" and CheckReach's
	// "still down, then marked unreachable" happen one at a time, so a
	// CheckReach that saw the old drop time cannot mark the worker
	// unreachable again after the pull has heard it. It is taken before mu,
	// never while mu (or the connections' lock) is held, and is held across
	// the database write.
	reach sync.Mutex

	mu        sync.Mutex
	connected bool
	droppedAt time.Time
	session   int
	closeNow  context.CancelFunc
	waiters   []workerWaiter
}

// workerWaiter is a manual sync waiting for a pull of a session after the
// one under way when it asked.
type workerWaiter struct {
	after int
	done  chan error
}

func NewWorkerConnections(db *store.Store, opts WorkerConnectionsOptions) *WorkerConnections {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.MinBackoff <= 0 {
		opts.MinBackoff = time.Second
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = time.Minute
	}
	if opts.Jitter == nil {
		opts.Jitter = func(d time.Duration) time.Duration {
			if d <= 1 {
				return d
			}
			return d/2 + rand.N(d/2+1)
		}
	}
	if opts.PingEvery <= 0 {
		opts.PingEvery = 15 * time.Second
	}
	if opts.UnreachableAfter <= 0 {
		opts.UnreachableAfter = 30 * time.Second
	}
	if opts.CheckEvery <= 0 {
		opts.CheckEvery = time.Second
	}
	return &WorkerConnections{db: db, opts: opts, conns: map[string]*workerConn{}, kick: make(chan struct{}, 1), prs: newAwayPRs(db, opts.PRs)}
}

// SetOwedCalls gives the connections the calls a main owes its workers.
func (c *WorkerConnections) SetOwedCalls(owed WorkerOwedCalls) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.opts.Owed = owed
}

func (c *WorkerConnections) owed() WorkerOwedCalls {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.opts.Owed
}

// WorkerBackoff is the wait before reconnect attempt n (from 0), before
// jitter: min, doubling, at most max.
func WorkerBackoff(attempt int, min, max time.Duration) time.Duration {
	d := min
	for i := 0; i < attempt && d < max; i++ {
		d *= 2
	}
	if d > max {
		d = max
	}
	return d
}

// Start connects to the workers that need it, and keeps doing so as away
// tasks come and go, until stop.
func (c *WorkerConnections) Start(ctx context.Context) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.ctx = ctx
	c.mu.Unlock()
	var refreshed <-chan events.Event
	if c.opts.Bus != nil {
		refreshed = c.opts.Bus.Subscribe(events.EventCardsRefreshed)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(c.opts.CheckEvery)
		defer ticker.Stop()
		c.reconcile(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.kick:
			case _, ok := <-refreshed:
				if !ok {
					refreshed = nil
				}
			case <-ticker.C:
				c.CheckReach(ctx)
			}
			c.reconcile(ctx)
		}
	}()
	return func() {
		cancel()
		<-done
		c.wg.Wait()
		c.prs.wait()
		if c.opts.Bus != nil && refreshed != nil {
			c.opts.Bus.Unsubscribe(refreshed)
		}
	}
}

// Kick makes the connections look again at once at which workers they
// need: one that got an away task, one that has none left.
func (c *WorkerConnections) Kick() {
	select {
	case c.kick <- struct{}{}:
	default:
	}
}

// Connected says whether the main has a live connection to a worker, past
// its pull.
func (c *WorkerConnections) Connected(workerInstanceID string) bool {
	c.mu.Lock()
	wc := c.conns[workerInstanceID]
	c.mu.Unlock()
	if wc == nil {
		return false
	}
	wc.mu.Lock()
	defer wc.mu.Unlock()
	return wc.connected
}

// needed lists the workers that hold away tasks or are owed a call, a
// removed worker not yet told to leave among them.
func (c *WorkerConnections) needed(ctx context.Context) (map[string]bool, error) {
	workers, err := c.db.ListAllWorkers(ctx)
	if err != nil {
		return nil, err
	}
	away, err := c.db.ListAwayTasks(ctx)
	if err != nil {
		return nil, err
	}
	holding := map[string]bool{}
	for _, a := range away {
		holding[a.WorkerInstanceID] = true
	}
	owed := c.owed()
	out := map[string]bool{}
	for _, w := range workers {
		want := holding[w.InstanceID]
		if !want && owed != nil {
			if owes, err := owed.Owes(ctx, w.InstanceID); err == nil && owes {
				want = true
			}
		}
		out[w.InstanceID] = want
	}
	return out, nil
}

// reconcile starts a connection loop for each worker that needs one and
// stops those of workers that no longer do.
func (c *WorkerConnections) reconcile(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	needed, err := c.needed(ctx)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("worker connections: %v", err)
		}
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, wc := range c.conns {
		if !needed[id] {
			wc.cancel()
			delete(c.conns, id)
		}
	}
	for id, want := range needed {
		if !want || c.conns[id] != nil {
			continue
		}
		loopCtx, cancel := context.WithCancel(ctx)
		wc := &workerConn{cancel: cancel, reconnect: make(chan struct{}, 1)}
		c.conns[id] = wc
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.run(loopCtx, id, wc)
		}()
	}
}

// Errors a connection ends with that are not the worker being down.
var (
	errWorkerRefusesMain = errors.New("the worker no longer accepts this main; pair again")
	errWorkerIsAnother   = errors.New("the worker answers as another Legato")
	errPongsMissed       = errors.New("the worker stopped answering pings")
)

// run connects to a worker again and again, with backoff, until ctx ends.
func (c *WorkerConnections) run(ctx context.Context, id string, wc *workerConn) {
	attempt := 0
	for {
		pulled, err := c.session(ctx, id, wc)
		if ctx.Err() != nil {
			wc.answer(-1, ctx.Err())
			wc.mu.Lock()
			wc.connected = false
			wc.mu.Unlock()
			return
		}
		if pulled {
			attempt = 0
		}
		wait := WorkerBackoff(attempt, c.opts.MinBackoff, c.opts.MaxBackoff)
		attempt++
		wc.mu.Lock()
		wc.connected = false
		session := wc.session
		switch {
		case errors.Is(err, errWorkerRefusesMain), errors.Is(err, errWorkerIsAnother), errors.Is(err, ErrWorkerNotPaired):
			// It answered: it is not down. Asking again soon helps nothing.
			wc.droppedAt = time.Time{}
			wait = c.opts.MaxBackoff
		default:
			if wc.droppedAt.IsZero() {
				wc.droppedAt = c.opts.Now()
			}
		}
		wc.mu.Unlock()
		if errors.Is(err, errWorkerRefusesMain) {
			c.refused(ctx, id)
		}
		wc.answer(session, err)
		select {
		case <-ctx.Done():
			wc.answer(-1, ctx.Err())
			return
		case <-time.After(c.opts.Jitter(wait)):
		case <-wc.reconnect:
			attempt = 0
		}
	}
}

// answer tells the manual syncs waiting on a session after `after` how it
// went; -1 answers them all.
func (wc *workerConn) answer(session int, err error) {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	kept := wc.waiters[:0]
	for _, w := range wc.waiters {
		if session >= 0 && session <= w.after {
			kept = append(kept, w)
			continue
		}
		w.done <- err
	}
	wc.waiters = kept
}

// session is one connection to a worker: dial, the owed calls, the pull,
// then rows until it drops. pulled says whether the pull was applied.
func (c *WorkerConnections) session(ctx context.Context, id string, wc *workerConn) (pulled bool, err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	wc.mu.Lock()
	wc.session++
	session := wc.session
	wc.closeNow = cancel
	wc.mu.Unlock()

	client, err := c.opts.Workers.owedClient(ctx, id)
	if err != nil {
		return false, err
	}
	if client.Worker.Leaving() {
		// A removed worker holds nothing of this main's to follow: it is
		// only sent the calls owed it, the last of which is Leave.
		if owed := c.owed(); owed != nil {
			if err := owed.SendOwed(ctx, client); err != nil {
				return false, err
			}
		}
		return false, fmt.Errorf("%w: %s was removed", ErrWorkerNotPaired, client.Worker.Name)
	}
	conn, err := client.dialEvents(ctx)
	if err != nil {
		return false, err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(16 << 20)

	if owed := c.owed(); owed != nil {
		if err := owed.SendOwed(ctx, client); err != nil {
			return false, classifyWorkerError(err)
		}
	}
	// The away tasks the pull must list: those away before it was asked
	// for. A handoff answered after the pull is taken is not in it.
	before, err := c.awayOn(ctx, id)
	if err != nil {
		return false, err
	}
	if err := wsjson.Write(ctx, conn, HandoffStreamMessage{Type: HandoffStreamPull}); err != nil {
		return false, fmt.Errorf("%w: %v", ErrWorkerUnreachable, err)
	}

	messages := make(chan HandoffStreamMessage)
	readErr := make(chan error, 1)
	go func() {
		for {
			var m HandoffStreamMessage
			if err := wsjson.Read(ctx, conn, &m); err != nil {
				readErr <- err
				return
			}
			select {
			case messages <- m:
			case <-ctx.Done():
				return
			}
		}
	}()
	dead := make(chan struct{})
	go c.ping(ctx, id, conn, dead)

	worker := client.Worker
	for {
		select {
		case <-ctx.Done():
			return pulled, ctx.Err()
		case <-dead:
			return pulled, fmt.Errorf("%w: %w", ErrWorkerUnreachable, errPongsMissed)
		case err := <-readErr:
			if websocket.CloseStatus(err) == HandoffStreamRefused {
				return pulled, errWorkerRefusesMain
			}
			return pulled, fmt.Errorf("%w: %v", ErrWorkerUnreachable, err)
		case m := <-messages:
			if m.WorkerInstanceID != worker.InstanceID {
				c.lostAll(ctx, worker, fmt.Sprintf("%s now answers as another Legato (%s), not %s", worker.Name, orUnknown(m.WorkerInstanceID), worker.InstanceID))
				return pulled, errWorkerIsAnother
			}
			switch m.Type {
			case HandoffStreamPull:
				c.applyPull(ctx, worker, before, m.Handoffs)
				// Connected before heard clears unreachable_since, under
				// reach: a CheckReach between the two would otherwise mark it
				// unreachable again, from the old outage's drop time.
				wc.reach.Lock()
				wc.mu.Lock()
				wc.connected, wc.droppedAt = true, time.Time{}
				wc.mu.Unlock()
				c.heard(ctx, id)
				wc.reach.Unlock()
				pulled = true
				wc.answer(session, nil)
			case HandoffStreamRow:
				if m.Row != nil {
					c.applyRow(ctx, worker, *m.Row)
					c.heard(ctx, id)
				}
			case HandoffStreamGone:
				c.lost(ctx, worker, m.HandoffID, fmt.Sprintf("%s no longer has this task", worker.Name))
			}
		}
	}
}

// ping pings the connection every PingEvery; two pings in a row without a
// pong close dead.
func (c *WorkerConnections) ping(ctx context.Context, id string, conn *websocket.Conn, dead chan<- struct{}) {
	missed := 0
	for {
		if missed == 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(c.opts.PingEvery):
			}
		}
		pingCtx, cancel := context.WithTimeout(ctx, c.opts.PingEvery)
		err := conn.Ping(pingCtx)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			missed++
			if missed >= 2 {
				close(dead)
				return
			}
			continue
		}
		missed = 0
		c.heard(ctx, id)
	}
}

// awayOn lists the tasks away on a worker.
func (c *WorkerConnections) awayOn(ctx context.Context, id string) ([]store.AwayTask, error) {
	all, err := c.db.ListAwayTasks(ctx)
	if err != nil {
		return nil, err
	}
	var out []store.AwayTask
	for _, a := range all {
		if a.WorkerInstanceID == id {
			out = append(out, a)
		}
	}
	return out, nil
}

// applyPull applies a pull's rows, and marks lost each task that was away
// on the worker before the pull and is not in it.
func (c *WorkerConnections) applyPull(ctx context.Context, worker Worker, before []store.AwayTask, rows []HandoffSummary) {
	listed := map[string]bool{}
	for _, r := range rows {
		listed[r.HandoffID] = true
		c.applyRow(ctx, worker, r)
	}
	for _, a := range before {
		if !listed[a.HandoffID] {
			c.lost(ctx, worker, a.HandoffID, fmt.Sprintf("%s no longer has this task", worker.Name))
		}
	}
}

// applyRow keeps a row as its away task's last summary, when the task is
// this main's and away on that worker, and tells the board.
func (c *WorkerConnections) applyRow(ctx context.Context, worker Worker, row HandoffSummary) {
	a, err := c.db.GetAwayTask(ctx, row.HandoffID)
	if err != nil || a.ClosedAt != nil || a.WorkerInstanceID != worker.InstanceID {
		return
	}
	// The branches it pushed: tracked here, their PRs found off this loop.
	c.prs.track(ctx, *a, row.Branches)
	data, err := json.Marshal(row)
	if err != nil {
		return
	}
	if a.LastSummary != nil && *a.LastSummary == string(data) && a.LostAt == nil {
		return
	}
	if err := c.db.SetAwayTaskSummary(ctx, row.HandoffID, string(data), c.opts.Now().UTC().Format(time.RFC3339)); err != nil {
		log.Printf("worker %s: keeping handoff %s's row: %v", worker.Name, row.HandoffID, err)
		return
	}
	c.publish(events.EventCardUpdated)
}

// lost marks an away task lost on its worker. It stays away: only the
// person takes it back.
func (c *WorkerConnections) lost(ctx context.Context, worker Worker, handoffID, reason string) {
	a, err := c.db.GetAwayTask(ctx, handoffID)
	if err != nil || a.WorkerInstanceID != worker.InstanceID {
		return
	}
	marked, err := c.db.SetAwayTaskLost(ctx, handoffID, reason, c.opts.Now().UTC().Format(time.RFC3339))
	if err != nil {
		log.Printf("worker %s: marking handoff %s lost: %v", worker.Name, handoffID, err)
		return
	}
	if marked {
		c.publish(events.EventCardUpdated)
	}
}

// lostAll marks every task away on a worker lost.
func (c *WorkerConnections) lostAll(ctx context.Context, worker Worker, reason string) {
	away, err := c.awayOn(ctx, worker.InstanceID)
	if err != nil {
		return
	}
	for _, a := range away {
		c.lost(ctx, worker, a.HandoffID, reason)
	}
}

// heard records that the worker answered: its last pull is now, and it is
// neither unreachable nor refusing this main.
func (c *WorkerConnections) heard(ctx context.Context, id string) {
	before, err := c.db.GetWorker(ctx, id)
	if err != nil {
		return
	}
	if err := c.db.SetWorkerHeard(ctx, id, c.opts.Now().UTC().Format(time.RFC3339)); err != nil {
		return
	}
	if before.UnreachableSince != nil || before.RefusedAt != nil {
		c.publish(events.EventCardsRefreshed)
	}
}

// refused records that the worker no longer accepts this main's token.
func (c *WorkerConnections) refused(ctx context.Context, id string) {
	if marked, err := c.db.SetWorkerRefused(ctx, id, c.opts.Now().UTC().Format(time.RFC3339)); err == nil && marked {
		c.publish(events.EventCardsRefreshed)
	}
}

// CheckReach marks unreachable each worker whose connection has been down
// longer than the grace, since the time it dropped.
func (c *WorkerConnections) CheckReach(ctx context.Context) {
	now := c.opts.Now()
	c.mu.Lock()
	down := map[string]*workerConn{}
	for id, wc := range c.conns {
		wc.mu.Lock()
		if !wc.droppedAt.IsZero() && now.Sub(wc.droppedAt) >= c.opts.UnreachableAfter {
			down[id] = wc
		}
		wc.mu.Unlock()
	}
	c.mu.Unlock()
	for id, wc := range down {
		c.markUnreachable(ctx, id, wc, now)
	}
}

// markUnreachable marks a worker unreachable since its connection dropped,
// if it is still down: a pull since CheckReach looked has heard it.
func (c *WorkerConnections) markUnreachable(ctx context.Context, id string, wc *workerConn, now time.Time) {
	wc.reach.Lock()
	defer wc.reach.Unlock()
	wc.mu.Lock()
	since := wc.droppedAt
	wc.mu.Unlock()
	if since.IsZero() || now.Sub(since) < c.opts.UnreachableAfter {
		return
	}
	marked, err := c.db.SetWorkerUnreachable(ctx, id, since.UTC().Format(time.RFC3339))
	if err != nil {
		log.Printf("worker %s: marking it unreachable: %v", id, err)
		return
	}
	if marked {
		c.publish(events.EventCardsRefreshed)
	}
}

// SyncWorkers is a manual sync: every worker with a connection reconnects
// and pulls, and every other one is pulled once. It waits for the pulls,
// and names each worker it could not pull.
func (c *WorkerConnections) SyncWorkers(ctx context.Context) error {
	c.mu.Lock()
	base := c.ctx
	c.mu.Unlock()
	if base != nil {
		c.reconcile(base)
	}
	workers, err := c.db.ListWorkers(ctx)
	if err != nil {
		return err
	}
	errs := make([]error, len(workers))
	var wg sync.WaitGroup
	for i, w := range workers {
		c.mu.Lock()
		wc := c.conns[w.InstanceID]
		c.mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			var err error
			if wc != nil {
				err = wc.resync(ctx, c.opts.Workers.opts.Timeout+5*time.Second)
			} else {
				err = c.pullOnce(ctx, workerFromRow(w))
			}
			if err != nil {
				errs[i] = fmt.Errorf("%s: %w", w.Name, err)
			}
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

// resync drops the connection and reconnects at once, and waits for the
// new connection's pull.
func (wc *workerConn) resync(ctx context.Context, timeout time.Duration) error {
	done := make(chan error, 1)
	wc.mu.Lock()
	wc.waiters = append(wc.waiters, workerWaiter{after: wc.session, done: done})
	if wc.closeNow != nil {
		wc.closeNow()
	}
	wc.mu.Unlock()
	select {
	case wc.reconnect <- struct{}{}:
	default:
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(timeout):
		return fmt.Errorf("%w: no pull within %s", ErrWorkerUnreachable, timeout)
	}
}

// pullOnce pulls a worker the main keeps no connection to.
func (c *WorkerConnections) pullOnce(ctx context.Context, worker Worker) error {
	client, err := c.opts.Workers.Client(ctx, worker.InstanceID)
	if err != nil {
		return err
	}
	before, err := c.awayOn(ctx, worker.InstanceID)
	if err != nil {
		return err
	}
	var pull HandoffPull
	if err := client.Call(ctx, http.MethodGet, HandoffPullPath, nil, &pull); err != nil {
		err = classifyWorkerError(err)
		if errors.Is(err, errWorkerRefusesMain) {
			c.refused(ctx, worker.InstanceID)
		}
		return err
	}
	if pull.WorkerInstanceID != worker.InstanceID {
		c.lostAll(ctx, worker, fmt.Sprintf("%s now answers as another Legato (%s), not %s", worker.Name, orUnknown(pull.WorkerInstanceID), worker.InstanceID))
		return errWorkerIsAnother
	}
	c.applyPull(ctx, worker, before, pull.Handoffs)
	c.heard(ctx, worker.InstanceID)
	return nil
}

// classifyWorkerError tells a worker that refuses this main's token from
// one that is down or refused something else.
func classifyWorkerError(err error) error {
	var refusal *WorkerRefusal
	if errors.As(err, &refusal) && refusal.Status == http.StatusUnauthorized {
		return errWorkerRefusesMain
	}
	return err
}

func (c *WorkerConnections) publish(t events.EventType) {
	if c.opts.Bus != nil {
		c.opts.Bus.Publish(events.Event{Type: t, At: time.Now()})
	}
}

// HandoffStreamRefused is the close status a worker ends the Events
// connection with once it no longer accepts the main's token.
const HandoffStreamRefused = websocket.StatusCode(4401)

// dialEvents opens the worker's Events connection with this main's token,
// over HTTP/1.1 (a WebSocket upgrade needs it) and the worker's
// certificate.
func (c *WorkerClient) dialEvents(ctx context.Context) (*websocket.Conn, error) {
	address := c.base
	switch {
	case strings.HasPrefix(address, "https://"):
		address = "wss://" + strings.TrimPrefix(address, "https://")
	case strings.HasPrefix(address, "http://"):
		address = "ws://" + strings.TrimPrefix(address, "http://")
	}
	client := &http.Client{Timeout: c.http.Timeout}
	if t, ok := c.http.Transport.(*http.Transport); ok {
		t = t.Clone()
		t.ForceAttemptHTTP2 = false
		t.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
		if t.TLSClientConfig != nil {
			t.TLSClientConfig.NextProtos = []string{"http/1.1"}
		}
		client.Transport = t
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.token)
	conn, resp, err := websocket.Dial(ctx, address+HandoffEventsPath, &websocket.DialOptions{HTTPClient: client, HTTPHeader: header})
	if err == nil {
		return conn, nil
	}
	if resp != nil {
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return nil, errWorkerRefusesMain
		case http.StatusNotFound:
			return nil, fmt.Errorf("%w: it has no Events route", ErrWorkerTooOld)
		}
		if resp.StatusCode != http.StatusSwitchingProtocols {
			return nil, &WorkerRefusal{Message: resp.Status, Status: resp.StatusCode}
		}
	}
	return nil, fmt.Errorf("%w: %v", ErrWorkerUnreachable, err)
}
