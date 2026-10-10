package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/chimera"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// TaskCreate creates a local task and returns its generated identity.
// workspaceName is optional and is resolved case-insensitively.
func TaskCreate(s *store.Store, title, description, status, priority string, workspaceName ...string) (*service.Card, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}

	ctx := context.Background()
	column := status
	if strings.TrimSpace(column) == "" {
		mappings, err := s.ListColumnMappings(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing columns: %w", err)
		}
		if len(mappings) == 0 {
			return nil, fmt.Errorf("cannot create task: no board columns configured")
		}
		column = mappings[0].ColumnName
	} else {
		var err error
		column, err = resolveColumn(ctx, s, column)
		if err != nil {
			return nil, err
		}
	}

	var workspaceID *int
	if len(workspaceName) > 0 {
		var err error
		workspaceID, err = resolveWorkspace(ctx, s, workspaceName[0])
		if err != nil {
			return nil, err
		}
	}
	board := service.NewBoardService(s, events.New())
	card, err := board.CreateTask(ctx, title, description, column, priority, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("creating task: %w", err)
	}
	ipc.Broadcast(ipc.Message{Type: "task_update", TaskID: card.ID, Status: column})
	return card, nil
}

type TaskListOptions struct {
	Status    string
	Workspace string
	Limit     int
}

type TaskListItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Priority  string `json:"priority,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}

// TaskList returns active, non-ephemeral tasks in board column and card order.
func TaskList(s *store.Store, opts TaskListOptions) ([]TaskListItem, error) {
	ctx := context.Background()
	columns, err := s.ListColumnMappings(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing columns: %w", err)
	}
	if opts.Status != "" {
		status, resolveErr := resolveColumn(ctx, s, opts.Status)
		if resolveErr != nil {
			return nil, resolveErr
		}
		columns = []store.ColumnMapping{{ColumnName: status}}
	}

	workspaces, err := s.ListWorkspaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing workspaces: %w", err)
	}
	workspaceNames := make(map[int]string, len(workspaces))
	view := store.WorkspaceView{Kind: store.ViewAll}
	for _, workspace := range workspaces {
		workspaceNames[workspace.ID] = workspace.Name
	}
	if opts.Workspace != "" && !strings.EqualFold(opts.Workspace, "all") {
		if strings.EqualFold(opts.Workspace, "none") || strings.EqualFold(opts.Workspace, "unassigned") {
			view = store.WorkspaceView{Kind: store.ViewUnassigned}
		} else {
			workspaceID, resolveErr := resolveWorkspace(ctx, s, opts.Workspace)
			if resolveErr != nil {
				return nil, resolveErr
			}
			view = store.WorkspaceView{Kind: store.ViewWorkspace, WorkspaceID: *workspaceID}
		}
	}

	result := make([]TaskListItem, 0)
	for _, column := range columns {
		tasks, listErr := s.ListTasksByStatusAndWorkspace(ctx, column.ColumnName, view)
		if listErr != nil {
			return nil, fmt.Errorf("listing %s tasks: %w", column.ColumnName, listErr)
		}
		for _, task := range tasks {
			item := TaskListItem{ID: task.ID, Title: task.Title, Status: task.Status, Priority: task.Priority}
			if task.Provider != nil {
				item.Provider = *task.Provider
			}
			if task.WorkspaceID != nil {
				item.Workspace = workspaceNames[*task.WorkspaceID]
			}
			result = append(result, item)
			if opts.Limit > 0 && len(result) >= opts.Limit {
				return result, nil
			}
		}
	}
	return result, nil
}

// TaskDescription replaces a task's Legato-owned description. Provider-owned
// details remain authoritative and are not modified.
func TaskDescription(s *store.Store, taskID, description string) error {
	if strings.TrimSpace(taskID) == "" {
		return fmt.Errorf("task ID is required")
	}
	board := service.NewBoardService(s, events.New())
	if err := board.UpdateTaskDescription(context.Background(), taskID, description); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("task %q not found: %w", taskID, err)
		}
		return err
	}
	ipc.Broadcast(ipc.Message{Type: "task_update", TaskID: taskID})
	return nil
}

// TaskWorkspace assigns a task to a workspace by name. The special names
// "none" and "unassigned" clear the task's workspace.
func TaskWorkspace(s *store.Store, taskID, workspaceName string) error {
	if strings.TrimSpace(taskID) == "" {
		return fmt.Errorf("task ID is required")
	}
	ctx := context.Background()
	workspaceID, err := resolveWorkspace(ctx, s, workspaceName)
	if err != nil {
		return err
	}
	board := service.NewBoardService(s, events.New())
	if err := board.UpdateTaskWorkspace(ctx, taskID, workspaceID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("task %q not found: %w", taskID, err)
		}
		return err
	}
	ipc.Broadcast(ipc.Message{Type: "task_update", TaskID: taskID})
	return nil
}

// TaskTitle replaces the title of a local task. Provider-backed task titles
// remain authoritative in their remote system.
func TaskTitle(s *store.Store, taskID, title string) error {
	if strings.TrimSpace(taskID) == "" {
		return fmt.Errorf("task ID is required")
	}
	board := service.NewBoardService(s, events.New())
	if err := board.UpdateTaskTitle(context.Background(), taskID, title); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("task %q not found: %w", taskID, err)
		}
		return err
	}
	ipc.Broadcast(ipc.Message{Type: "task_update", TaskID: taskID})
	return nil
}

type taskWorktreeRecorder interface {
	Record(ctx context.Context, taskID string, meta *store.TaskWorktree) error
}

// TaskWorktreeSet associates durable worktree metadata with a task.
func TaskWorktreeSet(s *store.Store, taskID string, meta store.TaskWorktree, recorders ...taskWorktreeRecorder) error {
	if strings.TrimSpace(taskID) == "" {
		return fmt.Errorf("task ID is required")
	}
	if strings.TrimSpace(meta.Path) == "" {
		return fmt.Errorf("worktree path is required")
	}
	if strings.Contains(meta.Path, "\n") || (meta.PrimaryDir != "" && strings.Contains(meta.PrimaryDir, "\n")) {
		return fmt.Errorf("worktree path and primary directory must not contain newlines")
	}
	if len(recorders) > 0 && recorders[0] != nil {
		if err := recorders[0].Record(context.Background(), taskID, &meta); err != nil {
			return fmt.Errorf("set worktree for task %s: %w", taskID, err)
		}
	} else if err := s.SetTaskWorktree(context.Background(), taskID, &meta); err != nil {
		return fmt.Errorf("set worktree for task %s: %w", taskID, err)
	}
	ipc.Broadcast(ipc.Message{Type: "worktree_changed", TaskID: taskID})
	return nil
}

// TaskWorktreeClear removes durable worktree metadata and notifies running clients.
func TaskWorktreeClear(s *store.Store, taskID string) error {
	if strings.TrimSpace(taskID) == "" {
		return fmt.Errorf("task ID is required")
	}
	if err := s.SetTaskWorktree(context.Background(), taskID, nil); err != nil {
		return fmt.Errorf("clear worktree for task %s: %w", taskID, err)
	}
	ipc.Broadcast(ipc.Message{Type: "worktree_changed", TaskID: taskID})
	return nil
}

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

func TaskNotes(s *store.Store, taskID string) ([]store.TaskNote, error) {
	if _, err := s.GetTask(context.Background(), taskID); err != nil {
		return nil, taskShowError(taskID, err)
	}
	return s.ListTaskNotes(context.Background(), taskID)
}

func taskShowError(taskID string, err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("task %q not found: %w", taskID, err)
	}
	return err
}

// TaskUpdate moves a task to the column matching the given status name (case-insensitive).
// Broadcasts an IPC notification to all running Legato instances.
func TaskUpdate(s *store.Store, taskID, status string) error {
	return TaskUpdateFields(s, taskID, TaskUpdateOptions{Status: &status})
}

// TaskUpdateOptions contains the fields task update may change. Pointer fields
// distinguish an omitted option from an explicitly empty value.
type TaskUpdateOptions struct {
	Status      *string
	Title       *string
	Description *string
	Workspace   *string
}

var broadcastTaskUpdate = ipc.Broadcast

// TaskUpdateFields validates and applies a task update as one durable write,
// then broadcasts one refresh notification.
func TaskUpdateFields(s *store.Store, taskID string, opts TaskUpdateOptions) error {
	ctx := context.Background()

	if strings.TrimSpace(taskID) == "" {
		return fmt.Errorf("task ID is required")
	}
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("task %q not found: %w", taskID, err)
	}

	if opts.Status != nil {
		column, err := resolveColumn(ctx, s, *opts.Status)
		if err != nil {
			return err
		}
		task.Status = column
	}

	if opts.Workspace != nil {
		workspaceID, err := resolveWorkspace(ctx, s, *opts.Workspace)
		if err != nil {
			return err
		}
		task.WorkspaceID = workspaceID
	}

	if opts.Title != nil {
		if *opts.Title == "" {
			return fmt.Errorf("title cannot be empty")
		}
		if task.Provider != nil {
			return fmt.Errorf("cannot edit title of remote task %s", taskID)
		}
		task.Title = *opts.Title
	}

	if opts.Description != nil {
		task.Description = *opts.Description
		task.DescriptionMD = *opts.Description
	}

	task.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.UpdateTask(ctx, *task); err != nil {
		return fmt.Errorf("updating task: %w", err)
	}

	broadcastTaskUpdate(ipc.Message{
		Type:   "task_update",
		TaskID: taskID,
		Status: task.Status,
	})

	return nil
}

// TaskNote appends a timestamped note to a task's description.
// Broadcasts an IPC notification to all running Legato instances.
func TaskNote(s *store.Store, taskID, message string) error {
	ctx := context.Background()

	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("task %q not found: %w", taskID, err)
	}
	if task.Provider != nil {
		return fmt.Errorf("cannot append note to remote task %s", taskID)
	}

	timestamp := time.Now().UTC().Format("2006-01-02 15:04")
	note := fmt.Sprintf("\n\n---\n**[%s]** %s", timestamp, message)

	board := service.NewBoardService(s, events.New())
	if err := board.UpdateTaskDescription(ctx, taskID, task.Description+note); err != nil {
		return fmt.Errorf("updating task: %w", err)
	}

	ipc.Broadcast(ipc.Message{
		Type:    "task_note",
		TaskID:  taskID,
		Content: message,
	})

	return nil
}

// AgentStateReport is an activity update from an AI-tool integration.
type AgentStateReport struct {
	TaskID      string
	ExecutionID string
	Activity    string
	WorkingDir  string
	SessionID   string
	Event       string
}

// AgentState applies an activity report. Reports from Chimera child sessions
// produce child readiness or error notifications without mutating the parent session.
func AgentState(s *store.Store, report AgentStateReport, pushNotifier, osNotifier service.Notifier) error {
	ctx := context.Background()
	session, err := s.GetAgentSessionByTaskID(ctx, report.TaskID)
	if err != nil {
		return fmt.Errorf("getting agent session: %w", err)
	}
	if report.SessionID != "" && session.ChimeraSessionID == nil {
		return fmt.Errorf("resolving Chimera session %q for task %s: parent session ID is unavailable", report.SessionID, report.TaskID)
	}
	if report.SessionID != "" && report.SessionID != *session.ChimeraSessionID {
		if session.ChimeraEndpoint == nil {
			return fmt.Errorf("resolving Chimera session %q for task %s: parent endpoint is unavailable", report.SessionID, report.TaskID)
		}
		subagent, err := findChimeraSubagent(ctx, *session.ChimeraEndpoint, *session.ChimeraSessionID, report.SessionID, 3)
		if err != nil {
			return fmt.Errorf("resolving Chimera session %q for task %s: %w", report.SessionID, report.TaskID, err)
		}
		if subagent == nil {
			return fmt.Errorf("resolving Chimera session %q for task %s: subagent not found", report.SessionID, report.TaskID)
		}
		if report.Activity == store.ActivityError || (report.Activity == "" && (report.Event == "" || report.Event == "SessionEnd")) {
			service.MaybeNotifyAgent(s, pushNotifier, osNotifier, service.NotificationContext{TaskID: report.TaskID, Actor: subagent.Purpose}, report.Activity)
		}
		return nil
	}

	if report.ExecutionID != "" && session.ExecutionID != report.ExecutionID {
		return fmt.Errorf("execution %q does not own running task session %s", report.ExecutionID, report.TaskID)
	}
	if err := s.UpdateAgentActivity(ctx, report.TaskID, report.Activity); err != nil {
		return fmt.Errorf("updating agent activity: %w", err)
	}
	// Legacy hooks omit execution identity. Keep the live operational status
	// useful, but never infer an execution or write reporting activity.
	if report.ExecutionID != "" {
		// Waiting on background work is not a turn of the agent's own: for
		// reporting it ends the span in progress, as going idle does.
		reported := report.Activity
		if reported == store.ActivityBackground {
			reported = ""
		}
		if err := s.RecordExecutionActivity(ctx, report.ExecutionID, reported, report.WorkingDir); err != nil {
			return fmt.Errorf("recording execution activity: %w", err)
		}
	}
	if (session.Activity == store.ActivityWorking && (report.Activity == store.ActivityWaiting || report.Activity == "")) ||
		(report.Activity == store.ActivityError && session.Activity != store.ActivityError) {
		service.MaybeNotify(s, pushNotifier, osNotifier, report.TaskID, report.Activity)
	}
	ipc.Broadcast(ipc.Message{Type: "agent_state", TaskID: report.TaskID, Status: report.Activity})
	return nil
}

func findChimeraSubagent(ctx context.Context, endpoint, parentID, reportingID string, depth int) (*chimera.Subagent, error) {
	subagents, err := chimera.NewClient(endpoint).ListSubagents(ctx, parentID)
	if err != nil {
		return nil, err
	}
	for i := range subagents {
		if subagents[i].SessionID == reportingID {
			return &subagents[i], nil
		}
	}
	if depth <= 1 {
		return nil, nil
	}
	for _, child := range subagents {
		if child.URL == "" {
			continue
		}
		found, err := findChimeraSubagent(ctx, child.URL, child.SessionID, reportingID, depth-1)
		if err != nil {
			return nil, err
		}
		if found != nil {
			return found, nil
		}
	}
	return nil, nil
}

// AgentSummary returns a tmux-formatted string showing agent session counts by activity state.
// If excludeTaskID is non-empty, that task's session is excluded from counts.
func AgentSummary(s *store.Store, excludeTaskID string) (string, error) {
	ctx := context.Background()

	working, waiting, errors, idle, err := s.GetAgentActivityCounts(ctx, excludeTaskID)
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
	if errors > 0 {
		parts = append(parts, fmt.Sprintf("#[fg=red]%d error", errors))
	}
	parts = append(parts, fmt.Sprintf("#[fg=colour245]%d idle", idle))

	return strings.Join(parts, " #[fg=colour240]· "), nil
}

// TaskLink associates durable repository context with a task.
// If branch is empty, auto-detects the current git branch.
// A non-empty sha records the head commit as a discovery anchor: PR polling
// resolves it via the exact commits/{sha}/pulls lookup, and the recorded
// link time rejects stale PRs from reused branch names.
// Broadcasts an IPC notification to all running Legato instances.
func TaskLink(s *store.Store, taskID, branch, repo, sha string) error {
	ctx := context.Background()

	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("task %q not found: %w", taskID, err)
	}

	if branch == "" {
		var err error
		branch, err = detectBranch()
		if err != nil {
			return fmt.Errorf("could not detect branch (use --branch flag): %w", err)
		}
	}

	existing, _ := store.ParseCodeContext(task.CodeContext)
	if existing == nil {
		existing = &store.CodeContext{}
	}
	branchChanged := existing.Branch != "" && existing.Branch != branch
	existing.Branch = branch
	existing.BranchSource = "manual"
	if repo != "" {
		existing.Repo = repo
		existing.RepoSource = "manual"
	}
	existing.HeadSHA = sha
	existing.LinkedAt = time.Now().UTC().Format(time.RFC3339)
	existing.UpdatedAt = existing.LinkedAt
	if err := s.SetCodeContext(ctx, taskID, existing); err != nil {
		return fmt.Errorf("linking branch: %w", err)
	}
	if branchChanged {
		if err := s.UpdatePRMeta(ctx, taskID, nil); err != nil {
			return fmt.Errorf("resetting stale PR association: %w", err)
		}
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
		return fmt.Errorf("task %q not found: %w", taskID, err)
	}

	if err := s.UpdatePRMeta(ctx, taskID, nil); err != nil {
		return fmt.Errorf("unlinking PR: %w", err)
	}
	if err := s.SetCodeContext(ctx, taskID, nil); err != nil {
		return fmt.Errorf("clearing repository context: %w", err)
	}

	ipc.Broadcast(ipc.Message{
		Type:   "pr_linked",
		TaskID: taskID,
	})

	return nil
}

type taskDetailJSON struct {
	ID                  string            `json:"id"`
	Title               string            `json:"title"`
	DescriptionMD       string            `json:"description_md"`
	LocalDescriptionMD  string            `json:"local_description_md"`
	RemoteDescriptionMD string            `json:"remote_description_md"`
	Status              string            `json:"status"`
	Priority            string            `json:"priority"`
	Provider            string            `json:"provider"`
	RemoteID            string            `json:"remote_id"`
	RemoteMeta          map[string]string `json:"remote_meta"`
	WorkspaceID         *int              `json:"workspace_id"`
	PRMeta              *prMetaJSON       `json:"pr_meta"`
	CodeContext         *codeContextJSON  `json:"code_context"`
	SwarmActiveStep     int               `json:"swarm_active_step"`
	SwarmStepNames      []string          `json:"swarm_step_names"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
	Notes               []store.TaskNote  `json:"notes"`
}

type codeContextJSON struct {
	Repo         string `json:"repo"`
	Branch       string `json:"branch"`
	HeadSHA      string `json:"head_sha"`
	WorkingDir   string `json:"working_dir"`
	BaseBranch   string `json:"base_branch"`
	RepoSource   string `json:"repo_source"`
	BranchSource string `json:"branch_source"`
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
	Pinned         bool   `json:"pinned"`
	MatchMethod    string `json:"match_method"`
	SyncOutcome    string `json:"sync_outcome"`
	SyncError      string `json:"sync_error"`
}

func taskDetailJSONFromService(detail *service.CardDetail) taskDetailJSON {
	resp := taskDetailJSON{
		ID:                  detail.ID,
		Title:               detail.Title,
		DescriptionMD:       detail.DescriptionMD,
		LocalDescriptionMD:  detail.LocalDescriptionMD,
		RemoteDescriptionMD: detail.RemoteDescriptionMD,
		Status:              detail.Status,
		Priority:            detail.Priority,
		Provider:            detail.Provider,
		RemoteID:            detail.RemoteID,
		RemoteMeta:          detail.RemoteMeta,
		WorkspaceID:         detail.WorkspaceID,
		SwarmActiveStep:     detail.SwarmActiveStep,
		SwarmStepNames:      detail.SwarmStepNames,
		CreatedAt:           detail.CreatedAt,
		UpdatedAt:           detail.UpdatedAt,
		Notes:               detail.Notes,
	}
	if resp.RemoteMeta == nil {
		resp.RemoteMeta = map[string]string{}
	}
	if resp.SwarmStepNames == nil {
		resp.SwarmStepNames = []string{}
	}
	if detail.CodeContext != nil {
		resp.CodeContext = &codeContextJSON{
			Repo: detail.CodeContext.Repo, Branch: detail.CodeContext.Branch, HeadSHA: detail.CodeContext.HeadSHA,
			WorkingDir: detail.CodeContext.WorkingDir, BaseBranch: detail.CodeContext.BaseBranch,
			RepoSource: detail.CodeContext.RepoSource, BranchSource: detail.CodeContext.BranchSource,
		}
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
			Pinned:         detail.PRMeta.Pinned,
			MatchMethod:    detail.PRMeta.MatchMethod,
			SyncOutcome:    detail.PRMeta.SyncOutcome,
			SyncError:      detail.PRMeta.SyncError,
		}
	}
	return resp
}

type workspaceJSON struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// WorkspaceList returns configured workspaces as newline-delimited names or
// as a JSON array.
func WorkspaceList(s *store.Store, format string) (string, error) {
	workspaces, err := s.ListWorkspaces(context.Background())
	if err != nil {
		return "", fmt.Errorf("listing workspaces: %w", err)
	}
	switch format {
	case "", "text":
		names := make([]string, len(workspaces))
		for i, workspace := range workspaces {
			names[i] = workspace.Name
		}
		return strings.Join(names, "\n"), nil
	case "json":
		result := make([]workspaceJSON, len(workspaces))
		for i, workspace := range workspaces {
			result[i] = workspaceJSON{ID: workspace.ID, Name: workspace.Name}
			if workspace.Color != nil {
				result[i].Color = *workspace.Color
			}
		}
		data, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("encoding workspaces: %w", err)
		}
		return string(data), nil
	default:
		return "", fmt.Errorf("unknown format %q; valid formats: text, json", format)
	}
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
	return "", fmt.Errorf("%w: unknown status %q; valid statuses: %s", ErrInvalidInput, status, strings.Join(names, ", "))
}

func resolveWorkspace(ctx context.Context, s *store.Store, name string) (*int, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.EqualFold(name, "none") || strings.EqualFold(name, "unassigned") {
		return nil, nil
	}

	workspaces, err := s.ListWorkspaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing workspaces: %w", err)
	}
	for _, workspace := range workspaces {
		if strings.EqualFold(workspace.Name, name) {
			id := workspace.ID
			return &id, nil
		}
	}

	names := make([]string, len(workspaces))
	for i, workspace := range workspaces {
		names[i] = workspace.Name
	}
	return nil, fmt.Errorf("%w: unknown workspace %q; valid workspaces: %s", ErrInvalidInput, name, strings.Join(names, ", "))
}
