package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// A main's Attention for its away tasks: only what needs an action that only
// the main can take (Return, Recall, Take back, pairing again), and a phone
// notification only for what the worker cannot report itself. "Needs you"
// raises nothing here: the worker's own Attention and notification cover
// it, and the Away lane shows it. See docs/ai/attention.md § Handoff.

// HandoffAttentionProducer is the producer of the items below.
const HandoffAttentionProducer = "handoff"

const (
	// AttentionSignalHandoffDone: an away task is done on its worker and
	// can be returned. Ready for review; no phone notification.
	AttentionSignalHandoffDone = "handoff_done"
	// AttentionSignalHandoffFailed: a handoff failed on the worker, with
	// its reason; it can only be recalled.
	AttentionSignalHandoffFailed = "handoff_failed"
	// AttentionSignalHandoffLost: the worker no longer has the task; only
	// taking it back is left.
	AttentionSignalHandoffLost = "handoff_lost"
	// AttentionSignalHandoffReviewStuck: an away task's review passes are
	// stuck and need the person: Return as is, Run another pass, Message
	// the author, or Recall.
	AttentionSignalHandoffReviewStuck = "handoff_review_stuck"
	// AttentionSignalWorkerNotAccepting: a worker refuses this main's token
	// (401); pair again. One per worker.
	AttentionSignalWorkerNotAccepting = "worker_not_accepting"
	// AttentionSignalWorkerUnreachable: a worker holding away tasks has
	// been unreachable for WorkerUnreachableAttentionAfter. One per worker.
	AttentionSignalWorkerUnreachable = "worker_unreachable"
)

// WorkerUnreachableAttentionAfter is how long a worker holding away tasks
// may be unreachable before the main raises it.
const WorkerUnreachableAttentionAfter = 15 * time.Minute

// HandoffAttentionOptions configure the handoff producer.
type HandoffAttentionOptions struct {
	// Handoffs carries out Return, Recall and Take back; nil lists the
	// items without executing them.
	Handoffs *TaskHandoffService
	// Now is the clock of the 15 minutes; time.Now when nil.
	Now func() time.Time
	// Location is the time zone an outage's start is written in;
	// time.Local when nil.
	Location *time.Location
	// Notifier sends the phone notifications; nil sends none.
	Notifier Notifier
	// ServerURL is this Legato's address for a phone, for the
	// notification's link; nil or "" for none.
	ServerURL func() string
}

// HandoffAttentionAdapter is the handoff producer and its action handler.
type HandoffAttentionAdapter struct {
	db   *store.Store
	opts HandoffAttentionOptions
}

func NewHandoffAttentionAdapter(db *store.Store, opts HandoffAttentionOptions) *HandoffAttentionAdapter {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Location == nil {
		opts.Location = time.Local
	}
	return &HandoffAttentionAdapter{db: db, opts: opts}
}

func (a *HandoffAttentionAdapter) Producer() string { return HandoffAttentionProducer }

// Register adds the producer to an Attention service, reconciled as rows,
// pulls and changes of reach reach the board (EventCardUpdated,
// EventCardsRefreshed), with its actions when it has Handoffs.
func (a *HandoffAttentionAdapter) Register(attention *AttentionService) error {
	if err := attention.RegisterProducerWithEvents(a, events.EventCardUpdated, events.EventCardsRefreshed); err != nil {
		return err
	}
	if a.opts.Handoffs == nil {
		return nil
	}
	return attention.RegisterHandler(a.Producer(), a)
}

// Watch reconciles the producer every interval, so that a worker down for
// 15 minutes is raised although nothing else changes. It stops with ctx or
// the returned function.
func (a *HandoffAttentionAdapter) Watch(ctx context.Context, attention *AttentionService, interval time.Duration) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := attention.Reconcile(ctx, a.Producer()); err != nil && ctx.Err() == nil {
					log.Printf("attention: %v", err)
				}
			}
		}
	}()
	return cancel
}

// handoffNotice is a state the phone is told of: an item's title and
// message, and where its link opens.
type handoffNotice struct {
	key, title, message, path string
}

func (a *HandoffAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	rows, err := a.db.ListAwayTasks(ctx)
	if err != nil {
		return nil, err
	}
	var items []AttentionInput
	var notices []handoffNotice
	awayOn := map[string]int{}
	for _, row := range rows {
		awayOn[row.WorkerInstanceID]++
		item, notice, ok, err := a.taskItem(ctx, awayTaskFromRow(row))
		if err != nil {
			return nil, err
		}
		if ok {
			items = append(items, item)
		}
		if notice != nil {
			notices = append(notices, *notice)
		}
	}
	workers, err := a.db.ListWorkers(ctx)
	if err != nil {
		return nil, err
	}
	for _, w := range workers {
		item, notice, ok := a.workerItem(w, awayOn[w.InstanceID])
		if ok {
			items = append(items, item)
			notices = append(notices, notice)
		}
	}
	a.notify(ctx, notices)
	return items, nil
}

// taskItem is an away task's item, if its state needs the main: lost on
// the worker, failed there, or done.
func (a *HandoffAttentionAdapter) taskItem(ctx context.Context, away AwayTask) (AttentionInput, *handoffNotice, bool, error) {
	title, workspace := away.TaskID, (*int)(nil)
	task, err := a.db.GetTask(ctx, away.TaskID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return AttentionInput{}, nil, false, err
	}
	if task != nil {
		title, workspace = task.Title, task.WorkspaceID
	}
	worker := away.WorkerName
	if worker == "" {
		worker = away.WorkerInstanceID
	}
	item := AttentionInput{
		SourceKey: away.HandoffID, TaskID: away.TaskID, WorkspaceID: workspace,
		Category: "handoff", Urgency: "high",
		Navigation: AttentionNavigation{Kind: "task", Target: away.TaskID},
	}
	openWorker := AttentionAction{ID: "open_worker", Label: "Open on " + worker, Kind: "navigate", Href: away.WorkerURL}
	openTask := AttentionAction{ID: "open", Label: "Open task", Kind: "navigate"}
	path := "/board?highlight_task=" + url.QueryEscape(away.TaskID)
	switch {
	case away.LostAt != "":
		item.SignalKind, item.SourceRevision = AttentionSignalHandoffLost, "lost:"+away.LostAt
		item.Title = fmt.Sprintf("%s is lost on %s", title, worker)
		item.Reason = away.Lost
		item.Detail = "Take it back to work on it here again. Its notes, branches and edits on " + worker + " do not come back."
		item.Actions = []AttentionAction{{ID: "take_back", Label: "Take back", Kind: "execute", Intent: "primary", RequiresConfirm: true,
			Confirmation: fmt.Sprintf("Take back %s without its results? %s. Its notes, branches and edits on %s stay there, and %s is told to stop its agent and archive its card when it can be reached.",
				title, strings.TrimSuffix(away.Lost, "."), worker, worker)}, openTask}
		notice := &handoffNotice{key: item.SourceKey + "@" + item.SourceRevision, title: item.Title, message: away.Lost + ". Take it back in Legato.", path: path}
		return item, notice, true, nil
	case away.State == store.HandoffFailed:
		item.SignalKind, item.SourceRevision = AttentionSignalHandoffFailed, "failed:"+away.Failure
		item.Title = fmt.Sprintf("Handing %s to %s failed", title, worker)
		item.Reason = away.Failure
		if item.Reason == "" {
			item.Reason = worker + " could not start its agent"
		}
		item.Detail = "Recall it, fix the cause and hand it off again."
		item.Actions = []AttentionAction{{ID: "recall", Label: "Recall", Kind: "execute", Intent: "primary"}, openWorker, openTask}
		notice := &handoffNotice{key: item.SourceKey + "@" + item.SourceRevision, title: item.Title, message: item.Reason, path: path}
		return item, notice, true, nil
	case away.Review.Stuck() && away.State != store.HandoffDone:
		review := away.Review
		item.SignalKind, item.SourceRevision = AttentionSignalHandoffReviewStuck, fmt.Sprintf("review:%d:%s", review.Pass, review.Reason)
		item.Title = fmt.Sprintf("The review of %s is stuck on %s", title, worker)
		item.Reason = review.Reason
		var open []string
		for _, c := range []struct {
			n    int
			what string
		}{{review.BlockingOpen, "blocking findings"}, {review.CommentsOpen, "comments"}, {review.ChecksOpen, "failing checks"}} {
			if c.n > 0 {
				open = append(open, fmt.Sprintf("%d %s", c.n, c.what))
			}
		}
		item.Detail = fmt.Sprintf("Pass %d.", review.Pass)
		if len(open) > 0 {
			item.Detail += " Open: " + strings.Join(open, ", ") + "."
		}
		item.Actions = []AttentionAction{
			{ID: "return", Label: "Return as is", Kind: "execute", Intent: "primary"},
			{ID: "another_pass", Label: "Run another pass", Kind: "execute"},
			{ID: "message", Label: "Message the author", Kind: "execute", RequiresText: true},
			{ID: "recall", Label: "Recall", Kind: "execute", RequiresConfirm: true,
				Confirmation: fmt.Sprintf("Recall %s from %s? Its agents stop, and nothing more is posted on GitHub.", title, worker)},
			openWorker, openTask,
		}
		notice := &handoffNotice{key: item.SourceKey + "@" + item.SourceRevision, title: item.Title, message: review.Reason, path: path}
		return item, notice, true, nil
	case away.State == store.HandoffDone:
		// The worker's own agent-finished notification tells the phone.
		item.SignalKind, item.SourceRevision = AttentionSignalHandoffDone, "done"
		item.Title = fmt.Sprintf("%s is done on %s", title, worker)
		item.Reason = "Return it to bring its notes, branches and edits back here."
		item.Urgency = "normal"
		item.Actions = []AttentionAction{{ID: "return", Label: "Return", Kind: "execute", Intent: "primary"}, openWorker, openTask}
		return item, nil, true, nil
	}
	return AttentionInput{}, nil, false, nil
}

// workerItem is a worker's item, if it holds away tasks and no longer
// accepts this main, or has been unreachable for 15 minutes.
func (a *HandoffAttentionAdapter) workerItem(w store.Worker, away int) (AttentionInput, handoffNotice, bool) {
	if away == 0 {
		return AttentionInput{}, handoffNotice{}, false
	}
	name := w.Name
	if name == "" {
		name = w.InstanceID
	}
	tasks := fmt.Sprintf("%d away tasks", away)
	if away == 1 {
		tasks = "1 away task"
	}
	item := AttentionInput{
		Category: "handoff", Urgency: "high",
		Navigation: AttentionNavigation{Kind: "settings", Target: "workers"},
	}
	switch {
	case w.RefusedAt != nil:
		item.SignalKind, item.SourceKey, item.SourceRevision = AttentionSignalWorkerNotAccepting, "worker:"+w.InstanceID+":refused", *w.RefusedAt
		item.Title = name + " no longer accepts this main"
		item.Reason = fmt.Sprintf("Pair again to follow its %s and bring them back.", tasks)
		item.Detail = name + " refused this main's token: it was unpaired there. Until it is paired again, the Away lane keeps what " + name + " last said."
		item.Actions = []AttentionAction{{ID: "pair", Label: "Pair again", Kind: "navigate", Intent: "primary"}}
	case w.UnreachableSince != nil:
		since, err := time.Parse(time.RFC3339, *w.UnreachableSince)
		if err != nil || a.opts.Now().Sub(since) < WorkerUnreachableAttentionAfter {
			return AttentionInput{}, handoffNotice{}, false
		}
		item.SignalKind, item.SourceKey, item.SourceRevision = AttentionSignalWorkerUnreachable, "worker:"+w.InstanceID+":unreachable", *w.UnreachableSince
		item.Title = fmt.Sprintf("%s unreachable since %s, %s", name, a.clock(since), tasks)
		item.Reason = "Legato keeps trying to reach it. If it is gone, take its tasks back from the Away lane."
		item.Actions = []AttentionAction{{ID: "open", Label: "Open Settings › Workers", Kind: "navigate", Intent: "primary"}}
	default:
		return AttentionInput{}, handoffNotice{}, false
	}
	return item, handoffNotice{key: item.SourceKey + "@" + item.SourceRevision, title: item.Title, message: item.Reason, path: "/settings#workers"}, true
}

// clock writes a time as the person reads it: the time of day today, else
// the date too.
func (a *HandoffAttentionAdapter) clock(t time.Time) string {
	t, now := t.In(a.opts.Location), a.opts.Now().In(a.opts.Location)
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return t.Format("15:04")
	}
	return t.Format("Jan 2 15:04")
}

// notify tells the phone of each state not told of before by any Legato on
// this database. The claim is made first, so a failed send is not retried.
func (a *HandoffAttentionAdapter) notify(ctx context.Context, notices []handoffNotice) {
	if a.opts.Notifier == nil || !a.opts.Notifier.Configured() {
		return
	}
	for _, n := range notices {
		claimed, err := a.db.ClaimHandoffNotice(ctx, n.key, a.opts.Now().UTC().Format(time.RFC3339))
		if err != nil {
			log.Printf("attention: recording a handoff notification: %v", err)
			continue
		}
		if !claimed {
			continue
		}
		link := ""
		if a.opts.ServerURL != nil {
			if base := strings.TrimRight(a.opts.ServerURL(), "/"); base != "" {
				link = base + n.path
			}
		}
		notifier := a.opts.Notifier
		go func() {
			if linker, ok := notifier.(LinkNotifier); ok {
				_ = linker.NotifyLink(n.title, n.message, link)
				return
			}
			_ = notifier.Notify(n.title, n.message)
		}()
	}
}

// Execute carries out an item's action: Return, Recall or Take back of the
// away task whose handoff the item is.
func (a *HandoffAttentionAdapter) Execute(ctx context.Context, req AttentionActionRequest) error {
	if a.opts.Handoffs == nil {
		return errors.New("this Legato cannot return away tasks")
	}
	away, err := a.db.GetAwayTask(ctx, req.SourceKey)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: no handoff %s", ErrReturnRefused, req.SourceKey)
	}
	if err != nil {
		return err
	}
	switch req.ActionID {
	case "return":
		_, err = a.opts.Handoffs.Return(ctx, away.TaskID)
	case "recall":
		_, err = a.opts.Handoffs.Recall(ctx, away.TaskID)
	case "take_back":
		_, err = a.opts.Handoffs.TakeBack(ctx, away.TaskID)
	case "another_pass":
		err = a.opts.Handoffs.AnotherPass(ctx, away.TaskID)
	case "message":
		err = a.opts.Handoffs.MessageAuthor(ctx, away.TaskID, req.Text)
	default:
		return fmt.Errorf("unknown handoff action %q", req.ActionID)
	}
	return err
}
