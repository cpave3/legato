package service

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/stackgraph"
	"github.com/cpave3/legato/internal/engine/store"
)

// A worker's Pull and Events: how a main instance hears of the tasks it
// handed to this worker. Pull lists a summary row for each of the main's
// unfinished handoffs; Events is a WebSocket the main opens, on which the
// worker sends the same pull when the main asks for it, then a row each time
// a handoff's state, its agent's activity, its column, title or pushed
// branches change. See docs/ai/integrations.md § Live away state.

// The worker's routes for Pull and Events.
const (
	HandoffPullPath   = "/api/integrations/handoff/pull"
	HandoffEventsPath = "/api/integrations/handoff/events"
)

// A handed-off agent's activity in a summary row.
const (
	HandoffActivityWorking = "working"
	// HandoffActivityWaiting: the agent waits on the person (a question,
	// an approval); the main shows it as needs you.
	HandoffActivityWaiting = "waiting"
	HandoffActivityIdle    = "idle"
	// HandoffActivityEnded: no agent runs on the card.
	HandoffActivityEnded = "ended"
	// HandoffActivityInterrupted: a crash on the worker took the card's
	// agent; it can be resumed there.
	HandoffActivityInterrupted = "interrupted"
)

// HandoffPull is the answer to Pull: the worker's instance ID, which the
// main checks, and a summary row for each of the main's handoffs not yet
// closed (pending, in progress, done, failed or returning), oldest first.
type HandoffPull struct {
	WorkerInstanceID string           `json:"worker_instance_id"`
	Handoffs         []HandoffSummary `json:"handoffs"`
}

// Messages on the Events connection. The main sends {"type":"pull"} once it
// has made the calls it owes the worker; the worker answers with a pull,
// then sends rows, and gone for a handoff it no longer has (its card was
// deleted, or the handoff closed). Every message from the worker carries its
// instance ID.
const (
	HandoffStreamPull = "pull"
	HandoffStreamRow  = "row"
	HandoffStreamGone = "gone"
)

// HandoffStreamMessage is one message on the Events connection.
type HandoffStreamMessage struct {
	Type             string `json:"type"`
	WorkerInstanceID string `json:"worker_instance_id,omitempty"`
	// Handoffs is a pull's rows.
	Handoffs []HandoffSummary `json:"handoffs,omitempty"`
	// Row is a row's summary.
	Row *HandoffSummary `json:"row,omitempty"`
	// HandoffID is the handoff a gone message is about.
	HandoffID string `json:"handoff_id,omitempty"`
}

// handoffActivityDebounce is the least time between two rows of one
// handoff that differ only in their agent's activity.
const handoffActivityDebounce = time.Second

// Pull lists a summary row for each handoff of a main not yet closed.
func (s *HandoffWorkerService) Pull(ctx context.Context, mainInstanceID string) (HandoffPull, error) {
	id, err := InstanceID(ctx, s.db)
	if err != nil {
		return HandoffPull{}, err
	}
	rows, err := s.summaries(ctx, mainInstanceID)
	if err != nil {
		return HandoffPull{}, err
	}
	return HandoffPull{WorkerInstanceID: id, Handoffs: rows}, nil
}

func (s *HandoffWorkerService) summaries(ctx context.Context, mainInstanceID string) ([]HandoffSummary, error) {
	open, err := s.db.ListOpenHandoffs(ctx)
	if err != nil {
		return nil, err
	}
	rows := []HandoffSummary{}
	for _, h := range open {
		if h.MainInstanceID != mainInstanceID {
			continue
		}
		rows = append(rows, s.summary(ctx, h))
	}
	return rows, nil
}

// summary is a handoff's row: its state, its card's column and title, its
// agent's activity, the branches pushed for it and the path that opens the
// card on this worker's web UI.
func (s *HandoffWorkerService) summary(ctx context.Context, h store.Handoff) HandoffSummary {
	row := HandoffSummary{HandoffID: h.HandoffID, WorkerTaskID: h.TaskID, State: h.State, Failure: h.Failure,
		URL: "/board?highlight_task=" + url.QueryEscape(h.TaskID)}
	task, err := s.db.GetTask(ctx, h.TaskID)
	if err == nil {
		row.Title, row.Column = task.Title, task.Status
	}
	if h.State != store.HandoffPending {
		row.Activity = HandoffActivityEnded
		if _, err := s.db.GetInterruptedAgentSession(ctx, h.TaskID); err == nil {
			row.Activity = HandoffActivityInterrupted
		}
		if session, err := s.db.GetAgentSessionByTaskID(ctx, h.TaskID); err == nil {
			switch session.Activity {
			case store.ActivityWorking:
				row.Activity = HandoffActivityWorking
			case store.ActivityWaiting:
				row.Activity = HandoffActivityWaiting
			default:
				row.Activity = HandoffActivityIdle
			}
		}
	}
	if task != nil {
		row.Branches = s.pushedBranches(ctx, h, task)
	}
	if s.opts.Reviews != nil {
		row.Review = s.opts.Reviews.ReviewBlock(ctx, h.TaskID)
	}
	return row
}

// pushedBranches are the branches of a handoff that its clone's origin has,
// in stack order: the clone's Staccato stack when it keeps one, else the
// handoff's branch. A clone is one of the task's job clones, or the folder
// its agent works in. Once no clone is left, the stacks Legato recorded
// for the task stand in, their pushed branches only. Nothing runs in a
// clone: git's files are read (stackgraph).
func (s *HandoffWorkerService) pushedBranches(ctx context.Context, h store.Handoff, task *store.Task) []string {
	var dirs []string
	if clones, err := s.db.ListLiveTaskJobClones(ctx, h.TaskID); err == nil {
		for _, c := range clones {
			if c.Repo == "" || strings.EqualFold(c.Repo, h.Repo) {
				dirs = append(dirs, c.Path)
			}
		}
	}
	if cc, err := store.ParseCodeContext(task.CodeContext); err == nil && cc != nil && cc.WorkingDir != "" && !slices.Contains(dirs, cc.WorkingDir) {
		dirs = append(dirs, cc.WorkingDir)
	}
	var out []string
	add := func(names ...string) {
		for _, n := range names {
			if !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
	}
	for _, dir := range dirs {
		names := []string{h.Branch}
		if st, err := stackgraph.Read(dir); err == nil {
			names = names[:0]
			for _, b := range st.Branches {
				names = append(names, b.Name)
			}
		} else if !errors.Is(err, stackgraph.ErrNoStack) {
			continue
		}
		pushed, err := stackgraph.OnRemote(dir, names...)
		if err != nil {
			continue
		}
		add(pushed...)
	}
	if len(dirs) == 0 {
		if stacks, err := s.db.ListTaskStacks(ctx, h.TaskID); err == nil {
			for _, st := range stacks {
				var branches []TaskStackBranch
				if json.Unmarshal([]byte(st.Branches), &branches) != nil {
					continue
				}
				for _, b := range branches {
					if b.State == string(stackgraph.Pushed) {
						add(b.Name)
					}
				}
			}
		}
	}
	return out
}

// Stream sends a main's Events: first a pull, then a row each time one of
// its handoffs changes, until ctx ends or send fails. Rows that differ from
// the last one sent only in the agent's activity go at most once a second
// per handoff; other changes go at once. A handoff that is no longer open
// is sent as gone.
func (s *HandoffWorkerService) Stream(ctx context.Context, mainInstanceID string, send func(HandoffStreamMessage) error) error {
	id, err := InstanceID(ctx, s.db)
	if err != nil {
		return err
	}
	trigger, stop := s.handoffTriggers()
	defer stop()
	rescanEvery := s.opts.StreamRescan
	if rescanEvery <= 0 {
		rescanEvery = 5 * time.Second
	}
	rescan := time.NewTicker(rescanEvery)
	defer rescan.Stop()

	rows, err := s.summaries(ctx, mainInstanceID)
	if err != nil {
		return err
	}
	if err := send(HandoffStreamMessage{Type: HandoffStreamPull, WorkerInstanceID: id, Handoffs: rows}); err != nil {
		return err
	}
	// The pull is no row: the first change after it goes at once.
	sent := map[string]HandoffSummary{}
	sentAt := map[string]time.Time{}
	for _, r := range rows {
		sent[r.HandoffID] = r
	}
	var flush <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-trigger:
		case <-rescan.C:
		case <-flush:
		}
		rows, err := s.summaries(ctx, mainInstanceID)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Printf("handoff events: %v", err)
			continue
		}
		now := time.Now()
		var wait time.Duration
		seen := map[string]bool{}
		for _, r := range rows {
			seen[r.HandoffID] = true
			prev, ok := sent[r.HandoffID]
			if ok && reflect.DeepEqual(prev, r) {
				continue
			}
			if ok && onlyActivityDiffers(prev, r) {
				if since := now.Sub(sentAt[r.HandoffID]); since < handoffActivityDebounce {
					if left := handoffActivityDebounce - since; wait == 0 || left < wait {
						wait = left
					}
					continue
				}
			}
			row := r
			if err := send(HandoffStreamMessage{Type: HandoffStreamRow, WorkerInstanceID: id, Row: &row}); err != nil {
				return err
			}
			sent[r.HandoffID], sentAt[r.HandoffID] = r, now
		}
		for handoffID := range sent {
			if seen[handoffID] {
				continue
			}
			if err := send(HandoffStreamMessage{Type: HandoffStreamGone, WorkerInstanceID: id, HandoffID: handoffID}); err != nil {
				return err
			}
			delete(sent, handoffID)
			delete(sentAt, handoffID)
		}
		flush = nil
		if wait > 0 {
			flush = time.After(wait)
		}
	}
}

func onlyActivityDiffers(a, b HandoffSummary) bool {
	a.Activity = b.Activity
	return reflect.DeepEqual(a, b)
}

// handoffTriggers is a channel that receives when something a summary row
// shows may have changed in this process: a handoff, a card, an agent.
func (s *HandoffWorkerService) handoffTriggers() (<-chan struct{}, func()) {
	out := make(chan struct{}, 1)
	if s.opts.Bus == nil {
		return out, func() {}
	}
	types := []events.EventType{events.EventHandoffChanged, events.EventAgentActivityChanged, events.EventAgentStarted,
		events.EventAgentDied, events.EventAgentCompleted, events.EventCardMoved, events.EventCardUpdated, events.EventCardsRefreshed}
	subs := make([]<-chan events.Event, 0, len(types))
	done := make(chan struct{})
	for _, t := range types {
		ch := s.opts.Bus.Subscribe(t)
		subs = append(subs, ch)
		go func() {
			for {
				select {
				case <-done:
					return
				case _, ok := <-ch:
					if !ok {
						return
					}
					select {
					case out <- struct{}{}:
					default:
					}
				}
			}
		}()
	}
	return out, func() {
		close(done)
		for _, ch := range subs {
			s.opts.Bus.Unsubscribe(ch)
		}
	}
}
