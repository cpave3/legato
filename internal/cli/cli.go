package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
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

// AgentSummary returns a tmux-formatted string showing agent session counts by activity state.
// If excludeTaskID is non-empty, that task's session is excluded from counts.
func AgentSummary(s *store.Store, excludeTaskID string) (string, error) {
	ctx := context.Background()

	working, waiting, idle, err := s.GetAgentActivityCounts(ctx, excludeTaskID)
	if err != nil {
		return "", fmt.Errorf("querying agent counts: %w", err)
	}

	var parts []string
	if working > 0 {
		parts = append(parts, fmt.Sprintf("#[fg=green]%d working", working))
	}
	if waiting > 0 {
		parts = append(parts, fmt.Sprintf("#[fg=yellow]%d waiting", waiting))
	}
	parts = append(parts, fmt.Sprintf("#[fg=colour245]%d idle", idle))

	return strings.Join(parts, " #[fg=colour240]· "), nil
}

// TaskLink associates a git branch with a task's PR metadata.
// If branch is empty, auto-detects the current git branch.
// Broadcasts an IPC notification to all running Legato instances.
func TaskLink(s *store.Store, taskID, branch, repo string) error {
	ctx := context.Background()

	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("task %q not found", taskID)
	}

	if branch == "" {
		var err error
		branch, err = detectBranch()
		if err != nil {
			return fmt.Errorf("could not detect branch (use --branch flag): %w", err)
		}
	}

	// Preserve existing PR metadata if already linked (don't overwrite enriched data).
	meta := &store.PRMeta{Branch: branch, Repo: repo}
	if task.PRMeta != nil {
		existing, _ := store.ParsePRMeta(task.PRMeta)
		if existing != nil && existing.PRNumber > 0 {
			// Already has PR data — just update repo/branch if provided.
			if repo != "" {
				existing.Repo = repo
			}
			if branch != "" {
				existing.Branch = branch
			}
			meta = existing
		}
	}

	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	rawStr := string(raw)

	if err := s.UpdatePRMeta(ctx, taskID, &rawStr); err != nil {
		return fmt.Errorf("linking branch: %w", err)
	}

	ipc.Broadcast(ipc.Message{
		Type:   "pr_linked",
		TaskID: taskID,
		Status: branch,
	})

	return nil
}

// TaskUnlink removes the branch/PR association from a task.
// Broadcasts an IPC notification to all running Legato instances.
func TaskUnlink(s *store.Store, taskID string) error {
	ctx := context.Background()

	if _, err := s.GetTask(ctx, taskID); err != nil {
		return fmt.Errorf("task %q not found", taskID)
	}

	if err := s.UpdatePRMeta(ctx, taskID, nil); err != nil {
		return fmt.Errorf("unlinking branch: %w", err)
	}

	ipc.Broadcast(ipc.Message{
		Type:   "pr_linked",
		TaskID: taskID,
	})

	return nil
}

// detectBranch returns the current git branch name.
func detectBranch() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("not in a git repository: %s", strings.TrimSpace(string(out)))
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" || branch == "HEAD" {
		return "", fmt.Errorf("detached HEAD — cannot auto-detect branch")
	}
	return branch, nil
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
