package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// ReeveWatcher tells the Reeve what the agents it started do: a note they
// add, a pull request opened or changed, a turn of work ended, a wait for
// the operator, the end of their session. Only agents a change set of the
// current session started or messaged are watched (Watch), so the Reeve is
// not told of the rest. Check runs in the process that serves the broker.
type ReeveWatcher struct {
	opts ReeveWatcherOptions
}

// ReeveWatcherOptions are a watcher's store, how it tells the Reeve, and
// its clock.
type ReeveWatcherOptions struct {
	Store *store.Store
	// Notify tells a Reeve session something (resuming it when suspended).
	Notify func(ctx context.Context, reeveTaskID, text string)
	Now    func() time.Time
}

// NewReeveWatcher makes a watcher.
func NewReeveWatcher(opts ReeveWatcherOptions) *ReeveWatcher {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &ReeveWatcher{opts: opts}
}

// ReeveWatchCheckEvery is how often the process that serves the broker
// checks the watched agents.
const ReeveWatchCheckEvery = 5 * time.Second

// watchSeen is what the watcher last saw of a task, kept as the watch's
// JSON, so a restart neither repeats nor loses news.
type watchSeen struct {
	// Notes are the IDs of the notes seen: notes made in the same second
	// do not come in the order they were made.
	Notes     []string          `json:"notes"`
	PRs       map[string]string `json:"prs"`
	Activity  string            `json:"activity"`
	CheckedAt time.Time         `json:"checked_at"`
	// Running is true once the agent was seen running; Since is when the
	// watch began, for an agent still being prepared.
	Running bool      `json:"running"`
	Since   time.Time `json:"since"`
}

// reeveWatchStartWait is how long a watch waits for an agent being
// prepared (a clone being made) to start, before it is dropped unreported.
const reeveWatchStartWait = 15 * time.Minute

// Watch starts telling the Reeve session reeveTaskID what the agent of
// taskID does from now on. Watching again changes nothing but the session.
func (w *ReeveWatcher) Watch(ctx context.Context, reeveTaskID, taskID string) error {
	if reeveTaskID == "" || taskID == "" || reeveTaskID == taskID {
		return nil
	}
	seen := w.look(ctx, taskID)
	seen.Since = seen.CheckedAt
	raw, _ := json.Marshal(seen)
	return w.opts.Store.WatchForReeve(ctx, store.ReeveWatch{TaskID: taskID, ReeveTaskID: reeveTaskID, Seen: string(raw)})
}

// look is what there is to see of a task now.
func (w *ReeveWatcher) look(ctx context.Context, taskID string) watchSeen {
	seen := watchSeen{PRs: map[string]string{}, CheckedAt: w.opts.Now()}
	if notes, err := w.opts.Store.ListTaskNotes(ctx, taskID); err == nil {
		for _, n := range notes {
			seen.Notes = append(seen.Notes, n.ID)
		}
	}
	for key, pr := range w.pullRequests(ctx, taskID) {
		seen.PRs[key] = prState(pr)
	}
	if a, err := w.opts.Store.LatestAgentSessionForTask(ctx, taskID); err == nil && a != nil && a.Status == "running" {
		seen.Activity = a.Activity
		seen.Running = true
	}
	return seen
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

// Check tells each watching session what its agents did since the last
// check, one message per agent, and drops watches of sessions that are not
// the current one and of agents that ended. It returns how many messages
// it sent.
func (w *ReeveWatcher) Check(ctx context.Context) int {
	watches, err := w.opts.Store.ListReeveWatches(ctx)
	if err != nil || len(watches) == 0 {
		return 0
	}
	current, _ := w.opts.Store.CurrentReeveSession(ctx)
	sent := 0
	for _, watch := range watches {
		if current == nil || current.TaskID != watch.ReeveTaskID {
			_ = w.opts.Store.DeleteReeveWatch(ctx, watch.TaskID)
			continue
		}
		var before watchSeen
		_ = json.Unmarshal([]byte(watch.Seen), &before)
		news, ended, now := w.news(ctx, watch.TaskID, before)
		if ended {
			_ = w.opts.Store.DeleteReeveWatch(ctx, watch.TaskID)
		} else {
			raw, _ := json.Marshal(now)
			if err := w.opts.Store.SetReeveWatchSeen(ctx, watch.TaskID, string(raw)); err != nil {
				log.Printf("reeve: watching %s: %v", watch.TaskID, err)
				continue
			}
		}
		if len(news) == 0 || w.opts.Notify == nil {
			continue
		}
		w.opts.Notify(ctx, watch.ReeveTaskID, w.message(ctx, watch.TaskID, news))
		sent++
	}
	return sent
}

// news is what happened on a task since before: the lines to tell, whether
// its agent ended, and what is seen now.
func (w *ReeveWatcher) news(ctx context.Context, taskID string, before watchSeen) ([]string, bool, watchSeen) {
	now := w.look(ctx, taskID)
	var news []string
	if notes, err := w.opts.Store.ListTaskNotes(ctx, taskID); err == nil {
		known := map[string]bool{}
		for _, id := range before.Notes {
			known[id] = true
		}
		for _, n := range notes {
			if !known[n.ID] {
				news = append(news, "added a note: "+clipRunes(strings.TrimSpace(n.Body), 400))
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
			news = append(news, fmt.Sprintf("opened pull request #%d (%s): %s", pr.PRNumber, firstNonEmpty(pr.PRURL, key), state))
		case old != state:
			news = append(news, fmt.Sprintf("pull request #%d is now %s (was %s)", pr.PRNumber, state, old))
		}
	}
	now.Since = before.Since
	agent, err := w.opts.Store.LatestAgentSessionForTask(ctx, taskID)
	if err != nil || agent == nil || agent.Status != "running" {
		if !before.Running {
			// Not started yet: wait for it a while, then let it go.
			return news, now.CheckedAt.Sub(before.Since) > reeveWatchStartWait, now
		}
		return append(news, "ended: its session is over"), true, now
	}
	worked := before.Activity == store.ActivityWorking
	if !worked && !before.CheckedAt.IsZero() {
		worked, _ = w.opts.Store.TaskWorkedSince(ctx, taskID, before.CheckedAt)
	}
	switch agent.Activity {
	case store.ActivityWorking, store.ActivityBackground:
	case store.ActivityWaiting:
		if before.Activity != store.ActivityWaiting {
			news = append(news, "is waiting for the operator: an answer or a permission")
		}
	case store.ActivityError:
		if before.Activity != store.ActivityError {
			news = append(news, "stopped with an error")
		}
	default:
		if worked {
			news = append(news, "finished its turn and is idle")
		}
	}
	return news, false, now
}

func (w *ReeveWatcher) message(ctx context.Context, taskID string, news []string) string {
	title := ""
	if task, err := w.opts.Store.GetTask(ctx, taskID); err == nil && task != nil {
		title = " (" + task.Title + ")"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[legato agent] The agent on %s%s, which you started or messaged:\n", taskID, title)
	for _, line := range news {
		b.WriteString("- " + line + "\n")
	}
	return b.String()
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
