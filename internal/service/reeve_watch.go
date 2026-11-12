package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// ReeveWatcher tells the Reeve what the agents it watches do: when one
// waits for the operator, goes idle, works only in the background, errors
// or exits, adds a note, or has a pull request opened or changed
// (docs/ai/reeve.md § Watching agents). Watches and the events not told yet
// are kept in the database, so a restart of Legato or of the Reeve loses
// nothing. Check runs in the process that serves the broker.
type ReeveWatcher struct {
	opts ReeveWatcherOptions
}

// ReeveWatchSettings are the reeve.watch.* settings, read at each check.
type ReeveWatchSettings struct {
	// Settle is how long an agent must stay idle or in the background
	// before the Reeve is told (30 s when zero).
	Settle time.Duration
	// Wake starts or resumes the Reeve to tell it, when no session is
	// running; off, events wait for the next session.
	Wake bool
	// Started watches every agent the Reeve's change sets start or
	// message.
	Started bool
}

// ReeveWatcherOptions wire a watcher.
type ReeveWatcherOptions struct {
	Store *store.Store
	// Deliver sends the Reeve a message: into the running session,
	// resuming a suspended one, or starting one (ReeveService.Message).
	// The watcher calls it only when the Reeve is between turns, or when
	// none runs and Wake is on.
	Deliver func(ctx context.Context, text string) error
	// Conversation is the end of an agent's conversation, for its last
	// message, what it asks and what still runs; nil, or an error (an agent
	// with no native endpoint), leaves them out.
	Conversation func(ctx context.Context, taskID string) (*ConversationTail, error)
	Settings     func(ctx context.Context) ReeveWatchSettings
	Now          func() time.Time
}

// NewReeveWatcher makes a watcher.
func NewReeveWatcher(opts ReeveWatcherOptions) *ReeveWatcher {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Settings == nil {
		opts.Settings = func(context.Context) ReeveWatchSettings { return ReeveWatchSettings{Started: true} }
	}
	return &ReeveWatcher{opts: opts}
}

// The events a watch can ask for.
const (
	WatchWaiting    = "waiting"
	WatchIdle       = "idle"
	WatchBackground = "background"
	WatchError      = "error"
	WatchExited     = "exited"
	WatchNote       = "note"
	WatchPR         = "pr"
)

// ReeveWatchEvents are every event, the default of a watch.
var ReeveWatchEvents = []string{WatchWaiting, WatchIdle, WatchBackground, WatchError, WatchExited, WatchNote, WatchPR}

// The states a watched agent is seen in. "running" is the activity
// "working"; "starting" an agent not seen running yet.
const (
	watchStarting = "starting"
	watchRunning  = "running"
)

const (
	// ReeveWatchCheckEvery is how often the process that serves the
	// broker checks the watched agents.
	ReeveWatchCheckEvery = 5 * time.Second
	// ReeveWatchSettleDefault is how long an agent must stay idle or in the
	// background before the Reeve is told, when reeve.watch.settle is
	// empty.
	ReeveWatchSettleDefault = 30 * time.Second
	// reeveWatchBatch is how long the first event waits for others, so
	// events close together go as one message.
	reeveWatchBatch = 10 * time.Second
	// reeveWatchPerMessage is the most events in one message; the rest go
	// in the next.
	reeveWatchPerMessage = 40
	// reeveWatchStartWait is how long a watch waits for an agent being
	// prepared (a clone being made) to start, before it is dropped
	// unreported.
	reeveWatchStartWait = 15 * time.Minute
	// reeveWatchLastMessage is the most characters of an agent's last
	// message the Reeve is given.
	reeveWatchLastMessage = 400
)

// watchSeen is what the watcher last saw of a task, kept as the watch's
// JSON, so a restart neither repeats nor loses news.
type watchSeen struct {
	// Notes are the IDs of the notes seen: notes made in the same second
	// do not come in the order they were made.
	Notes     []string          `json:"notes"`
	PRs       map[string]string `json:"prs"`
	CheckedAt time.Time         `json:"checked_at"`
	// State is the agent's state (watchStarting, watchRunning, or a
	// watch event) since StateSince; Prev the one before it, which lasted
	// PrevFor. Told is true once the Reeve was told of State (or it
	// needed no telling).
	State      string        `json:"state"`
	StateSince time.Time     `json:"state_since"`
	Prev       string        `json:"prev,omitempty"`
	PrevFor    time.Duration `json:"prev_for,omitempty"`
	Told       bool          `json:"told"`
	// LastMessage is the agent's last message as last read, kept for when
	// its conversation can no longer be read (it exited).
	LastMessage string `json:"last_message,omitempty"`
	// Since is when the watch began, for an agent still being prepared.
	Since time.Time `json:"since"`
}

// WatchEvent is one thing a watched agent did, queued for the Reeve.
type WatchEvent struct {
	TaskID string    `json:"task_id"`
	Title  string    `json:"title,omitempty"`
	Kind   string    `json:"kind"`
	At     time.Time `json:"at"`
	// For a change of state: from, to, and how long the agent was in from.
	From string        `json:"from,omitempty"`
	To   string        `json:"to,omitempty"`
	For  time.Duration `json:"for,omitempty"`
	// LastMessage is the agent's last message, trimmed.
	LastMessage string `json:"last_message,omitempty"`
	// Running is what the agent started that still runs; NothingRunning
	// says Legato looked and saw nothing; both empty, it could not tell.
	Running        string `json:"running,omitempty"`
	NothingRunning bool   `json:"nothing_running,omitempty"`
	// Asks is the question or permission the agent waits on.
	Asks string `json:"asks,omitempty"`
	// Detail is a note's text, or what changed of a pull request.
	Detail string `json:"detail,omitempty"`
}

// ReeveWatchInfo is one watch, as listed.
type ReeveWatchInfo struct {
	TaskID      string    `json:"task_id"`
	Title       string    `json:"title"`
	Events      []string  `json:"events"`
	State       string    `json:"state"`
	StateSince  time.Time `json:"state_since"`
	ReeveTaskID string    `json:"reeve_task_id"`
	WatchedAt   string    `json:"watched_at"`
}

// parseWatchEvents checks events and gives them in their canonical order;
// none is all of them.
func parseWatchEvents(events []string) ([]string, error) {
	if len(events) == 0 {
		return append([]string(nil), ReeveWatchEvents...), nil
	}
	asked := map[string]bool{}
	for _, e := range events {
		e = strings.ToLower(strings.TrimSpace(e))
		known := false
		for _, k := range ReeveWatchEvents {
			known = known || k == e
		}
		if !known {
			return nil, fmt.Errorf("unknown event %q: watch any of %s", e, strings.Join(ReeveWatchEvents, ", "))
		}
		asked[e] = true
	}
	var out []string
	for _, k := range ReeveWatchEvents {
		if asked[k] {
			out = append(out, k)
		}
	}
	return out, nil
}

func splitEvents(raw string) []string {
	if raw == "" {
		return append([]string(nil), ReeveWatchEvents...)
	}
	return strings.Split(raw, ",")
}

// Watch starts telling the Reeve what the agent of taskID does from now
// on, for the events asked (all when none). reeveTaskID is the session
// that asked. Watching again replaces the events and keeps what was seen.
func (w *ReeveWatcher) Watch(ctx context.Context, reeveTaskID, taskID string, events []string) error {
	asked, err := parseWatchEvents(events)
	if err != nil {
		return err
	}
	if taskID == "" || taskID == reeveTaskID || IsReeveTask(ctx, w.opts.Store, taskID) {
		return fmt.Errorf("the Reeve watches agents, not itself")
	}
	if _, err := w.opts.Store.GetTask(ctx, taskID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("no task %s: %w", taskID, store.ErrNotFound)
		}
		return err
	}
	seen := w.look(ctx, taskID)
	seen.Since = seen.CheckedAt
	seen.StateSince = seen.CheckedAt
	seen.Told = true // what it is doing now is not news
	raw, _ := json.Marshal(seen)
	return w.opts.Store.WatchForReeve(ctx, store.ReeveWatch{TaskID: taskID, ReeveTaskID: reeveTaskID, Events: strings.Join(asked, ","), Seen: string(raw)})
}

// WatchStarted watches, for every event, an agent a change set of the
// Reeve started or messaged, while reeve.watch.started is on. An agent
// already watched keeps its events.
func (w *ReeveWatcher) WatchStarted(ctx context.Context, reeveTaskID, taskID string) {
	if !w.opts.Settings(ctx).Started {
		return
	}
	watches, _ := w.opts.Store.ListReeveWatches(ctx)
	for _, watch := range watches {
		if watch.TaskID == taskID {
			return
		}
	}
	if err := w.Watch(ctx, reeveTaskID, taskID, nil); err != nil {
		log.Printf("reeve: watching %s: %v", taskID, err)
	}
}

// Unwatch stops watching a task's agent; store.ErrNotFound when it is not
// watched.
func (w *ReeveWatcher) Unwatch(ctx context.Context, taskID string) error {
	watches, err := w.opts.Store.ListReeveWatches(ctx)
	if err != nil {
		return err
	}
	for _, watch := range watches {
		if watch.TaskID == taskID {
			return w.opts.Store.DeleteReeveWatch(ctx, taskID)
		}
	}
	return fmt.Errorf("%s is not watched: %w", taskID, store.ErrNotFound)
}

// List lists the watched agents, oldest watch first.
func (w *ReeveWatcher) List(ctx context.Context) ([]ReeveWatchInfo, error) {
	watches, err := w.opts.Store.ListReeveWatches(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ReeveWatchInfo, 0, len(watches))
	for _, watch := range watches {
		var seen watchSeen
		_ = json.Unmarshal([]byte(watch.Seen), &seen)
		out = append(out, ReeveWatchInfo{TaskID: watch.TaskID, Title: w.title(ctx, watch.TaskID), Events: splitEvents(watch.Events),
			State: seen.State, StateSince: seen.StateSince, ReeveTaskID: watch.ReeveTaskID, WatchedAt: watch.CreatedAt})
	}
	return out, nil
}

func (w *ReeveWatcher) title(ctx context.Context, taskID string) string {
	if task, err := w.opts.Store.GetTask(ctx, taskID); err == nil && task != nil {
		return task.Title
	}
	return ""
}

// look is what there is to see of a task now, but its state's history.
func (w *ReeveWatcher) look(ctx context.Context, taskID string) watchSeen {
	seen := watchSeen{PRs: map[string]string{}, CheckedAt: w.opts.Now(), State: watchStarting}
	if notes, err := w.opts.Store.ListTaskNotes(ctx, taskID); err == nil {
		for _, n := range notes {
			seen.Notes = append(seen.Notes, n.ID)
		}
	}
	for key, pr := range w.pullRequests(ctx, taskID) {
		seen.PRs[key] = prState(pr)
	}
	if state, ok := w.agentState(ctx, taskID); ok {
		seen.State = state
	}
	return seen
}

// agentState is the state of a task's running agent; false when none runs.
func (w *ReeveWatcher) agentState(ctx context.Context, taskID string) (string, bool) {
	a, err := w.opts.Store.LatestAgentSessionForTask(ctx, taskID)
	if err != nil || a == nil || a.Status != "running" {
		return "", false
	}
	switch a.Activity {
	case store.ActivityWorking:
		return watchRunning, true
	case store.ActivityWaiting:
		return WatchWaiting, true
	case store.ActivityError:
		return WatchError, true
	case store.ActivityBackground:
		return WatchBackground, true
	}
	return WatchIdle, true
}

// backgroundJobs names what a task's running agent started that still runs
// and will wake it, as its hooks reported; "" when none is known.
func (w *ReeveWatcher) backgroundJobs(ctx context.Context, taskID string) string {
	a, err := w.opts.Store.LatestAgentSessionForTask(ctx, taskID)
	if err != nil || a == nil || a.Status != "running" || len(a.BackgroundJobs) == 0 {
		return ""
	}
	names := make([]string, len(a.BackgroundJobs))
	for i, job := range a.BackgroundJobs {
		names[i] = fmt.Sprintf("%s %q", job.Kind, oneLine(clipRunes(job.Label, 120)))
	}
	return clipRunes(strings.Join(names, "; "), 300)
}

func (w *ReeveWatcher) pullRequests(ctx context.Context, taskID string) map[string]store.PRMeta {
	out := map[string]store.PRMeta{}
	branches, err := w.opts.Store.ListTaskBranches(ctx, taskID)
	if err != nil {
		return out
	}
	for _, b := range branches {
		if b.PR != nil && b.PR.PRNumber > 0 {
			out[fmt.Sprintf("%s#%d", b.Repo, b.PR.PRNumber)] = *b.PR
		}
	}
	return out
}

// prState is what of a pull request the Reeve is told when it changes.
func prState(pr store.PRMeta) string {
	parts := []string{strings.ToLower(firstNonEmpty(pr.State, "open"))}
	if pr.IsDraft {
		parts = append(parts, "draft")
	}
	if pr.CheckStatus != "" {
		parts = append(parts, "checks "+pr.CheckStatus)
	}
	if pr.ReviewDecision != "" {
		parts = append(parts, strings.ToLower(strings.ReplaceAll(pr.ReviewDecision, "_", " ")))
	}
	return strings.Join(parts, ", ")
}

// Check looks at every watched agent, queues what the Reeve has not been
// told, and tells it when it is between turns (or, with Wake on, when no
// session runs). It returns how many messages it sent.
func (w *ReeveWatcher) Check(ctx context.Context) int {
	settings := w.opts.Settings(ctx)
	if settings.Settle <= 0 {
		settings.Settle = ReeveWatchSettleDefault
	}
	watches, err := w.opts.Store.ListReeveWatches(ctx)
	if err != nil {
		log.Printf("reeve: listing watches: %v", err)
	}
	for _, watch := range watches {
		w.checkOne(ctx, watch, settings)
	}
	return w.deliver(ctx, settings)
}

func (w *ReeveWatcher) checkOne(ctx context.Context, watch store.ReeveWatch, settings ReeveWatchSettings) {
	var before watchSeen
	_ = json.Unmarshal([]byte(watch.Seen), &before)
	events, ended, now := w.news(ctx, watch.TaskID, before, settings.Settle)
	asked := map[string]bool{}
	for _, e := range splitEvents(watch.Events) {
		asked[e] = true
	}
	title := w.title(ctx, watch.TaskID)
	for _, e := range events {
		if !asked[e.Kind] {
			continue
		}
		e.Title = title
		raw, _ := json.Marshal(e)
		if err := w.opts.Store.QueueReeveWatchEvent(ctx, watch.TaskID, string(raw)); err != nil {
			log.Printf("reeve: queueing what %s did: %v", watch.TaskID, err)
			return // seen is not moved on, so the next check finds it again
		}
	}
	if ended {
		_ = w.opts.Store.DeleteReeveWatch(ctx, watch.TaskID)
		return
	}
	raw, _ := json.Marshal(now)
	if err := w.opts.Store.SetReeveWatchSeen(ctx, watch.TaskID, string(raw)); err != nil {
		log.Printf("reeve: watching %s: %v", watch.TaskID, err)
	}
}

// news is what happened on a task since before: the events, whether the
// watch is over, and what is seen now.
func (w *ReeveWatcher) news(ctx context.Context, taskID string, before watchSeen, settle time.Duration) ([]WatchEvent, bool, watchSeen) {
	now := w.look(ctx, taskID)
	at := now.CheckedAt
	now.Since, now.LastMessage = before.Since, before.LastMessage
	if before.State == "" {
		before.State, before.StateSince = watchStarting, before.Since
	}
	var events []WatchEvent

	if notes, err := w.opts.Store.ListTaskNotes(ctx, taskID); err == nil {
		known := map[string]bool{}
		for _, id := range before.Notes {
			known[id] = true
		}
		for _, n := range notes {
			if !known[n.ID] {
				events = append(events, WatchEvent{TaskID: taskID, Kind: WatchNote, At: at, Detail: clipRunes(strings.TrimSpace(n.Body), reeveWatchLastMessage)})
			}
		}
	}
	prs := w.pullRequests(ctx, taskID)
	keys := make([]string, 0, len(prs))
	for key := range prs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		pr, state := prs[key], now.PRs[key]
		old, had := before.PRs[key]
		switch {
		case !had:
			events = append(events, WatchEvent{TaskID: taskID, Kind: WatchPR, At: at, Detail: fmt.Sprintf("opened pull request #%d (%s): %s", pr.PRNumber, firstNonEmpty(pr.PRURL, key), state)})
		case old != state:
			events = append(events, WatchEvent{TaskID: taskID, Kind: WatchPR, At: at, Detail: fmt.Sprintf("pull request #%d is now %s (was %s)", pr.PRNumber, state, old)})
		}
	}

	// Its state: carried over unless it changed.
	now.StateSince, now.Prev, now.PrevFor, now.Told = before.StateSince, before.Prev, before.PrevFor, before.Told
	if _, running := w.agentState(ctx, taskID); !running {
		if before.State == watchStarting {
			// Not started yet: wait for it a while, then let it go.
			now.State = watchStarting
			return events, at.Sub(before.Since) > reeveWatchStartWait, now
		}
		e := WatchEvent{TaskID: taskID, Kind: WatchExited, At: at, From: before.State, To: WatchExited, For: at.Sub(before.StateSince), LastMessage: before.LastMessage}
		return append(events, e), true, now
	}
	changed := now.State != before.State
	if !changed && now.State != watchRunning && !before.CheckedAt.IsZero() {
		// A turn shorter than a check: it worked, and is back where it was.
		if worked, _ := w.opts.Store.TaskWorkedSince(ctx, taskID, before.CheckedAt); worked {
			before.State, before.StateSince = watchRunning, before.CheckedAt
			changed = true
		}
	}
	if changed {
		now.Prev, now.PrevFor, now.StateSince, now.Told = before.State, at.Sub(before.StateSince), at, now.State == watchRunning
	}
	if now.Told {
		return events, false, now
	}
	switch now.State {
	case WatchIdle, WatchBackground:
		if at.Sub(now.StateSince) < settle {
			return events, false, now
		}
	}
	e := WatchEvent{TaskID: taskID, Kind: now.State, At: at, From: now.Prev, To: now.State, For: now.PrevFor}
	w.describe(ctx, &e, &now)
	now.Told = true
	return append(events, e), false, now
}

// describe adds to a change of state what the agent's conversation says:
// its last message, what still runs, and what it asks.
func (w *ReeveWatcher) describe(ctx context.Context, e *WatchEvent, seen *watchSeen) {
	// The hooks' list of background jobs is the whole of what still runs;
	// the conversation is read for it only when they listed none.
	fromHooks := false
	if e.To == WatchBackground {
		e.Running = "work it started in the background"
		if jobs := w.backgroundJobs(ctx, e.TaskID); jobs != "" {
			e.Running, fromHooks = jobs, true
		}
	}
	if w.opts.Conversation == nil {
		e.LastMessage = seen.LastMessage
		return
	}
	tail, err := w.opts.Conversation(ctx, e.TaskID)
	if err != nil || tail == nil {
		e.LastMessage = seen.LastMessage
		return
	}
	var running []string
	for _, entry := range tail.Entries {
		switch {
		case entry.Kind == EntryAssistant && strings.TrimSpace(entry.Text) != "":
			seen.LastMessage = clipRunes(strings.TrimSpace(entry.Text), reeveWatchLastMessage)
		case entry.Kind == EntryTool && entry.State == ToolRunning:
			running = append(running, strings.TrimSpace(entry.Tool+": "+firstNonEmpty(entry.Command, entry.Text)))
		}
	}
	e.LastMessage = seen.LastMessage
	switch {
	case fromHooks:
	case len(running) > 0:
		e.Running = clipRunes(strings.Join(running, "; "), 300)
	case e.Running == "":
		e.NothingRunning = true
	}
	switch {
	case tail.Permission != nil && tail.Permission.Plan != "":
		e.Asks = "approval of a plan: " + clipRunes(tail.Permission.Plan, 300)
	case tail.Permission != nil:
		e.Asks = strings.TrimSpace(fmt.Sprintf("permission to use %s: %s", tail.Permission.Tool, firstNonEmpty(tail.Permission.Command, tail.Permission.Reason)))
		if tail.Permission.Reason != "" && tail.Permission.Command != "" {
			e.Asks += " (" + tail.Permission.Reason + ")"
		}
	case tail.Question != nil:
		e.Asks = clipRunes(tail.Question.Text, 300)
		if len(tail.Question.Options) > 0 {
			e.Asks += " Options: " + strings.Join(tail.Question.Options, " / ")
		}
	}
}

// deliver tells the Reeve the queued events, in one message, once the
// first has waited reeveWatchBatch for others and the Reeve is between
// turns; with no session running, only when Wake is on.
func (w *ReeveWatcher) deliver(ctx context.Context, settings ReeveWatchSettings) int {
	if w.opts.Deliver == nil {
		return 0
	}
	queued, err := w.opts.Store.ListReeveWatchEvents(ctx, reeveWatchPerMessage)
	if err != nil || len(queued) == 0 {
		return 0
	}
	events := make([]WatchEvent, 0, len(queued))
	for _, q := range queued {
		var e WatchEvent
		if json.Unmarshal([]byte(q.Body), &e) == nil {
			events = append(events, e)
		}
	}
	if len(events) > 0 && w.opts.Now().Sub(events[0].At) < reeveWatchBatch {
		return 0
	}
	if !w.reeveCanHear(ctx, settings) {
		return 0
	}
	if len(events) > 0 {
		if err := w.opts.Deliver(ctx, WatchMessage(events)); err != nil {
			log.Printf("reeve: telling it what its agents did: %v", err)
			return 0
		}
	}
	if err := w.opts.Store.DeleteReeveWatchEventsThrough(ctx, queued[len(queued)-1].ID); err != nil {
		log.Printf("reeve: clearing what it was told: %v", err)
	}
	return 1
}

// reeveCanHear says whether a message to the Reeve now lands between its
// turns: a running session whose agent is not working or waiting on the
// operator; or, with Wake on, a suspended session or none, which the
// message resumes or starts.
func (w *ReeveWatcher) reeveCanHear(ctx context.Context, settings ReeveWatchSettings) bool {
	current, err := w.opts.Store.CurrentReeveSession(ctx)
	if err != nil {
		return false
	}
	if current == nil || current.State != store.ReeveRunning {
		return settings.Wake
	}
	agent, err := w.opts.Store.LatestAgentSessionForTask(ctx, current.TaskID)
	if err != nil || agent == nil || agent.Status != "running" {
		// Its agent is starting or stopping: wait for it.
		return false
	}
	return agent.Activity != store.ActivityWorking && agent.Activity != store.ActivityWaiting
}

// WatchMessage is the message that tells the Reeve what its watched agents
// did.
func WatchMessage(events []WatchEvent) string {
	var b strings.Builder
	if len(events) == 1 {
		b.WriteString("[legato agent] An agent you watch:\n")
	} else {
		fmt.Fprintf(&b, "[legato agent] %d things from agents you watch:\n", len(events))
	}
	asks := false
	for _, e := range events {
		// What an agent or a ticket wrote (a title, a note, a question) is
		// quoted or kept to one line, so it never starts a line of its own
		// and passes for Legato's words.
		name := e.TaskID
		if e.Title != "" {
			name += " (" + oneLine(e.Title) + ")"
		}
		switch e.Kind {
		case WatchNote:
			fmt.Fprintf(&b, "- %s added a note: %q\n", name, e.Detail)
			continue
		case WatchPR:
			fmt.Fprintf(&b, "- %s %s\n", name, e.Detail)
			continue
		}
		fmt.Fprintf(&b, "- %s: %s → %s", name, e.From, e.To)
		if e.For > 0 && e.From != watchStarting {
			fmt.Fprintf(&b, " after %s %s", e.For.Round(time.Second), e.From)
		}
		b.WriteString(".")
		if e.To == WatchExited {
			b.WriteString(" Its watch has ended.")
		}
		b.WriteString("\n")
		switch {
		case e.LastMessage != "" && e.To == WatchExited:
			// An ended agent's conversation cannot be read: this is the last
			// message read at an earlier event, perhaps turns ago.
			fmt.Fprintf(&b, "  The last message Legato read from it, before it exited: %q\n", e.LastMessage)
		case e.LastMessage != "":
			fmt.Fprintf(&b, "  Its last message: %q\n", e.LastMessage)
		case e.To != WatchExited:
			b.WriteString("  Its last message: Legato cannot read its conversation.\n")
		}
		if e.To != WatchExited {
			switch {
			case e.Running != "":
				fmt.Fprintf(&b, "  Still running: %s\n", e.Running)
			case e.NothingRunning:
				b.WriteString("  Nothing it started still runs, as far as Legato can see: only a message will wake it.\n")
			default:
				b.WriteString("  Legato cannot tell whether anything it started still runs.\n")
			}
		}
		if e.To == WatchWaiting {
			asks = true
			if e.Asks != "" {
				fmt.Fprintf(&b, "  It waits on the operator for %s\n", oneLine(e.Asks))
			} else {
				b.WriteString("  It waits on the operator: open the agent to see what it asks.\n")
			}
		}
	}
	b.WriteString("Read the task's notes before you decide; then propose one step at a time.")
	if asks {
		b.WriteString(" Never answer a permission prompt yourself: tell the operator what it asks.")
	}
	b.WriteString("\n")
	return b.String()
}

// oneLine is s with its line breaks and tabs as spaces.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
