package service

import (
	"context"
	"time"
)

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

// Card represents a ticket summary for list views.
type Card struct {
	ID         string
	Summary    string
	Priority   string
	IssueType  string
	Status     string
	SortOrder  int
	HasWarning bool
}

// CardDetail contains all metadata for a single ticket.
type CardDetail struct {
	ID            string
	Summary       string
	DescriptionMD string
	Status        string
	Priority      string
	IssueType     string
	Assignee      string
	Labels        string
	EpicKey       string
	EpicName      string
	URL           string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// SyncResult contains the outcome of a sync operation.
type SyncResult struct {
	TicketsSynced int
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
	GetCard(ctx context.Context, id string) (*CardDetail, error)
	MoveCard(ctx context.Context, id string, targetColumn string) error
	ReorderCard(ctx context.Context, id string, newPosition int) error
	SearchCards(ctx context.Context, query string) ([]Card, error)
	ExportCardContext(ctx context.Context, id string, format ExportFormat) (string, error)
}

// SyncService manages data synchronization.
type SyncService interface {
	Sync(ctx context.Context) (*SyncResult, error)
	Status() SyncStatus
	Subscribe() <-chan SyncEvent
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
