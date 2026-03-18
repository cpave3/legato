package service

import (
	"context"
	"time"
)

// TicketProvider abstracts the remote ticket source (Jira, Linear, GitHub Issues, etc.).
// The sync service depends on this interface, not a concrete implementation.
type TicketProvider interface {
	// Search returns tickets matching the given query string.
	// The query format is provider-specific (e.g., JQL for Jira).
	Search(ctx context.Context, query string) ([]RemoteTicket, error)

	// GetTicket returns full detail for a single ticket by ID/key.
	GetTicket(ctx context.Context, id string) (*RemoteTicket, error)

	// ListTransitions returns available state transitions for a ticket.
	ListTransitions(ctx context.Context, id string) ([]RemoteTransition, error)

	// DoTransition executes a state transition on a ticket.
	DoTransition(ctx context.Context, id string, transitionID string) error
}

// RemoteTicket represents a ticket from the remote provider, normalized to a common shape.
type RemoteTicket struct {
	ID            string
	Summary       string
	DescriptionMD string // Already converted to markdown by the provider
	Status        string
	Priority      string
	IssueType     string
	Assignee      string
	Labels        []string
	EpicKey       string
	EpicName      string
	URL           string
	UpdatedAt     time.Time
}

// RemoteTransition represents an available state transition from the remote provider.
type RemoteTransition struct {
	ID           string
	Name         string
	TargetStatus string
}
