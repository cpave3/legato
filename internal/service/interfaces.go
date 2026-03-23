package service

import (
	"context"
	"time"

	"github.com/cpave3/legato/internal/engine/analytics"
	"github.com/cpave3/legato/internal/engine/store"
)

// PRMetaView holds parsed PR metadata for display.
type PRMetaView struct {
	Branch         string
	PRNumber       int
	PRURL          string
	State          string
	IsDraft        bool
	ReviewDecision string
	CheckStatus    string
	CommentCount   int
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
	WorkspaceName  string
	WorkspaceColor string
}

// Workspace represents a workspace for grouping tasks.
type Workspace struct {
	ID    int
	Name  string
	Color string
}

// CardDetail contains all metadata for a single task.
type CardDetail struct {
	ID            string
	Title         string
	DescriptionMD string
	Status        string
	Priority      string
	Provider      string
	RemoteID      string
	RemoteMeta    map[string]string
	WorkspaceID   *int
	PRMeta        *PRMetaView
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// SyncResult contains the outcome of a sync operation.
type SyncResult struct {
	TasksSynced int
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
}

type SyncService interface {
	Sync(ctx context.Context) (*SyncResult, error)
	Status() SyncStatus
	Subscribe() <-chan SyncEvent
	StartScheduler(ctx context.Context) func()
	SearchRemote(ctx context.Context, query string) ([]RemoteSearchResult, error)
	ImportRemoteTask(ctx context.Context, ticketID string, workspaceID *int) (*Card, error)
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
