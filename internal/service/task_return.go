package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// A main's side of bringing away tasks home. Return brings back a task the
// worker finished; Recall brings back one in any state, with a note naming
// the worker and the state. Both are the worker's two steps: step 1 stops
// the agent and answers with the task's title, details, the worker's notes,
// branches and stack, which the main applies in one transaction that ends
// the away state; step 2 has the worker archive its card, and stays owed
// (WorkerOwedCalls) until the worker confirms it. A task the worker shows
// still returning, because a step went unanswered, is finished by the
// return loop (StartReturnLoop). See docs/ai/integrations.md § Return and
// recall.

// ErrReturnRefused: the task cannot be brought back that way: it is not
// away, a failed handoff can only be recalled, a task not done is
// recalled, a task lost on its worker is taken back.
var ErrReturnRefused = errors.New("cannot bring the task back that way")

// NoteSourceWorker marks a note a worker's agent or person wrote, which came
// back to the main with its task; RemoteID is its ID on the worker.
const NoteSourceWorker = "worker"

// ReturnedTask is an away task brought home: the column it landed in,
// whether it was recalled, or taken back without its results, and the
// branches it came back with, in stack order.
type ReturnedTask struct {
	TaskID    string   `json:"task_id"`
	Column    string   `json:"column"`
	Recalled  bool     `json:"recalled"`
	TakenBack bool     `json:"taken_back,omitempty"`
	Branches  []string `json:"branches"`
}

// Return brings home a task its worker finished.
func (s *TaskHandoffService) Return(ctx context.Context, taskID string) (ReturnedTask, error) {
	return s.bringBack(ctx, taskID, false)
}

// Recall brings home a task in any state, with whatever exists so far.
func (s *TaskHandoffService) Recall(ctx context.Context, taskID string) (ReturnedTask, error) {
	return s.bringBack(ctx, taskID, true)
}

// ReturnAllDone returns every task done on a worker. It goes on past a
// task the worker refuses, and stops at a worker that does not answer; it
// returns the tasks brought home and why the others were not.
func (s *TaskHandoffService) ReturnAllDone(ctx context.Context, workerInstanceID string) ([]ReturnedTask, error) {
	rows, err := s.db.ListAwayTasks(ctx)
	if err != nil {
		return nil, err
	}
	out := []ReturnedTask{}
	var errs []error
	for _, r := range rows {
		a := awayTaskFromRow(r)
		if r.WorkerInstanceID != workerInstanceID || a.State != store.HandoffDone || a.Lost != "" {
			continue
		}
		back, err := s.Return(ctx, r.TaskID)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", r.TaskID, err))
			if errors.Is(err, ErrWorkerUnreachable) {
				break
			}
			continue
		}
		out = append(out, back)
	}
	return out, errors.Join(errs...)
}

func (s *TaskHandoffService) bringBack(ctx context.Context, taskID string, recall bool) (ReturnedTask, error) {
	row, err := s.db.OpenAwayTask(ctx, taskID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && row.State != store.AwayAway) {
		return ReturnedTask{}, fmt.Errorf("%w: task %s is not away", ErrReturnRefused, taskID)
	}
	if err != nil {
		return ReturnedTask{}, err
	}
	away := awayTaskFromRow(*row)
	name := orUnknown(away.WorkerName)
	if away.Lost != "" {
		return ReturnedTask{}, fmt.Errorf("%w: %s; take it back", ErrReturnRefused, away.Lost)
	}
	// A task the worker shows returning already is finished as it was
	// asked; any other is a new request.
	if away.State != store.HandoffReturning || row.ReturnAskedAt == nil {
		from := ""
		switch {
		case recall:
			from = workerStateView(away)
		case away.State == store.HandoffFailed:
			return ReturnedTask{}, fmt.Errorf("%w: the handoff failed on %s; recall it", ErrReturnRefused, name)
		case away.State != store.HandoffDone && away.State != store.HandoffReturning:
			return ReturnedTask{}, fmt.Errorf("%w: task %s is not done on %s; recall it to bring back what there is", ErrReturnRefused, taskID, name)
		}
		if err := s.db.AskAwayTaskReturn(ctx, row.HandoffID, from, s.now()); err != nil {
			return ReturnedTask{}, err
		}
	}
	return s.finishReturn(ctx, row.HandoffID, true)
}

// workerStateView is what the worker last said of an away task, apart
// from the main's own view of the worker: the state a recall is made from.
func workerStateView(a AwayTask) string {
	a.Lost, a.LostAt, a.WorkerUnreachableSince, a.WorkerRefusedAt = "", "", "", ""
	return awayView(a)
}

// recallNote is the note a recall adds: the worker and the state the task
// was recalled from.
func recallNote(worker, from, failure string) string {
	where := map[string]string{
		AwayViewStarting:   "where it was starting",
		AwayViewWorking:    "where its agent was working",
		AwayViewNeedsYou:   "where its agent was waiting on you",
		AwayViewIdle:       "where its agent was idle",
		AwayViewEnded:      "where its agent had ended",
		AwayViewDone:       "where it was done",
		AwayViewComingBack: "where it was coming back",
	}[from]
	if from == AwayViewFailed {
		where = "where it had failed"
		if failure != "" {
			where += ": " + failure
		}
	}
	if where == "" {
		where = "where it was " + from
	}
	return fmt.Sprintf("Recalled from %s, %s.", worker, where)
}

func (s *TaskHandoffService) now() string { return s.opts.Now().UTC().Format(time.RFC3339) }

// lockReturn holds a handoff while it is brought home; wait false gives up
// at once when another call holds it.
func (s *TaskHandoffService) lockReturn(handoffID string, wait bool) (unlock func(), ok bool) {
	v, _ := s.returning.LoadOrStore(handoffID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	if wait {
		mu.Lock()
	} else if !mu.TryLock() {
		return nil, false
	}
	return mu.Unlock, true
}

// finishReturn makes both steps of a return the person asked for, or that
// the worker shows under way, skipping what is done: a task applied
// already only has its step 2 sent again. mark says whether a worker that
// does not answer is marked unreachable at once, as for the person's own
// call.
func (s *TaskHandoffService) finishReturn(ctx context.Context, handoffID string, mark bool) (ReturnedTask, error) {
	unlock, _ := s.lockReturn(handoffID, true)
	defer unlock()
	a, err := s.db.GetAwayTask(ctx, handoffID)
	if err != nil {
		return ReturnedTask{}, err
	}
	name := orUnknown(a.WorkerName)
	client, err := s.opts.Workers.Client(ctx, a.WorkerInstanceID)
	if err != nil {
		return ReturnedTask{}, err
	}
	if a.ClosedAt != nil {
		back := ReturnedTask{TaskID: a.TaskID, Recalled: a.RecalledFrom != ""}
		if task, err := s.db.GetTask(ctx, a.TaskID); err == nil {
			back.Column = task.Status
		}
		if a.ArchiveOwed {
			s.archive(ctx, client, a, mark)
		}
		return back, nil
	}

	var answer HandoffReturn
	if err := client.Call(ctx, http.MethodPost, HandoffReturnPath(handoffID), nil, &answer); err != nil {
		return ReturnedTask{}, s.returnFailed(ctx, a, err, mark)
	}
	if answer.WorkerInstanceID != a.WorkerInstanceID {
		return ReturnedTask{}, &handoffWorkerError{ErrWorkerRefused, fmt.Sprintf("%s answers as %s, not %s", name, orUnknown(answer.WorkerInstanceID), a.WorkerInstanceID)}
	}
	task, err := s.db.GetTask(ctx, a.TaskID)
	if err != nil {
		return ReturnedTask{}, err
	}
	column, err := s.landingColumn(ctx, answer.Column, a.LeftColumn)
	if err != nil {
		return ReturnedTask{}, err
	}
	repo := answer.Repo
	if repo == "" {
		repo = a.Repo
	}
	apply := store.AwayReturn{HandoffID: a.HandoffID, TaskID: a.TaskID, Title: task.Title, Description: task.DescriptionMD, Status: column,
		CodeContext: returnedCodeContext(task, repo, answer.Branches, s.now()), At: s.now()}
	if task.Provider != nil {
		// A provider-backed task's title is the provider's, which its sync
		// keeps; only its Legato details come back.
		apply.Title = ""
	} else if strings.TrimSpace(answer.Title) != "" {
		apply.Title = answer.Title
	}
	apply.Description = answer.Details
	for _, n := range answer.Notes {
		if n.ID == "" {
			continue
		}
		apply.Notes = append(apply.Notes, store.TaskNote{ID: workflowID("tn-"), Body: n.Body, Source: NoteSourceWorker, RemoteID: n.ID, CreatedAt: n.CreatedAt})
	}
	if a.RecalledFrom != "" {
		failure := ""
		if view := awayTaskFromRow(*a); view.Failure != "" {
			failure = view.Failure
		}
		apply.Notes = append(apply.Notes, store.TaskNote{ID: workflowID("tn-"), Body: recallNote(name, a.RecalledFrom, failure), Source: "local"})
	}
	if len(answer.Stack) > 0 {
		stack := string(answer.Stack)
		apply.Stack = &stack
	}
	applied, err := s.db.ApplyAwayTaskReturn(ctx, apply)
	if err != nil {
		return ReturnedTask{}, err
	}
	if applied && len(answer.Branches) > 0 {
		// Branches tracked already (while away) keep their place and PRs.
		if _, err := AddTaskBranches(ctx, s.db, a.TaskID, repo, answer.Branches); err != nil {
			log.Printf("handoff %s: tracking the branches it came back with: %v", handoffID, err)
		}
	}
	s.publish(events.EventCardsRefreshed)
	if again, err := s.db.GetAwayTask(ctx, handoffID); err == nil && again.ArchiveOwed {
		s.archive(ctx, client, again, mark)
	}
	return ReturnedTask{TaskID: a.TaskID, Column: column, Recalled: a.RecalledFrom != "", Branches: answer.Branches}, nil
}

// returnFailed is why step 1 failed, in words that name the worker, after
// noting what it says of the task: a worker that did not answer is marked
// unreachable (when mark), one that no longer has the task loses it.
func (s *TaskHandoffService) returnFailed(ctx context.Context, a *store.AwayTask, err error, mark bool) error {
	name := orUnknown(a.WorkerName)
	if errors.Is(err, ErrWorkerUnreachable) {
		if mark {
			s.opts.Workers.MarkUnreachable(ctx, a.WorkerInstanceID)
		}
		return &handoffWorkerError{ErrWorkerUnreachable, fmt.Sprintf("%s did not answer, so the task is still away; trying again is safe", name)}
	}
	var refusal *WorkerRefusal
	if errors.As(err, &refusal) {
		switch {
		case refusal.Code == "not_found":
			reason := fmt.Sprintf("%s no longer has this task", name)
			if marked, err := s.db.SetAwayTaskLost(ctx, a.HandoffID, reason, s.now()); err == nil && marked {
				s.publish(events.EventCardUpdated)
			}
			return fmt.Errorf("%w: %s; take it back", ErrReturnRefused, reason)
		case refusal.Status == http.StatusNotFound:
			return &handoffWorkerError{ErrWorkerTooOld, fmt.Sprintf("update the worker: %s cannot return tasks", name)}
		case refusal.Status == http.StatusUnauthorized:
			return &handoffWorkerError{ErrWorkerRefused, fmt.Sprintf("%s no longer accepts this main; pair again", name)}
		}
		_ = s.db.ClearAwayTaskReturn(ctx, a.HandoffID)
		return &handoffWorkerError{ErrWorkerRefused, fmt.Sprintf("%s refused to return the task: %s", name, refusal.Message)}
	}
	return err
}

// archive sends step 2 for a returned task, and records it done once the
// worker confirms it (or no longer has the handoff). An unanswered call
// stays owed; it is logged, as the task is home already.
func (s *TaskHandoffService) archive(ctx context.Context, client *WorkerClient, a *store.AwayTask, mark bool) error {
	var archived HandoffArchived
	err := client.Call(ctx, http.MethodPost, HandoffArchivePath(a.HandoffID), nil, &archived)
	var refusal *WorkerRefusal
	if err != nil && !(errors.As(err, &refusal) && refusal.Code == "not_found") {
		if mark && errors.Is(err, ErrWorkerUnreachable) {
			s.opts.Workers.MarkUnreachable(ctx, a.WorkerInstanceID)
		}
		log.Printf("handoff %s: asking %s to archive its card: %v", a.HandoffID, orUnknown(a.WorkerName), err)
		return err
	}
	if err := s.db.ClearArchiveOwed(ctx, a.HandoffID); err != nil {
		return err
	}
	// The connection to the worker may no longer be needed.
	s.publish(events.EventCardsRefreshed)
	return nil
}

// Owes says whether the main owes a worker a call: step 2 of a return, the
// Drop of a task taken back, or, for a removed worker, Leave. The
// connection to it stays wanted until they are made (WorkerOwedCalls). A
// removed worker whose time to be told ran out is forgotten here.
func (s *TaskHandoffService) Owes(ctx context.Context, workerInstanceID string) (bool, error) {
	if w, err := s.db.GetWorker(ctx, workerInstanceID); err == nil && w.LeavingUntil != nil {
		if s.leaveLapsed(ctx, w) {
			s.publish(events.EventCardsRefreshed)
			return false, nil
		}
		return true, nil
	}
	owed, err := s.db.ListArchiveOwed(ctx, workerInstanceID)
	if err != nil || len(owed) > 0 {
		return len(owed) > 0, err
	}
	drops, err := s.db.ListDropOwed(ctx, workerInstanceID)
	return len(drops) > 0, err
}

// SendOwed sends a worker every call the main owes it, on connect, before
// the pull: each step 2, then each Drop, then, for a removed worker, Leave,
// after which the worker is forgotten. A worker that does not answer ends
// the connection.
func (s *TaskHandoffService) SendOwed(ctx context.Context, client *WorkerClient) error {
	err := s.sendOwedArchives(ctx, client)
	if err == nil {
		err = s.sendOwedDrops(ctx, client)
	}
	if !client.Worker.Leaving() {
		return err
	}
	if err := s.leave(ctx, client, err); err != nil {
		return err
	}
	s.publish(events.EventCardsRefreshed)
	return nil
}

func (s *TaskHandoffService) sendOwedArchives(ctx context.Context, client *WorkerClient) error {
	owed, err := s.db.ListArchiveOwed(ctx, client.Worker.InstanceID)
	if err != nil {
		return err
	}
	for i := range owed {
		if err := s.archive(ctx, client, &owed[i], false); err != nil {
			return err
		}
	}
	return nil
}

// FinishReturning finishes the returns the workers show under way: each
// away task whose last row says returning, because a step of its return
// went unanswered or this main stopped between them. One a call of the
// person's is bringing home is left to it.
func (s *TaskHandoffService) FinishReturning(ctx context.Context) {
	rows, err := s.db.ListAwayTasks(ctx)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("handoffs: listing returns under way: %v", err)
		}
		return
	}
	for _, r := range rows {
		if a := awayTaskFromRow(r); a.State != store.HandoffReturning || a.Lost != "" || a.WorkerRefusedAt != "" {
			continue
		}
		unlock, ok := s.lockReturn(r.HandoffID, false)
		if !ok {
			continue
		}
		unlock()
		if _, err := s.finishReturn(ctx, r.HandoffID, false); err != nil {
			log.Printf("handoff %s: finishing its return: %v", r.HandoffID, err)
		}
	}
}

// SendOwedArchives sends again each step 2 a worker has not confirmed,
// while the connection to it stays up and so does not send it on connect.
// One that goes unanswered stays owed.
func (s *TaskHandoffService) SendOwedArchives(ctx context.Context) {
	owed, err := s.db.ListArchiveOwed(ctx, "")
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("handoffs: listing the archives owed: %v", err)
		}
		return
	}
	for i := range owed {
		client, err := s.opts.Workers.Client(ctx, owed[i].WorkerInstanceID)
		if err != nil {
			continue
		}
		if s.archive(ctx, client, &owed[i], false) != nil {
			// That worker is down or refusing: its next connect sends it.
			continue
		}
	}
}

// StartReturnLoop finishes returns under way whenever the board hears of a
// summary row or a worker (EventCardUpdated, EventCardsRefreshed), at
// start, and every interval besides, when it also sends again the step 2
// calls and the Drops owed. stop ends it.
func (s *TaskHandoffService) StartReturnLoop(ctx context.Context, interval time.Duration) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	var updated, refreshed <-chan events.Event
	if s.opts.Bus != nil {
		updated = s.opts.Bus.Subscribe(events.EventCardUpdated)
		refreshed = s.opts.Bus.Subscribe(events.EventCardsRefreshed)
	}
	var tick <-chan time.Time
	var ticker *time.Ticker
	if interval > 0 {
		ticker = time.NewTicker(interval)
		tick = ticker.C
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if ticker != nil {
			defer ticker.Stop()
		}
		s.SendOwedArchives(ctx)
		s.SendOwedDrops(ctx)
		for {
			s.FinishReturning(ctx)
			select {
			case <-ctx.Done():
				return
			case <-updated:
			case <-refreshed:
			case <-tick:
				s.SendOwedArchives(ctx)
				s.SendOwedDrops(ctx)
			}
		}
	}()
	return func() {
		cancel()
		<-done
		if s.opts.Bus != nil {
			s.opts.Bus.Unsubscribe(updated)
			s.opts.Bus.Unsubscribe(refreshed)
		}
	}
}

// landingColumn is where a task brought home lands: the main's column of
// the same name as its column on the worker, else the column it left,
// else the first column.
func (s *TaskHandoffService) landingColumn(ctx context.Context, workerColumn, left string) (string, error) {
	columns, err := s.db.ListColumnMappings(ctx)
	if err != nil {
		return "", err
	}
	for _, want := range []string{workerColumn, left} {
		for _, c := range columns {
			if want != "" && strings.EqualFold(c.ColumnName, want) {
				return c.ColumnName, nil
			}
		}
	}
	if len(columns) > 0 {
		return columns[0].ColumnName, nil
	}
	return left, nil
}

// returnedCodeContext is a returned task's code context: the repository,
// and the bottom branch of those it came back with; the worker's folder
// means nothing here.
func returnedCodeContext(task *store.Task, repo string, branches []string, now string) *store.CodeContext {
	cc, err := store.ParseCodeContext(task.CodeContext)
	if err != nil || cc == nil {
		cc = &store.CodeContext{}
	}
	if !strings.EqualFold(cc.Repo, repo) {
		*cc = store.CodeContext{}
	}
	cc.Repo, cc.RepoSource, cc.WorkingDir, cc.HeadSHA = repo, "handoff", "", ""
	if len(branches) > 0 {
		cc.Branch, cc.BranchSource = branches[0], "handoff"
	}
	if cc.LinkedAt == "" {
		cc.LinkedAt = now
	}
	cc.UpdatedAt = now
	return cc
}

func (s *TaskHandoffService) publish(t events.EventType) {
	if s.opts.Bus != nil {
		s.opts.Bus.Publish(events.Event{Type: t, At: time.Now()})
	}
}
