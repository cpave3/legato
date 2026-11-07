package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// A main's ways out when a worker is gone or no longer wanted. Take back
// ends a task's away state on the main alone, without the worker's
// results, and leaves a Drop owed: the worker stops the task's agent and
// archives its card the next time the main reaches it. Removing a worker
// recalls what it can, takes back the rest, and tells the worker to unpair
// this main (Leave); a worker that cannot be reached is kept, hidden, only
// to send it those calls, for WorkerLeaveWindow. The main never takes a
// task back on its own. See docs/ai/integrations.md § Take back, Drop and
// Leave.

// WorkerLeaveWindow is how long a removed worker's address and token are
// kept to tell it to leave.
const WorkerLeaveWindow = 30 * 24 * time.Hour

// HandoffLeavePath is the worker's route for Leave: it unpairs the calling
// main.
const HandoffLeavePath = "/api/integrations/handoff/leave"

// HandoffDropPath is the worker's route for Drop of a handoff.
func HandoffDropPath(handoffID string) string {
	return HandoffCallPath + "/" + url.PathEscape(handoffID) + "/drop"
}

// HandoffDropped is the worker's answer to Drop, HandoffLeft its answer to
// Leave.
type HandoffDropped struct {
	HandoffID        string `json:"handoff_id"`
	WorkerInstanceID string `json:"worker_instance_id"`
}

type HandoffLeft struct {
	WorkerInstanceID string `json:"worker_instance_id"`
	// Closed is how many of the main's handoffs were still open, and are
	// now ordinary cards on the worker.
	Closed int `json:"closed"`
}

// RemovedWorker is what removing a worker did: the tasks recalled with
// their results, those taken back without them, and whether the worker was
// told to unpair this main (false: it is told when it is next reached).
type RemovedWorker struct {
	Worker    string         `json:"worker"`
	Recalled  []ReturnedTask `json:"recalled"`
	TakenBack []ReturnedTask `json:"taken_back"`
	Left      bool           `json:"left"`
}

// TakeBack ends a task's away state without its worker's results, when
// the worker cannot bring it back: it is unreachable, lost the task, or no
// longer accepts this main. The worker is told to drop it when it is next
// reached. A worker that can still bring the task back is recalled
// instead.
func (s *TaskHandoffService) TakeBack(ctx context.Context, taskID string) (ReturnedTask, error) {
	row, err := s.db.OpenAwayTask(ctx, taskID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && row.State != store.AwayAway) {
		return ReturnedTask{}, fmt.Errorf("%w: task %s is not away", ErrReturnRefused, taskID)
	}
	if err != nil {
		return ReturnedTask{}, err
	}
	away := awayTaskFromRow(*row)
	name := orUnknown(away.WorkerName)
	var why string
	switch away.View {
	case AwayViewLost:
		why = away.Lost
	case AwayViewUnreachable:
		why = fmt.Sprintf("%s has been unreachable since %s", name, away.WorkerUnreachableSince)
	case AwayViewNotAccepted:
		why = fmt.Sprintf("%s no longer accepts this main", name)
	default:
		return ReturnedTask{}, fmt.Errorf("%w: %s can still bring task %s back; recall it", ErrReturnRefused, name, taskID)
	}
	return s.takeBack(ctx, row, why, true)
}

// takeBack closes an away task without its results, with a note saying
// why, and leaves its Drop owed; tell sends the Drop at once to a worker
// not known to be down or refusing this main.
func (s *TaskHandoffService) takeBack(ctx context.Context, row *store.AwayTask, why string, tell bool) (ReturnedTask, error) {
	name := orUnknown(row.WorkerName)
	body := fmt.Sprintf("Taken back from %s without its results: %s. %s is told to drop it when it can be reached.", name, why, name)
	if review := ReviewBlockNote(awayTaskFromRow(*row).Review); review != "" {
		body += "\n\n" + review
	}
	note := store.TaskNote{ID: workflowID("tn-"), Source: "local", Body: body}
	took, err := s.db.TakeBackAwayTask(ctx, row.HandoffID, note, s.now())
	if err != nil {
		return ReturnedTask{}, err
	}
	if !took {
		return ReturnedTask{}, fmt.Errorf("%w: task %s is not away", ErrReturnRefused, row.TaskID)
	}
	s.publish(events.EventCardsRefreshed)
	if tell && row.WorkerUnreachableSince == nil && row.WorkerRefusedAt == nil {
		if client, err := s.opts.Workers.owedClient(ctx, row.WorkerInstanceID); err == nil {
			// Unanswered, it stays owed: the next connect sends it.
			_ = s.drop(ctx, client, row)
		}
	}
	back := ReturnedTask{TaskID: row.TaskID, TakenBack: true}
	if task, err := s.db.GetTask(ctx, row.TaskID); err == nil {
		back.Column = task.Status
	}
	return back, nil
}

// drop sends a worker the Drop of a task taken back, and records it done
// once the worker confirms it, or no longer has the handoff, or is too old
// to know Drop.
func (s *TaskHandoffService) drop(ctx context.Context, client *WorkerClient, a *store.AwayTask) error {
	var dropped HandoffDropped
	err := client.Call(ctx, http.MethodPost, HandoffDropPath(a.HandoffID), nil, &dropped)
	var refusal *WorkerRefusal
	if err != nil && !(errors.As(err, &refusal) && refusal.Status == http.StatusNotFound) {
		log.Printf("handoff %s: asking %s to drop it: %v", a.HandoffID, orUnknown(a.WorkerName), err)
		return err
	}
	if err := s.db.ClearDropOwed(ctx, a.HandoffID); err != nil {
		return err
	}
	// The connection to the worker may no longer be needed.
	s.publish(events.EventCardsRefreshed)
	return nil
}

// RemoveWorker removes a paired worker: it recalls each task away on it
// that it can, takes back the rest without results, forgets the worker and
// tells it to unpair this main. A worker that does not answer is kept,
// hidden from every list, only to send it the Drops and the Leave owed it,
// for WorkerLeaveWindow.
func (s *TaskHandoffService) RemoveWorker(ctx context.Context, workerInstanceID string) (RemovedWorker, error) {
	client, err := s.opts.Workers.Client(ctx, workerInstanceID)
	if err != nil {
		return RemovedWorker{}, err
	}
	name := client.Worker.Name
	out := RemovedWorker{Worker: name, Recalled: []ReturnedTask{}, TakenBack: []ReturnedTask{}}
	rows, err := s.db.ListAwayTasks(ctx)
	if err != nil {
		return RemovedWorker{}, err
	}
	reachable := true
	for _, r := range rows {
		if r.WorkerInstanceID != workerInstanceID {
			continue
		}
		if a := awayTaskFromRow(r); reachable && a.Lost == "" && a.WorkerRefusedAt == "" {
			back, err := s.Recall(ctx, r.TaskID)
			if err == nil {
				out.Recalled = append(out.Recalled, back)
				continue
			}
			if errors.Is(err, ErrWorkerUnreachable) {
				reachable = false
			}
			log.Printf("removing worker %s: recalling %s: %v", name, r.TaskID, err)
		}
		row, err := s.db.OpenAwayTask(ctx, r.TaskID)
		if errors.Is(err, store.ErrNotFound) || (err == nil && row.HandoffID != r.HandoffID) {
			continue
		}
		if err != nil {
			return out, err
		}
		back, err := s.takeBack(ctx, row, fmt.Sprintf("%s was removed", name), false)
		if err != nil {
			return out, err
		}
		out.TakenBack = append(out.TakenBack, back)
	}
	until := s.opts.Now().Add(WorkerLeaveWindow).UTC().Format(time.RFC3339)
	if err := s.db.SetWorkerLeaving(ctx, workerInstanceID, until); err != nil {
		return out, err
	}
	s.publish(events.EventCardsRefreshed)
	if !reachable || client.Worker.RefusedAt != nil {
		return out, nil
	}
	leaving, err := s.opts.Workers.owedClient(ctx, workerInstanceID)
	if err != nil {
		return out, nil
	}
	if err := s.SendOwed(ctx, leaving); err != nil {
		log.Printf("removing worker %s: telling it to leave: %v", name, err)
		return out, nil
	}
	_, err = s.db.GetWorker(ctx, workerInstanceID)
	out.Left = errors.Is(err, store.ErrNotFound)
	return out, nil
}

// sendOwedDrops sends a worker every Drop the main owes it. A worker that
// does not answer ends it.
func (s *TaskHandoffService) sendOwedDrops(ctx context.Context, client *WorkerClient) error {
	owed, err := s.db.ListDropOwed(ctx, client.Worker.InstanceID)
	if err != nil {
		return err
	}
	for i := range owed {
		if err := s.drop(ctx, client, &owed[i]); err != nil {
			return err
		}
	}
	return nil
}

// leave tells a removed worker to unpair this main, after the calls owed
// it, then forgets the worker. A worker that no longer accepts this main
// has unpaired it already.
func (s *TaskHandoffService) leave(ctx context.Context, client *WorkerClient, owedErr error) error {
	err := owedErr
	if err == nil {
		var left HandoffLeft
		err = client.Call(ctx, http.MethodPost, HandoffLeavePath, nil, &left)
	}
	var refusal *WorkerRefusal
	if err != nil && !(errors.As(err, &refusal) && refusal.Status == http.StatusUnauthorized) {
		return err
	}
	return s.opts.Workers.forget(ctx, client.Worker.InstanceID)
}

// SendOwedDrops sends again each Drop a worker has not confirmed, while the
// connection to it stays up and so does not send it on connect. One that
// goes unanswered stays owed.
func (s *TaskHandoffService) SendOwedDrops(ctx context.Context) {
	owed, err := s.db.ListDropOwed(ctx, "")
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("handoffs: listing the drops owed: %v", err)
		}
		return
	}
	for i := range owed {
		if owed[i].WorkerUnreachableSince != nil || owed[i].WorkerRefusedAt != nil {
			continue
		}
		client, err := s.opts.Workers.owedClient(ctx, owed[i].WorkerInstanceID)
		if err != nil {
			continue
		}
		_ = s.drop(ctx, client, &owed[i])
	}
}

// leaveLapsed forgets a removed worker whose time to be told to leave ran
// out, and says whether it did.
func (s *TaskHandoffService) leaveLapsed(ctx context.Context, w *store.Worker) bool {
	if w.LeavingUntil == nil {
		return false
	}
	until, err := time.Parse(time.RFC3339, *w.LeavingUntil)
	if err != nil || s.opts.Now().Before(until) {
		return false
	}
	if err := s.opts.Workers.forget(ctx, w.InstanceID); err != nil {
		log.Printf("worker %s: forgetting it: %v", w.Name, err)
		return false
	}
	return true
}

// forget drops a worker's record and its token, and every call owed it.
func (s *WorkerService) forget(ctx context.Context, instanceID string) error {
	row, err := s.db.GetWorker(ctx, instanceID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.db.DeleteWorker(ctx, instanceID); err != nil {
		return err
	}
	if s.opts.Secrets != nil {
		if err := s.opts.Secrets.Remove(ctx, row.TokenSecret); err != nil {
			log.Printf("worker %s: removing its token: %v", row.Name, err)
		}
	}
	if s.opts.Bus != nil {
		s.opts.Bus.Publish(events.Event{Type: events.EventCardsRefreshed, At: time.Now()})
	}
	return nil
}
