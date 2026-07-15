package server

// HealthResponse is the JSON response for GET /health.
type HealthResponse struct {
	Status   string           `json:"status"`
	Columns  []ColumnResponse `json:"columns"`
	SyncedAt *string          `json:"synced_at"`
}

// ColumnResponse represents a column with its cards in the health response.
type ColumnResponse struct {
	Name  string         `json:"name"`
	Cards []CardResponse `json:"cards"`
}

// CardResponse represents a card summary in the board response.
type CardResponse struct {
	ID               string              `json:"id"`
	Title            string              `json:"title"`
	Priority         string              `json:"priority"`
	IssueType        string              `json:"issue_type"`
	Status           string              `json:"status"`
	Provider         string              `json:"provider"`
	HasWarning       bool                `json:"has_warning"`
	WorkspaceName    string              `json:"workspace_name"`
	WorkspaceColor   string              `json:"workspace_color"`
	AgentActive      bool                `json:"agent_active"`
	AgentState       string              `json:"agent_state"`
	WorkingSeconds   float64             `json:"working_seconds"`
	WaitingSeconds   float64             `json:"waiting_seconds"`
	PRNumber         int                 `json:"pr_number"`
	PRCheckStatus    string              `json:"pr_check_status"`
	PRReviewDecision string              `json:"pr_review_decision"`
	PRCommentCount   int                 `json:"pr_comment_count"`
	PRIsDraft        bool                `json:"pr_is_draft"`
	SwarmStats       *SwarmStatsResponse `json:"swarm_stats,omitempty"`
	ReviewReady      bool                `json:"review_ready"`
	ReviewUnreviewed int                 `json:"review_unreviewed"`
}

// SwarmStatsResponse is the JSON representation of aggregate sub-task counts.
type SwarmStatsResponse struct {
	Total    int `json:"total"`
	Done     int `json:"done"`
	InReview int `json:"in_review"`
	Building int `json:"building"`
	Queued   int `json:"queued"`
	Rejected int `json:"rejected"`
}

// TasksResponse groups tasks by column (legacy endpoint).
type TasksResponse map[string][]CardResponse

// BoardResponse is the JSON response for GET /api/board.
type BoardResponse struct {
	Columns    []ColumnResponse    `json:"columns"`
	Workspaces []WorkspaceResponse `json:"workspaces"`
}

// WorkspaceResponse represents a workspace in the JSON response.
type WorkspaceResponse struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// CardDetailResponse is the JSON response for GET /api/tasks/{id}.
type CardDetailResponse struct {
	ID              string            `json:"id"`
	Title           string            `json:"title"`
	DescriptionMD   string            `json:"description_md"`
	Status          string            `json:"status"`
	Priority        string            `json:"priority"`
	Provider        string            `json:"provider"`
	RemoteID        string            `json:"remote_id"`
	RemoteMeta      map[string]string `json:"remote_meta,omitempty"`
	WorkspaceID     *int              `json:"workspace_id,omitempty"`
	PRMeta          *PRMetaResponse   `json:"pr_meta,omitempty"`
	SwarmActiveStep int               `json:"swarm_active_step"`
	SwarmStepNames  []string          `json:"swarm_step_names,omitempty"`
	CreatedAt       string            `json:"created_at"`
	UpdatedAt       string            `json:"updated_at"`
}

// PRMetaResponse is the JSON representation of PR metadata linked to a task.
type PRMetaResponse struct {
	Repo           string `json:"repo,omitempty"`
	Branch         string `json:"branch,omitempty"`
	PRNumber       int    `json:"pr_number"`
	PRURL          string `json:"pr_url,omitempty"`
	State          string `json:"state,omitempty"`
	IsDraft        bool   `json:"is_draft"`
	ReviewDecision string `json:"review_decision,omitempty"`
	CheckStatus    string `json:"check_status,omitempty"`
	CommentCount   int    `json:"comment_count"`
}

// PRPreviewResponse is the JSON response for GET /api/tasks/{id}/pr-preview.
type PRPreviewResponse struct {
	Number         int    `json:"number"`
	URL            string `json:"url"`
	State          string `json:"state"`
	IsDraft        bool   `json:"is_draft"`
	CheckStatus    string `json:"check_status"`
	ReviewDecision string `json:"review_decision"`
	CommentCount   int    `json:"comment_count"`
	HeadBranch     string `json:"head_branch,omitempty"`
	Title          string `json:"title,omitempty"`
}

// RemoteSearchResultResponse is the JSON representation of a remote search result.
type RemoteSearchResultResponse struct {
	ID        string `json:"id"`
	Summary   string `json:"summary"`
	Status    string `json:"status"`
	Priority  string `json:"priority"`
	IssueType string `json:"issue_type"`
}

// ArchiveDoneResponse is the JSON response for POST /api/board/archive-done.
type ArchiveDoneResponse struct {
	Archived int `json:"archived"`
}
