package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// TaskShow returns a task's content in a format suitable for agents and scripts.
// Supported formats are "description" (default), "full", and "json".
func TaskShow(s *store.Store, taskID, format string) (string, error) {
	ctx := context.Background()
	if format == "" {
		format = "description"
	}

	board := service.NewBoardService(s, nil)
	switch format {
	case "description":
		out, err := board.ExportCardContext(ctx, taskID, service.ExportFormatDescription)
		if err != nil {
			return "", taskShowError(taskID, err)
		}
		return out, nil
	case "full":
		out, err := board.ExportCardContext(ctx, taskID, service.ExportFormatFull)
		if err != nil {
			return "", taskShowError(taskID, err)
		}
		return out, nil
	case "json":
		detail, err := board.GetCard(ctx, taskID)
		if err != nil {
			return "", taskShowError(taskID, err)
		}
		out, err := json.Marshal(taskDetailJSONFromService(detail))
		if err != nil {
			return "", fmt.Errorf("encoding task: %w", err)
		}
		return string(out), nil
	default:
		return "", fmt.Errorf("unknown format %q; valid formats: description, full, json", format)
	}
}

func taskShowError(taskID string, err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("task %q not found", taskID)
	}
	return err
}

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
// workingDir, when non-empty, is recorded on the interval to help track
// time spent in directories as a proxy for project focus.
// Broadcasts an IPC notification to all running Legato instances.
// If notifier is non-nil and configured, a push notification is sent when
// the transition is working -> waiting or working -> idle and the task
// has notifications enabled.
func AgentState(s *store.Store, taskID, activity, workingDir string, pushNotifier, osNotifier service.Notifier) error {
	ctx := context.Background()

	oldActivity := ""
	if sess, err := s.GetAgentSessionByTaskID(ctx, taskID); err == nil {
		oldActivity = sess.Activity
	}

	if err := s.UpdateAgentActivity(ctx, taskID, activity); err != nil {
		return fmt.Errorf("updating agent activity: %w", err)
	}

	// Record state interval for duration tracking
	if err := s.RecordStateTransition(ctx, taskID, activity, workingDir); err != nil {
		return fmt.Errorf("recording state transition: %w", err)
	}

	if oldActivity == "working" && (activity == "waiting" || activity == "") {
		service.MaybeNotify(s, pushNotifier, osNotifier, taskID, activity)
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
// A non-empty sha records the head commit as a discovery anchor: PR polling
// resolves it via the exact commits/{sha}/pulls lookup, and the recorded
// link time rejects stale PRs from reused branch names.
// Broadcasts an IPC notification to all running Legato instances.
func TaskLink(s *store.Store, taskID, branch, repo, sha string) error {
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
		if existing != nil && existing.PRNumber > 0 && existing.Branch == branch {
			// Same branch, already has PR data — just update repo if provided.
			if repo != "" {
				existing.Repo = repo
			}
			meta = existing
		}
		// A different branch means a different PR: fall through to the fresh
		// meta so stale PR data (e.g. from spawn-time auto-link) is reset.
	}
	if sha != "" {
		meta.HeadSHA = sha
		meta.LinkedAt = time.Now().UTC().Format(time.RFC3339)
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

type taskDetailJSON struct {
	ID              string            `json:"id"`
	Title           string            `json:"title"`
	DescriptionMD   string            `json:"description_md"`
	Status          string            `json:"status"`
	Priority        string            `json:"priority"`
	Provider        string            `json:"provider"`
	RemoteID        string            `json:"remote_id"`
	RemoteMeta      map[string]string `json:"remote_meta"`
	WorkspaceID     *int              `json:"workspace_id"`
	PRMeta          *prMetaJSON       `json:"pr_meta"`
	SwarmActiveStep int               `json:"swarm_active_step"`
	SwarmStepNames  []string          `json:"swarm_step_names"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

type prMetaJSON struct {
	Repo           string `json:"repo"`
	Branch         string `json:"branch"`
	PRNumber       int    `json:"pr_number"`
	PRURL          string `json:"pr_url"`
	State          string `json:"state"`
	IsDraft        bool   `json:"is_draft"`
	ReviewDecision string `json:"review_decision"`
	CheckStatus    string `json:"check_status"`
	CommentCount   int    `json:"comment_count"`
}

func taskDetailJSONFromService(detail *service.CardDetail) taskDetailJSON {
	resp := taskDetailJSON{
		ID:              detail.ID,
		Title:           detail.Title,
		DescriptionMD:   detail.DescriptionMD,
		Status:          detail.Status,
		Priority:        detail.Priority,
		Provider:        detail.Provider,
		RemoteID:        detail.RemoteID,
		RemoteMeta:      detail.RemoteMeta,
		WorkspaceID:     detail.WorkspaceID,
		SwarmActiveStep: detail.SwarmActiveStep,
		SwarmStepNames:  detail.SwarmStepNames,
		CreatedAt:       detail.CreatedAt,
		UpdatedAt:       detail.UpdatedAt,
	}
	if resp.RemoteMeta == nil {
		resp.RemoteMeta = map[string]string{}
	}
	if resp.SwarmStepNames == nil {
		resp.SwarmStepNames = []string{}
	}
	if detail.PRMeta != nil {
		resp.PRMeta = &prMetaJSON{
			Repo:           detail.PRMeta.Repo,
			Branch:         detail.PRMeta.Branch,
			PRNumber:       detail.PRMeta.PRNumber,
			PRURL:          detail.PRMeta.PRURL,
			State:          detail.PRMeta.State,
			IsDraft:        detail.PRMeta.IsDraft,
			ReviewDecision: detail.PRMeta.ReviewDecision,
			CheckStatus:    detail.PRMeta.CheckStatus,
			CommentCount:   detail.PRMeta.CommentCount,
		}
	}
	return resp
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
