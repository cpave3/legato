package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// AddTaskNote records a durable Markdown note on a task. Notes are kept beside
// the task's description, never in it, and are not published to a provider.
// source says who wrote it ("workflow", "agent").
func AddTaskNote(ctx context.Context, s *store.Store, taskID, body, source string) (store.TaskNote, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return store.TaskNote{}, fmt.Errorf("note body is empty")
	}
	if _, err := s.GetTask(ctx, taskID); err != nil {
		return store.TaskNote{}, fmt.Errorf("task %q not found: %w", taskID, err)
	}
	note := store.TaskNote{ID: workflowID("tn-"), TaskID: taskID, Body: body, Source: source}
	if err := s.InsertTaskNote(ctx, note); err != nil {
		return store.TaskNote{}, err
	}
	return note, nil
}
