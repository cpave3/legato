package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/cpave3/legato/internal/engine/store"
)

// ErrTaskAway: the task is away on a worker instance, so this main refuses
// to change it: edits, moves, agent starts and another handoff.
var ErrTaskAway = errors.New("the task is away on a worker")

// TaskAwayError says which worker holds an away task. It is ErrTaskAway.
type TaskAwayError struct {
	TaskID string
	Worker string
}

func (e *TaskAwayError) Error() string {
	return fmt.Sprintf("task %s is away on %s: it can change only there until it comes back", e.TaskID, e.Worker)
}

func (e *TaskAwayError) Is(target error) bool { return target == ErrTaskAway }

// RefuseAway is a *TaskAwayError when the task is away on a worker, else
// nil. Every way of changing a task on a main asks it first: the board
// service's edits and moves, the agent service's starts, the CLI's task
// update and another handoff.
func RefuseAway(ctx context.Context, db *store.Store, taskID string) error {
	away, err := db.OpenAwayTask(ctx, taskID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && away.State != store.AwayAway) {
		return nil
	}
	if err != nil {
		return err
	}
	worker := away.WorkerName
	if worker == "" {
		worker = away.WorkerInstanceID
	}
	return &TaskAwayError{TaskID: taskID, Worker: worker}
}

// ErrTaskHandedOff: the task is a card a main instance handed to this
// worker, with its handoff open, so the worker refuses to archive or delete
// it; the main returns or recalls it. Moves, edits, notes and its agent
// work as usual.
var ErrTaskHandedOff = errors.New("the task belongs to a main instance")

// TaskHandedOffError names the main instance a handed-off card belongs to.
// It is ErrTaskHandedOff.
type TaskHandedOffError struct {
	TaskID string
	Main   string
}

func (e *TaskHandedOffError) Error() string {
	return fmt.Sprintf("this task belongs to %s; return or recall it from there", e.Main)
}

func (e *TaskHandedOffError) Is(target error) bool { return target == ErrTaskHandedOff }

// RefuseHandedOff is a *TaskHandedOffError when the task is a card a main
// handed to this worker and its handoff is open, else nil. The board
// service asks it before archiving or deleting a card.
func RefuseHandedOff(ctx context.Context, db *store.Store, taskID string) error {
	h, err := db.OpenHandoffForTask(ctx, taskID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	main := h.MainName
	if main == "" {
		main = h.MainInstanceID
	}
	return &TaskHandedOffError{TaskID: taskID, Main: main}
}
