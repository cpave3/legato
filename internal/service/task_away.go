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
