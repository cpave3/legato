package service

import (
	"context"
	"time"

	"github.com/cpave3/legato/internal/engine/analytics"
	"github.com/cpave3/legato/internal/engine/store"
)

// CodeContextView holds durable repository identity for display and editing.
type CodeContextView struct {
	Repo         string
	Branch       string
	HeadSHA      string
	WorkingDir   string
	BaseBranch   string
	RepoSource   string
	BranchSource string
}

// PRMetaView holds parsed remote PR metadata for display.
type PRMetaView struct {
	Repo           string // populated from code context for compatibility
	Branch         string // populated from code context for compatibility
	PRNumber       int
	PRURL          string
	State          string
	IsDraft        bool
	ReviewDecision string
	CheckStatus    string
	CommentCount   int
	Pinned         bool
	MatchMethod    string
	SyncOutcome    string
	SyncError      string
}

// TrackedBranchView is one branch a task tracks, in stack order, with its
// PR once found.
type TrackedBranchView struct {
	Repo   string
	Branch string
	Source string
	PR     *PRMetaView
}

// ExportFormat defines the output format for card context export.
type ExportFormat int

const (
	ExportFormatDescription ExportFormat = iota
	ExportFormatFull
)

// Column represents a kanban board column.
type Column struct {
	Name      string
	SortOrder int
}

// Card represents a task summary for list views.
type Card struct {
	ID             string
	Title          string
	Priority       string
	IssueType      string
	Status         string
	Provider       string // "jira", "github", or "" for local
	SortOrder      int
	HasWarning     bool
	HasWorktree    bool
	WorkspaceID    *int
	WorkspaceName  string
	WorkspaceColor string
	// HandedOffFrom is set on a worker for a card a main instance handed
	// to it, while the handoff is open.
	HandedOffFrom *HandoffOrigin
	// Away is set on a main for a task away on a worker.
	Away *AwayOn
}

// HandoffOrigin is where a handed-off card came from: the main instance's
// name and its own ID for the task.
type HandoffOrigin struct {
	MainName   string
	MainTaskID string
}

// AwayOn is the worker an away task is on.
type AwayOn struct {
	WorkerInstanceID string
	WorkerName       string
	HandoffID        string
}

// Workspace represents a workspace for grouping tasks.
type Workspace struct {
	ID    int
	Name  string
	Color string
}

// CardDetail contains all metadata for a single task.
type CardDetail struct {
	ID                  string
	Title               string
	DescriptionMD       string
	LocalDescriptionMD  string
	RemoteDescriptionMD string
	Status              string
	Priority            string
	Provider            string
	RemoteID            string
	RemoteMeta          map[string]string
	Metadata            map[string]any
	WorkspaceID         *int
	// PRMeta is the task's first PR in stack order (or how discovery last
	// ended), kept for readers of the single form.
	PRMeta *PRMetaView
	// PullRequests are every PR of the task, in stack order.
	PullRequests []PRMetaView
	// Branches are the branches the task tracks, in stack order.
	Branches            []TrackedBranchView
	CodeContext         *CodeContextView
	Worktree            *store.TaskWorktree
	HasChimeraSession   bool
	ResumableAgentKinds []string
	SwarmActiveStep     int
	SwarmStepNames      []string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	Attachments         []LocalAttachment
	Notes               []store.TaskNote
}

// LocalAttachment describes a cached attachment available to agents.
type LocalAttachment struct {
	Filename string
	MimeType string
	Path     string
}

// SyncResult contains the outcome of a sync operation.
type SyncResult struct {
	TasksSynced int
	// Sources is each ticket source's outcome, by provider name.
	Sources map[string]SourceSyncResult
}

// SourceSyncResult is one ticket source's part of a sync.
type SourceSyncResult struct {
	TasksSynced int
	Err         error
	// Disabled: the source is not configured or not connected.
	Disabled bool
}

// SyncStatus reports the current state of sync.
type SyncStatus struct {
	InProgress bool
	LastSync   time.Time
}

// SyncEvent represents a lifecycle event during sync.
type SyncEvent struct {
	Type    string
	Message string
}

// BoardService provides kanban board operations.
type TaskActivity struct {
	Reviews      []store.ReviewTour     `json:"reviews"`
	Plans        []store.Plan           `json:"plans"`
	WorkflowRuns []TaskWorkflowActivity `json:"workflow_runs"`
	// Delegations are briefs integrations (Argus) handed the task's agent,
	// newest first, with the agent's replies.
	Delegations []TaskDelegationActivity `json:"delegations"`
}

// TaskDelegationActivity is a delegation as a task's activity shows it.
type TaskDelegationActivity struct {
	ID           string              `json:"id"`
	Integration  string              `json:"integration"`
	CaseID       string              `json:"case_id"`
	Mode         string              `json:"mode"`
	Route        string              `json:"route"`
	State        string              `json:"state"`
	Enforced     bool                `json:"enforced"`
	ErrorCode    string              `json:"error_code,omitempty"`
	ErrorMessage string              `json:"error_message,omitempty"`
	Brief        string              `json:"brief"`
	CreatedAt    string              `json:"created_at"`
	DeliveredAt  string              `json:"delivered_at,omitempty"`
	Replies      []TaskCaseReplyItem `json:"replies"`
}

// TaskCaseReplyItem is an agent's reply to a delegation.
type TaskCaseReplyItem struct {
	ID        string `json:"id"`
	Outcome   string `json:"outcome"`
	Text      string `json:"text"`
	State     string `json:"state"`
	LastError string `json:"last_error,omitempty"`
	CreatedAt string `json:"created_at"`
}
type TaskWorkflowActivity struct {
	Run       store.WorkflowRun        `json:"run"`
	Artifacts []store.WorkflowArtifact `json:"artifacts"`
}
type TaskActivityService interface {
	TaskActivity(context.Context, string) (*TaskActivity, error)
}

type TaskMetadataService interface {
	SetTaskMetadata(ctx context.Context, taskID, path string, value any) error
}

type BoardService interface {
	ListColumns(ctx context.Context) ([]Column, error)
	ListCards(ctx context.Context, column string) ([]Card, error)
	ListCardsByWorkspace(ctx context.Context, column string, view store.WorkspaceView) ([]Card, error)
	GetCard(ctx context.Context, id string) (*CardDetail, error)
	MoveCard(ctx context.Context, id string, targetColumn string) error
	ReorderCard(ctx context.Context, id string, newPosition int) error
	SearchCards(ctx context.Context, query string) ([]Card, error)
	ExportCardContext(ctx context.Context, id string, format ExportFormat) (string, error)
	CreateTask(ctx context.Context, title, description, column, priority string, workspaceID *int) (*Card, error)
	DeleteTask(ctx context.Context, id string) error
	UpdateTaskDescription(ctx context.Context, id, description string) error
	UpdateTaskTitle(ctx context.Context, id, title string) error
	UpdateTaskWorkspace(ctx context.Context, id string, workspaceID *int) error
	ListWorkspaces(ctx context.Context) ([]Workspace, error)
	ArchiveDoneCards(ctx context.Context) (int, error)
	ArchiveTask(ctx context.Context, id string) error
	ListArchivedTasks(ctx context.Context) ([]Card, error)
	RestoreTask(ctx context.Context, id string) error
	CountDoneCards(ctx context.Context) (int, error)
}

// SyncService manages data synchronization.
// RemoteSearchResult is a lightweight result from searching remote providers.
type RemoteSearchResult struct {
	ID        string
	Summary   string
	Status    string
	Priority  string
	IssueType string
	// Provider is the source the ticket is from ("jira").
	Provider string
}

type SyncService interface {
	Sync(ctx context.Context) (*SyncResult, error)
	Status() SyncStatus
	Subscribe() <-chan SyncEvent
	StartScheduler(ctx context.Context) func()
	SearchRemote(ctx context.Context, query string) ([]RemoteSearchResult, error)
	// ImportRemoteTask tracks a ticket as a task. provider names its source;
	// "" is the only one connected.
	ImportRemoteTask(ctx context.Context, provider, ticketID string, workspaceID *int) (*Card, error)
}

// CardMoveSync coordinates a local card move with its remote provider.
// Sync services may implement this in addition to SyncService.
type CardMoveSync interface {
	PushMove(ctx context.Context, taskID, targetColumn string) error
}

// ReportService generates analytics reports.
type ReportService interface {
	GenerateReport(ctx context.Context, period analytics.TimeRange) (*Report, error)
}

// EventBus abstracts event publishing and subscription.
type EventBus interface {
	Publish(eventType string, payload interface{})
	Subscribe(eventType string) <-chan interface{}
	Unsubscribe(ch <-chan interface{})
}

// Event type constants for the service layer.
const (
	EventCardMoved      = "card.moved"
	EventCardUpdated    = "card.updated"
	EventCardsRefreshed = "cards.refreshed"
	EventSyncStarted    = "sync.started"
	EventSyncCompleted  = "sync.completed"
	EventSyncFailed     = "sync.failed"
)
