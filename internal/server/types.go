package server

import (
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// HealthResponse is the JSON response for GET /health.
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

type MobileMetaResponse struct {
	Version        string `json:"version"`
	MobileShellAPI int    `json:"mobile_shell_api"`
	AuthBootstrap  string `json:"auth_bootstrap"`
	// PhrasePairing is 1 when a phone can pair by phrase (/api/pair/...).
	PhrasePairing int `json:"phrase_pairing,omitempty"`
	// Tint is this Legato's colour, so a switcher shows it before it has
	// a token for the server.
	Tint string `json:"tint,omitempty"`
	HandoffMeta
}

// ColumnResponse represents a column with its cards in the board response.
type ColumnResponse struct {
	Name  string         `json:"name"`
	Cards []CardResponse `json:"cards"`
}

// CardResponse represents a card summary in the board response.
type CardResponse struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Priority       string `json:"priority"`
	IssueType      string `json:"issue_type"`
	Status         string `json:"status"`
	Provider       string `json:"provider"`
	HasWarning     bool   `json:"has_warning"`
	HasWorktree    bool   `json:"has_worktree"`
	WorkspaceID    *int   `json:"workspace_id"`
	WorkspaceName  string `json:"workspace_name"`
	WorkspaceColor string `json:"workspace_color"`
	AgentActive    bool   `json:"agent_active"`
	AgentState     string `json:"agent_state"`
	AgentKind      string `json:"agent_kind,omitempty"`
	// SlotAgents are the task's running named slots (a reviewer), each
	// shown as a chip beside the main slot's state.
	SlotAgents     []service.SlotAgentSummary `json:"slot_agents,omitempty"`
	WorkingSeconds float64                    `json:"working_seconds"`
	WaitingSeconds float64                    `json:"waiting_seconds"`
	ErrorSeconds   float64                    `json:"error_seconds"`
	// PullRequests are the task's PRs in stack order, bottom first.
	PullRequests     []PRMetaResponse    `json:"pull_requests,omitempty"`
	SwarmStats       *SwarmStatsResponse `json:"swarm_stats,omitempty"`
	ReviewTourID     string              `json:"review_tour_id,omitempty"`
	ReviewName       string              `json:"review_name,omitempty"`
	ReviewReady      bool                `json:"review_ready"`
	ReviewUnreviewed int                 `json:"review_unreviewed"`
	// HandoffFrom is set on a worker for a card a main instance handed to
	// it: "from <main> · <main task id>".
	HandoffFrom *HandoffFromResponse `json:"handoff_from,omitempty"`
	// Away is set on a main for a task away on a worker.
	Away *AwayResponse `json:"away,omitempty"`
}

// HandoffFromResponse is the main instance a handed-off card came from.
type HandoffFromResponse struct {
	Main       string `json:"main"`
	MainTaskID string `json:"main_task_id"`
}

// AwayResponse is an away task as the web board shows it: the worker it is
// on, how it was handed off, the column it left, the worker's state of it as
// last heard (state, failure, state_at; absent when nothing was) and the
// address that opens it on the worker (url).
type AwayResponse struct {
	WorkerInstanceID string `json:"worker_instance_id"`
	Worker           string `json:"worker"`
	HandoffID        string `json:"handoff_id"`
	WorkerTaskID     string `json:"worker_task_id"`
	Purpose          string `json:"purpose"`
	Kind             string `json:"kind"`
	Repo             string `json:"repo"`
	Branch           string `json:"branch"`
	AgentKind        string `json:"agent_kind,omitempty"`
	LeftColumn       string `json:"left_column"`
	HandedOffAt      string `json:"handed_off_at"`
	State            string `json:"state,omitempty"`
	Failure          string `json:"failure,omitempty"`
	StateAt          string `json:"state_at,omitempty"`
	URL              string `json:"url,omitempty"`
	// What the worker's live connection says (service.AwayTask): the
	// agent's activity, the card's column and title there, the branches
	// pushed in stack order, why it is lost, since when the worker is
	// unreachable or refuses this main, and the view the lane shows.
	Activity               string   `json:"activity,omitempty"`
	WorkerColumn           string   `json:"worker_column,omitempty"`
	WorkerTitle            string   `json:"worker_title,omitempty"`
	Branches               []string `json:"branches,omitempty"`
	Lost                   string   `json:"lost,omitempty"`
	LostAt                 string   `json:"lost_at,omitempty"`
	WorkerUnreachableSince string   `json:"worker_unreachable_since,omitempty"`
	WorkerRefusedAt        string   `json:"worker_refused_at,omitempty"`
	View                   string   `json:"view,omitempty"`
	// Review is the handoff's review passes on the worker, as last heard.
	Review *service.HandoffReviewBlock `json:"review,omitempty"`
}

// BoardWorkerResponse is a worker this main is paired with, for the board's
// Away lane: one group per worker, shown once one is paired.
type BoardWorkerResponse struct {
	InstanceID string  `json:"instance_id"`
	Name       string  `json:"name"`
	Address    string  `json:"address"`
	LastPullAt *string `json:"last_pull_at,omitempty"`
	// UnreachableSince is when the worker went down, once past the grace;
	// RefusedAt when it stopped accepting this main (pair again).
	UnreachableSince *string `json:"unreachable_since,omitempty"`
	RefusedAt        *string `json:"refused_at,omitempty"`
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
	// Workers are the workers this main is paired with; empty when none.
	Workers []BoardWorkerResponse `json:"workers"`
}

// WorkspaceResponse represents a workspace in the JSON response.
type WorkspaceResponse struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// CardDetailResponse is the JSON response for GET /api/tasks/{id}.
type CardDetailResponse struct {
	ID                  string            `json:"id"`
	Title               string            `json:"title"`
	DescriptionMD       string            `json:"description_md"`
	LocalDescriptionMD  string            `json:"local_description_md"`
	RemoteDescriptionMD string            `json:"remote_description_md"`
	Status              string            `json:"status"`
	Priority            string            `json:"priority"`
	Provider            string            `json:"provider"`
	RemoteID            string            `json:"remote_id"`
	RemoteMeta          map[string]string `json:"remote_meta,omitempty"`
	Metadata            map[string]any    `json:"metadata"`
	WorkspaceID         *int              `json:"workspace_id,omitempty"`
	// PRMeta is the first PR in stack order, or how discovery last ended
	// when there is none.
	PRMeta *PRMetaResponse `json:"pr_meta,omitempty"`
	// PullRequests are every PR of the task, in stack order.
	PullRequests []PRMetaResponse `json:"pull_requests"`
	// TrackedBranches are the branches the task tracks, in stack order,
	// each with its PR once found.
	TrackedBranches     []TrackedBranchResponse `json:"tracked_branches"`
	CodeContext         *CodeContextResponse    `json:"code_context,omitempty"`
	Worktree            *TaskWorktreeResponse   `json:"worktree,omitempty"`
	HasChimeraSession   bool                    `json:"has_chimera_session"`
	ResumableAgentKinds []string                `json:"resumable_agent_kinds"`
	SwarmActiveStep     int                     `json:"swarm_active_step"`
	SwarmStepNames      []string                `json:"swarm_step_names,omitempty"`
	CreatedAt           string                  `json:"created_at"`
	UpdatedAt           string                  `json:"updated_at"`
	Notes               []store.TaskNote        `json:"notes"`
}

type TaskWorktreeResponse struct {
	PrimaryDir string `json:"primary_dir"`
	Path       string `json:"path"`
	Branch     string `json:"branch"`
	BaseBranch string `json:"base_branch"`
}

type CodeContextResponse struct {
	Repo         string `json:"repo,omitempty"`
	Branch       string `json:"branch,omitempty"`
	HeadSHA      string `json:"head_sha,omitempty"`
	WorkingDir   string `json:"working_dir,omitempty"`
	BaseBranch   string `json:"base_branch,omitempty"`
	RepoSource   string `json:"repo_source,omitempty"`
	BranchSource string `json:"branch_source,omitempty"`
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
	Pinned         bool   `json:"pinned"`
	MatchMethod    string `json:"match_method,omitempty"`
	SyncOutcome    string `json:"sync_outcome,omitempty"`
	SyncError      string `json:"sync_error,omitempty"`
}

// TrackedBranchResponse is one branch a task tracks. Source is
// code_context, stack, api, manual or discovered.
type TrackedBranchResponse struct {
	Repo   string          `json:"repo"`
	Branch string          `json:"branch"`
	Source string          `json:"source"`
	PR     *PRMetaResponse `json:"pr,omitempty"`
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
	// Provider is the source the ticket is from: "jira", "burnlite".
	Provider string `json:"provider"`
}

// ArchiveDoneResponse is the JSON response for POST /api/board/archive-done.
type ArchiveDoneResponse struct {
	Archived int `json:"archived"`
}
