package store

import "encoding/json"

// PRMeta holds PR tracking metadata stored as JSON in the pr_meta column.
type PRMeta struct {
	Repo           string `json:"repo,omitempty"`             // owner/repo format
	Branch         string `json:"branch"`
	PRNumber       int    `json:"pr_number,omitempty"`
	PRURL          string `json:"pr_url,omitempty"`
	State          string `json:"state,omitempty"`           // OPEN, MERGED, CLOSED, or ""
	IsDraft        bool   `json:"is_draft,omitempty"`
	ReviewDecision string `json:"review_decision,omitempty"` // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, or ""
	CheckStatus    string `json:"check_status,omitempty"`    // pass, fail, pending, or ""
	CommentCount   int    `json:"comment_count,omitempty"`
	UpdatedAt      string `json:"updated_at,omitempty"`      // RFC3339
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
	ID            string  `db:"id"`
	Title         string  `db:"title"`
	Description   string  `db:"description"`
	DescriptionMD string  `db:"description_md"`
	Status        string  `db:"status"`
	Priority      string  `db:"priority"`
	SortOrder     int     `db:"sort_order"`
	Provider      *string `db:"provider"`
	RemoteID      *string `db:"remote_id"`
	RemoteMeta    *string `db:"remote_meta"`
	PRMeta        *string `db:"pr_meta"`
	WorkspaceID   *int    `db:"workspace_id"`
	ArchivedAt    *string `db:"archived_at"`
	Ephemeral     bool    `db:"ephemeral"`
	CreatedAt     string  `db:"created_at"`
	UpdatedAt     string  `db:"updated_at"`
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
	ID        int     `db:"id"`
	TaskID    string  `db:"task_id"`
	State     string  `db:"state"`
	StartedAt string  `db:"started_at"`
	EndedAt   *string `db:"ended_at"`
}

type AgentSession struct {
	ID          int     `db:"id"`
	TaskID      string  `db:"task_id"`
	TmuxSession string  `db:"tmux_session"`
	Command     string  `db:"command"`
	Status      string  `db:"status"`
	Activity    string  `db:"activity"`
	StartedAt   string  `db:"started_at"`
	EndedAt     *string `db:"ended_at"`
}
