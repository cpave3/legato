package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
)

// TaskUpdate moves a task to the column matching the given status name (case-insensitive).
// Broadcasts an IPC notification to all running Legato instances.
func TaskUpdate(s *store.Store, taskID, status string) error {
	ctx := context.Background()

	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("task %q not found", taskID)
	}

	column, err := resolveColumn(ctx, s, status)
	if err != nil {
		return err
	}

	task.Status = column
	task.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.UpdateTask(ctx, *task); err != nil {
		return fmt.Errorf("updating task: %w", err)
	}

	ipc.Broadcast(ipc.Message{
		Type:   "task_update",
		TaskID: taskID,
		Status: column,
	})

	return nil
}

// TaskNote appends a timestamped note to a task's description.
// Broadcasts an IPC notification to all running Legato instances.
func TaskNote(s *store.Store, taskID, message string) error {
	ctx := context.Background()

	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("task %q not found", taskID)
	}

	timestamp := time.Now().UTC().Format("2006-01-02 15:04")
	note := fmt.Sprintf("\n\n---\n**[%s]** %s", timestamp, message)

	task.Description = task.Description + note
	task.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.UpdateTask(ctx, *task); err != nil {
		return fmt.Errorf("updating task: %w", err)
	}

	ipc.Broadcast(ipc.Message{
		Type:    "task_note",
		TaskID:  taskID,
		Content: message,
	})

	return nil
}

// AgentState updates the activity state of an agent session for a task.
// Valid activities: "working", "waiting", "" (clear).
// Broadcasts an IPC notification to all running Legato instances.
func AgentState(s *store.Store, taskID, activity string) error {
	ctx := context.Background()

	if err := s.UpdateAgentActivity(ctx, taskID, activity); err != nil {
		return fmt.Errorf("updating agent activity: %w", err)
	}

	// Record state interval for duration tracking
	if err := s.RecordStateTransition(ctx, taskID, activity); err != nil {
		return fmt.Errorf("recording state transition: %w", err)
	}

	ipc.Broadcast(ipc.Message{
		Type:   "agent_state",
		TaskID: taskID,
		Status: activity,
	})

	return nil
}

// resolveColumn finds the column name matching the given status (case-insensitive).
func resolveColumn(ctx context.Context, s *store.Store, status string) (string, error) {
	mappings, err := s.ListColumnMappings(ctx)
	if err != nil {
		return "", fmt.Errorf("listing columns: %w", err)
	}

	for _, m := range mappings {
		if strings.EqualFold(m.ColumnName, status) {
			return m.ColumnName, nil
		}
	}

	names := make([]string, len(mappings))
	for i, m := range mappings {
		names[i] = m.ColumnName
	}
	return "", fmt.Errorf("unknown status %q; valid statuses: %s", status, strings.Join(names, ", "))
}
