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
	ID               string  `db:"id"`
	Title            string  `db:"title"`
	Description      string  `db:"description"`
	DescriptionMD    string  `db:"description_md"`
	Status           string  `db:"status"`
	Priority         string  `db:"priority"`
	SortOrder        int     `db:"sort_order"`
	Provider         *string `db:"provider"`
	RemoteID         *string `db:"remote_id"`
	RemoteMeta       *string `db:"remote_meta"`
	PRMeta           *string `db:"pr_meta"`
	WorkspaceID      *int    `db:"workspace_id"`
	ArchivedAt       *string `db:"archived_at"`
	Ephemeral        bool    `db:"ephemeral"`
	SwarmWorkingDir  *string `db:"swarm_working_dir"`
	SwarmActiveStep  int     `db:"swarm_active_step"`
	ChimeraSessionID *string `db:"chimera_session_id"`
	CreatedAt        string  `db:"created_at"`
	UpdatedAt        string  `db:"updated_at"`
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
