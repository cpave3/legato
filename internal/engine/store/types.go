package store

import "encoding/json"

// PRMeta holds PR tracking metadata stored as JSON in the pr_meta column.
type PRMeta struct {
	Repo           string `json:"repo,omitempty"` // owner/repo format
	Branch         string `json:"branch"`
	PRNumber       int    `json:"pr_number,omitempty"`
	PRURL          string `json:"pr_url,omitempty"`
	State          string `json:"state,omitempty"` // OPEN, MERGED, CLOSED, or ""
	IsDraft        bool   `json:"is_draft,omitempty"`
	ReviewDecision string `json:"review_decision,omitempty"` // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, or ""
	CheckStatus    string `json:"check_status,omitempty"`    // pass, fail, pending, or ""
	CommentCount   int    `json:"comment_count,omitempty"`
	UpdatedAt      string `json:"updated_at,omitempty"` // RFC3339
	HeadSHA        string `json:"head_sha,omitempty"`   // commit SHA recorded at link time — anchors PR discovery
	LinkedAt       string `json:"linked_at,omitempty"`  // RFC3339 time the link was created — PRs created before this are rejected during discovery
}

// MarshalPRMeta serializes PRMeta to a JSON string pointer for storage.
func MarshalPRMeta(m *PRMeta) (*string, error) {
	if m == nil {
		return nil, nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	s := string(b)
	return &s, nil
}

// ParsePRMeta deserializes a pr_meta JSON string into a PRMeta struct.
func ParsePRMeta(raw *string) (*PRMeta, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	var m PRMeta
	if err := json.Unmarshal([]byte(*raw), &m); err != nil {
		return nil, err
	}
	return &m, nil
}

type Task struct {
	ID                 string  `db:"id"`
	Title              string  `db:"title"`
	Description        string  `db:"description"`
	DescriptionMD      string  `db:"description_md"`
	Status             string  `db:"status"`
	Priority           string  `db:"priority"`
	SortOrder          int     `db:"sort_order"`
	Provider           *string `db:"provider"`
	RemoteID           *string `db:"remote_id"`
	RemoteMeta         *string `db:"remote_meta"`
	PRMeta             *string `db:"pr_meta"`
	WorkspaceID        *int    `db:"workspace_id"`
	ArchivedAt         *string `db:"archived_at"`
	Ephemeral          bool    `db:"ephemeral"`
	SwarmWorkingDir    *string `db:"swarm_working_dir"`
	SwarmActiveStep    int     `db:"swarm_active_step"`
	ChimeraSessionID   *string `db:"chimera_session_id"`
	Group              *string `db:"task_group"`
	WorktreePrimaryDir *string `db:"worktree_primary_dir"`
	WorktreePath       *string `db:"worktree_path"`
	WorktreeBranch     *string `db:"worktree_branch"`
	WorktreeBaseBranch *string `db:"worktree_base_branch"`
	CreatedAt          string  `db:"created_at"`
	UpdatedAt          string  `db:"updated_at"`
}

// TaskWorktree is the durable worktree associated with a task.
type TaskWorktree struct {
	PrimaryDir string
	Path       string
	Branch     string
	BaseBranch string
}

type Workspace struct {
	ID        int     `db:"id"`
	Name      string  `db:"name"`
	Color     *string `db:"color"`
	SortOrder int     `db:"sort_order"`
}

type ColumnMapping struct {
	ID               int    `db:"id"`
	ColumnName       string `db:"column_name"`
	RemoteStatuses   string `db:"remote_statuses"`
	RemoteTransition string `db:"remote_transition"`
	SortOrder        int    `db:"sort_order"`
}

type SyncLogEntry struct {
	ID        int    `db:"id"`
	TaskID    string `db:"task_id"`
	Action    string `db:"action"`
	Detail    string `db:"detail"`
	CreatedAt string `db:"created_at"`
}

type StateInterval struct {
	ID         int     `db:"id"`
	TaskID     string  `db:"task_id"`
	State      string  `db:"state"`
	StartedAt  string  `db:"started_at"`
	EndedAt    *string `db:"ended_at"`
	WorkingDir *string `db:"working_dir"`
}

type AgentSession struct {
	ID           int     `db:"id"`
	TaskID       string  `db:"task_id"`
	TmuxSession  string  `db:"tmux_session"`
	Command      string  `db:"command"`
	AgentKind    string  `db:"agent_kind"`
	Status       string  `db:"status"`
	Activity     string  `db:"activity"`
	Role         string  `db:"role"`
	ParentTaskID *string `db:"parent_task_id"`
	SubtaskID    *string `db:"subtask_id"`
	StartedAt    string  `db:"started_at"`
	EndedAt      *string `db:"ended_at"`
}

// SwarmEvent is a single conductor-bound event written to the swarm_events
// inbox. Events are pulled via `legato swarm inbox <parent-id>`, which marks
// them acked. See SwarmService for the producer side.
type SwarmEvent struct {
	ID           int     `db:"id"`
	ParentTaskID string  `db:"parent_task_id"`
	SubtaskID    *string `db:"subtask_id"`
	Kind         string  `db:"kind"`
	WorkerTitle  string  `db:"worker_title"`
	Payload      string  `db:"payload"`
	CreatedAt    string  `db:"created_at"`
	AckedAt      *string `db:"acked_at"`
}

// PendingPlanEntry is a persisted proposed plan awaiting HITL approval.
type PendingPlanEntry struct {
	ID           int    `db:"id"`
	ParentTaskID string `db:"parent_task_id"`
	PlanPath     string `db:"plan_path"`
	ReplySocket  string `db:"reply_socket"`
	CreatedAt    string `db:"created_at"`
}

// ReviewTour is a per-task review packet header. A task may have multiple
// named tours. Status lifecycle: capturing → ready → reviewed.
type ReviewTour struct {
	ID              string  `db:"id" json:"id"`
	TaskID          string  `db:"task_id" json:"task_id"`
	Name            string  `db:"name" json:"name"`
	Status          string  `db:"status" json:"status"`
	Summary         string  `db:"summary" json:"summary"`
	BaseSHA         string  `db:"base_sha" json:"base_sha"`
	HeadSHA         string  `db:"head_sha" json:"head_sha"`
	RepositoryPath  string  `db:"repository_path" json:"repository_path"`
	LastReviewedSHA string  `db:"last_reviewed_sha" json:"last_reviewed_sha"`
	ReadyAt         *string `db:"ready_at" json:"ready_at,omitempty"`
	CreatedAt       string  `db:"created_at" json:"created_at"`
	UpdatedAt       string  `db:"updated_at" json:"updated_at"`
}

// ReviewStep is one reviewable unit of a tour: a commit, the synthetic dirty
// step, or a file-anchored note. Identity is the generated ID — never the SHA —
// so annotations and transcript survive re-syncs.
type ReviewStep struct {
	ID               string  `db:"id" json:"id"`
	TaskID           string  `db:"task_id" json:"task_id"`
	TourID           string  `db:"tour_id" json:"tour_id"`
	Kind             string  `db:"kind" json:"kind"` // commit|dirty|note|chapter
	CommitSHA        string  `db:"commit_sha" json:"commit_sha"`
	Files            string  `db:"files" json:"files"` // JSON array of paths
	Title            string  `db:"title" json:"title"`
	Narration        string  `db:"narration" json:"narration"`
	Risk             string  `db:"risk" json:"risk"` // ''|low|medium|high|unsure
	OrderHint        *int    `db:"order_hint" json:"order_hint,omitempty"`
	Seq              int     `db:"seq" json:"seq"`
	SubtaskID        string  `db:"subtask_id" json:"subtask_id"`
	DirtyFingerprint string  `db:"dirty_fingerprint" json:"dirty_fingerprint"`
	ReviewedAt       *string `db:"reviewed_at" json:"reviewed_at,omitempty"`
	OrphanedAt       *string `db:"orphaned_at" json:"orphaned_at,omitempty"`
	CreatedAt        string  `db:"created_at" json:"created_at"`
	UpdatedAt        string  `db:"updated_at" json:"updated_at"`
}

// ReviewHunkNote is a durable annotation attached to hunk content within a
// review step's file diff.
type ReviewHunkNote struct {
	ID         string `db:"id" json:"id"`
	TaskID     string `db:"task_id" json:"task_id"`
	TourID     string `db:"tour_id" json:"tour_id"`
	StepID     string `db:"step_id" json:"step_id"`
	FilePath   string `db:"file_path" json:"file_path"`
	HunkAnchor string `db:"hunk_anchor" json:"hunk_anchor"`
	LineStart  *int   `db:"line_start" json:"line_start,omitempty"`
	LineEnd    *int   `db:"line_end" json:"line_end,omitempty"`
	LineAnchor string `db:"line_anchor" json:"line_anchor,omitempty"`
	Body       string `db:"body" json:"body"`
	CreatedAt  string `db:"created_at" json:"created_at"`
	UpdatedAt  string `db:"updated_at" json:"updated_at"`
}

// ReviewChapterHunk assigns one base-to-head diff hunk to a chapter step.
type ReviewChapterHunk struct {
	ID         string `db:"id" json:"id"`
	TaskID     string `db:"task_id" json:"task_id"`
	TourID     string `db:"tour_id" json:"tour_id"`
	StepID     string `db:"step_id" json:"step_id"`
	FilePath   string `db:"file_path" json:"file_path"`
	HunkAnchor string `db:"hunk_anchor" json:"hunk_anchor"`
	Seq        int    `db:"seq" json:"seq"`
	Generated  bool   `db:"generated" json:"generated"`
	CreatedAt  string `db:"created_at" json:"created_at"`
	UpdatedAt  string `db:"updated_at" json:"updated_at"`
}

// ReviewMessage is one Q&A transcript entry attached to a review step.
type ReviewMessage struct {
	ID          int     `db:"id" json:"id"`
	TaskID      string  `db:"task_id" json:"task_id"`
	TourID      string  `db:"tour_id" json:"tour_id"`
	StepID      string  `db:"step_id" json:"step_id"`
	Kind        string  `db:"kind" json:"kind"`     // question|answer
	Author      string  `db:"author" json:"author"` // user|agent
	Body        string  `db:"body" json:"body"`
	DeliveredAt *string `db:"delivered_at" json:"delivered_at,omitempty"`
	CreatedAt   string  `db:"created_at" json:"created_at"`
}

// Plan is a durable planning artifact attached to a task.
type Plan struct {
	ID             string  `db:"id" json:"id"`
	TaskID         string  `db:"task_id" json:"task_id"`
	Name           string  `db:"name" json:"name"`
	Title          string  `db:"title" json:"title"`
	Summary        string  `db:"summary" json:"summary"`
	Status         string  `db:"status" json:"status"`
	LatestRevision int     `db:"latest_revision" json:"latest_revision"`
	ApprovedAt     *string `db:"approved_at" json:"approved_at,omitempty"`
	RejectedAt     *string `db:"rejected_at" json:"rejected_at,omitempty"`
	CreatedAt      string  `db:"created_at" json:"created_at"`
	UpdatedAt      string  `db:"updated_at" json:"updated_at"`
}

type PlanRevision struct {
	ID           string `db:"id" json:"id"`
	PlanID       string `db:"plan_id" json:"plan_id"`
	Revision     int    `db:"revision" json:"revision"`
	Markdown     string `db:"markdown" json:"markdown"`
	ManifestJSON string `db:"manifest_json" json:"manifest_json"`
	CreatedAt    string `db:"created_at" json:"created_at"`
}

type PlanQuestion struct {
	ID              string `db:"id" json:"id"`
	PlanID          string `db:"plan_id" json:"plan_id"`
	RevisionID      string `db:"revision_id" json:"revision_id"`
	Key             string `db:"question_key" json:"key"`
	Kind            string `db:"kind" json:"kind"`
	Prompt          string `db:"prompt" json:"prompt"`
	Rationale       string `db:"rationale" json:"rationale"`
	Required        bool   `db:"required" json:"required"`
	OptionsJSON     string `db:"options_json" json:"options_json"`
	RecommendedJSON string `db:"recommended_json" json:"recommended_json"`
	CreatedAt       string `db:"created_at" json:"created_at"`
}

type PlanResponse struct {
	ID         string `db:"id" json:"id"`
	PlanID     string `db:"plan_id" json:"plan_id"`
	RevisionID string `db:"revision_id" json:"revision_id"`
	QuestionID string `db:"question_id" json:"question_id"`
	ValuesJSON string `db:"values_json" json:"values_json"`
	Text       string `db:"text" json:"text"`
	CreatedAt  string `db:"created_at" json:"created_at"`
	UpdatedAt  string `db:"updated_at" json:"updated_at"`
}

type PlanComment struct {
	ID             string  `db:"id" json:"id"`
	PlanID         string  `db:"plan_id" json:"plan_id"`
	RevisionID     string  `db:"revision_id" json:"revision_id"`
	Body           string  `db:"body" json:"body"`
	SelectionStart *int    `db:"selection_start" json:"selection_start,omitempty"`
	SelectionEnd   *int    `db:"selection_end" json:"selection_end,omitempty"`
	SelectedText   string  `db:"selected_text" json:"selected_text"`
	Prefix         string  `db:"prefix" json:"prefix"`
	Suffix         string  `db:"suffix" json:"suffix"`
	SubmittedAt    *string `db:"submitted_at" json:"submitted_at,omitempty"`
	CreatedAt      string  `db:"created_at" json:"created_at"`
}

type PlanMessage struct {
	ID          int     `db:"id" json:"id"`
	PlanID      string  `db:"plan_id" json:"plan_id"`
	RevisionID  string  `db:"revision_id" json:"revision_id"`
	ThreadID    string  `db:"thread_id" json:"thread_id"`
	Kind        string  `db:"kind" json:"kind"`
	Author      string  `db:"author" json:"author"`
	Body        string  `db:"body" json:"body"`
	DeliveredAt *string `db:"delivered_at" json:"delivered_at,omitempty"`
	CreatedAt   string  `db:"created_at" json:"created_at"`
}

// Subtask represents a swarm sub-task: a unit of work parented to a task,
// owned by one worker agent, scoped to a set of file globs.
//
// Lifecycle: queued → dispatched → in_progress → reporting → done | cancelled.
// BuilderAgentID/ReviewerAgentID are kept (under their original column names)
// for migration compatibility — v1 only uses BuilderAgentID, repurposed as the
// single worker_agent_id slot.
type Subtask struct {
	ID              string  `db:"id"`
	ParentTaskID    string  `db:"parent_task_id"`
	Title           string  `db:"title"`
	Description     string  `db:"description"`
	Prompt          string  `db:"prompt"`
	ScopeGlobs      string  `db:"scope_globs"`
	Role            string  `db:"role"`
	AgentKind       string  `db:"agent_kind"`
	Tier            string  `db:"tier"`
	Status          string  `db:"status"`
	StepIndex       int     `db:"step_index"`
	BuilderAgentID  *int    `db:"builder_agent_id"`
	ReviewerAgentID *int    `db:"reviewer_agent_id"`
	CreatedAt       string  `db:"created_at"`
	DispatchedAt    *string `db:"dispatched_at"`
	StartedAt       *string `db:"started_at"`
	CompletedAt     *string `db:"completed_at"`
}
