package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

const conflictWindow = 5 * time.Minute

type syncService struct {
	store    *store.Store
	bus      *events.Bus
	provider TicketProvider
	jql      string
	interval time.Duration

	mu   sync.Mutex
	last time.Time
	subs []chan SyncEvent
}

// NewSyncService creates a SyncService backed by a TicketProvider.
func NewSyncService(s *store.Store, bus *events.Bus, provider TicketProvider, jql string, interval time.Duration) SyncService {
	return &syncService{
		store:    s,
		bus:      bus,
		provider: provider,
		jql:      jql,
		interval: interval,
	}
}

func (s *syncService) Sync(ctx context.Context) (*SyncResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.broadcast(SyncEvent{Type: EventSyncStarted, Message: "sync started"})
	s.bus.Publish(events.Event{Type: events.EventSyncStarted, At: time.Now()})

	result, err := s.pullSync(ctx)
	if err != nil {
		s.broadcast(SyncEvent{Type: EventSyncFailed, Message: err.Error()})
		s.bus.Publish(events.Event{Type: events.EventSyncFailed, Payload: err.Error(), At: time.Now()})
		// Publish specific error event
		errType := classifyError(err)
		s.bus.Publish(events.Event{
			Type: errType,
			Payload: events.ErrorPayload{
				ErrorType: errorTypeName(errType),
				Message:   err.Error(),
			},
			At: time.Now(),
		})
		return nil, err
	}

	// Retry any failed push transitions
	s.retryFailedPushes(ctx)

	s.last = time.Now()

	s.broadcast(SyncEvent{Type: EventCardsRefreshed, Message: "cards refreshed"})
	s.bus.Publish(events.Event{Type: events.EventCardsRefreshed, At: time.Now()})

	s.broadcast(SyncEvent{Type: EventSyncCompleted, Message: "sync completed"})
	s.bus.Publish(events.Event{Type: events.EventSyncCompleted, At: time.Now()})

	return result, nil
}

func (s *syncService) pullSync(ctx context.Context) (*SyncResult, error) {
	// Fetch remote tickets
	remoteTickets, err := s.provider.Search(ctx, s.jql)
	if err != nil {
		return nil, err
	}

	// Load column mappings for status-to-column resolution
	mappings, err := s.store.ListColumnMappings(ctx)
	if err != nil {
		return nil, err
	}

	// Track which tickets we've seen from the remote
	seenIDs := make(map[string]bool, len(remoteTickets))
	synced := 0

	for _, rt := range remoteTickets {
		seenIDs[rt.ID] = true

		// Resolve the local column from the remote status
		column := resolveColumn(rt.Status, mappings)
		if column == "" {
			column = "Backlog" // fallback
		}

		existing, err := s.store.GetTicket(ctx, rt.ID)
		if errors.Is(err, store.ErrNotFound) {
			// New ticket — insert
			now := time.Now().UTC().Format(time.RFC3339)
			labels := strings.Join(rt.Labels, ",")
			ticket := store.Ticket{
				ID:            rt.ID,
				Summary:       rt.Summary,
				Description:   "",
				DescriptionMD: rt.DescriptionMD,
				Status:        column,
				RemoteStatus:    rt.Status,
				Priority:      rt.Priority,
				IssueType:     rt.IssueType,
				Assignee:      rt.Assignee,
				Labels:        labels,
				EpicKey:       rt.EpicKey,
				EpicName:      rt.EpicName,
				URL:           rt.URL,
				CreatedAt:     now,
				UpdatedAt:     now,
				RemoteUpdatedAt: rt.UpdatedAt.UTC().Format(time.RFC3339),
				SortOrder:     0,
			}
			if err := s.store.CreateTicket(ctx, ticket); err != nil {
				return nil, err
			}
			synced++
			continue
		} else if err != nil {
			return nil, err
		}

		// Existing ticket — check if remote updated
		existingUpdated, _ := time.Parse(time.RFC3339, existing.RemoteUpdatedAt)
		if !rt.UpdatedAt.After(existingUpdated) {
			// Clear stale marker since ticket is still in results
			if existing.StaleAt != nil {
				existing.StaleAt = nil
				s.store.UpdateTicket(ctx, *existing)
			}
			continue
		}

		// Update fields from remote
		existing.Summary = rt.Summary
		existing.DescriptionMD = rt.DescriptionMD
		existing.Priority = rt.Priority
		existing.IssueType = rt.IssueType
		existing.Assignee = rt.Assignee
		existing.Labels = strings.Join(rt.Labels, ",")
		existing.EpicKey = rt.EpicKey
		existing.EpicName = rt.EpicName
		existing.URL = rt.URL
		existing.RemoteUpdatedAt = rt.UpdatedAt.UTC().Format(time.RFC3339)
		existing.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		existing.RemoteStatus = rt.Status
		existing.StaleAt = nil // clear stale marker

		// Check if status/column changed
		newColumn := resolveColumn(rt.Status, mappings)
		if newColumn != "" && newColumn != existing.Status {
			// Check conflict window: if local move is recent, preserve local
			if s.isWithinConflictWindow(existing) {
				// Local wins — log the conflict
				s.store.InsertSyncLog(ctx, store.SyncLogEntry{
					TicketID: rt.ID,
					Action:   "conflict_local_wins",
					Detail:   "local=" + existing.Status + " remote=" + newColumn,
				})
			} else {
				existing.Status = newColumn
			}
		}

		if err := s.store.UpdateTicket(ctx, *existing); err != nil {
			return nil, err
		}
		synced++
	}

	// Mark stale tickets (present locally but absent from remote results)
	allIDs, err := s.store.ListTicketIDs(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range allIDs {
		if seenIDs[id] {
			continue
		}
		ticket, err := s.store.GetTicket(ctx, id)
		if err != nil {
			continue
		}
		if ticket.StaleAt == nil {
			ticket.StaleAt = &now
			s.store.UpdateTicket(ctx, *ticket)
		}
	}

	return &SyncResult{TicketsSynced: synced}, nil
}

func (s *syncService) isWithinConflictWindow(ticket *store.Ticket) bool {
	if ticket.LocalMoveAt == nil || *ticket.LocalMoveAt == "" {
		return false
	}
	moveAt, err := time.Parse(time.RFC3339, *ticket.LocalMoveAt)
	if err != nil {
		return false
	}
	return time.Since(moveAt) < conflictWindow
}

// PushMove updates the local ticket immediately and queues an async remote transition.
func (s *syncService) PushMove(ctx context.Context, ticketID, targetColumn string) error {
	ticket, err := s.store.GetTicket(ctx, ticketID)
	if err != nil {
		return err
	}

	// Update local state immediately
	now := time.Now().UTC().Format(time.RFC3339)
	ticket.Status = targetColumn
	ticket.UpdatedAt = now
	ticket.LocalMoveAt = &now
	if err := s.store.UpdateTicket(ctx, *ticket); err != nil {
		return err
	}

	// Find the transition ID for the target column
	mappings, err := s.store.ListColumnMappings(ctx)
	if err != nil {
		return err
	}
	var transitionID string
	for _, m := range mappings {
		if m.ColumnName == targetColumn && m.RemoteTransition != "" {
			transitionID = m.RemoteTransition
			break
		}
	}

	if transitionID == "" {
		// No transition configured — local-only move
		return nil
	}

	// Queue async remote transition
	go s.executePush(ticketID, transitionID, targetColumn)
	return nil
}

func (s *syncService) executePush(ticketID, transitionID, targetColumn string) {
	ctx := context.Background()

	err := s.provider.DoTransition(ctx, ticketID, transitionID)
	if err != nil {
		// Push failed — log and set warning
		s.store.InsertSyncLog(ctx, store.SyncLogEntry{
			TicketID: ticketID,
			Action:   "push_failed",
			Detail:   err.Error(),
		})

		s.broadcast(SyncEvent{Type: EventSyncFailed, Message: "push failed: " + err.Error()})
		s.bus.Publish(events.Event{Type: events.EventSyncFailed, Payload: err.Error(), At: time.Now()})
		// Publish specific transition failure event
		s.bus.Publish(events.Event{
			Type: events.EventTransitionFailed,
			Payload: events.ErrorPayload{
				ErrorType: "transition_failed",
				Message:   err.Error(),
				TicketKey: ticketID,
			},
			At: time.Now(),
		})
		return
	}

	// Push succeeded — update remote status
	ticket, getErr := s.store.GetTicket(ctx, ticketID)
	if getErr != nil {
		return
	}

	// Look up what statuses map to the target column and use the first one
	mappings, _ := s.store.ListColumnMappings(ctx)
	for _, m := range mappings {
		if m.ColumnName == targetColumn {
			var statuses []string
			json.Unmarshal([]byte(m.RemoteStatuses), &statuses)
			if len(statuses) > 0 {
				ticket.RemoteStatus = statuses[0]
			}
			break
		}
	}

	ticket.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	s.store.UpdateTicket(ctx, *ticket)

	s.store.InsertSyncLog(ctx, store.SyncLogEntry{
		TicketID: ticketID,
		Action:   "push_success",
		Detail:   "transitioned to " + targetColumn,
	})
}

// retryFailedPushes retries any pending failed push transitions.
func (s *syncService) retryFailedPushes(ctx context.Context) {
	allIDs, err := s.store.ListTicketIDs(ctx)
	if err != nil {
		return
	}

	mappings, _ := s.store.ListColumnMappings(ctx)

	for _, id := range allIDs {
		ticket, err := s.store.GetTicket(ctx, id)
		if err != nil {
			continue
		}

		// Check if the ticket's local column has a different remote status than expected
		expectedColumn := resolveColumn(ticket.RemoteStatus, mappings)
		if expectedColumn == ticket.Status {
			continue // In sync
		}

		// Find the transition ID for the ticket's current local column
		for _, m := range mappings {
			if m.ColumnName == ticket.Status && m.RemoteTransition != "" {
				s.executePush(id, m.RemoteTransition, ticket.Status)
				break
			}
		}
	}
}

// StartScheduler begins periodic sync at the configured interval.
// Returns a stop function to cancel the scheduler.
func (s *syncService) StartScheduler(ctx context.Context) func() {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.Sync(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
	return cancel
}

func (s *syncService) Status() SyncStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SyncStatus{
		InProgress: false,
		LastSync:   s.last,
	}
}

func (s *syncService) Subscribe() <-chan SyncEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan SyncEvent, 64)
	s.subs = append(s.subs, ch)
	return ch
}

func (s *syncService) broadcast(e SyncEvent) {
	for _, ch := range s.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// classifyError maps an error to the appropriate error event type.
func classifyError(err error) events.EventType {
	msg := err.Error()
	if strings.Contains(msg, "authentication failed") || strings.Contains(msg, "401") || strings.Contains(msg, "403") {
		return events.EventAuthFailed
	}
	if strings.Contains(msg, "rate limited") || strings.Contains(msg, "429") {
		return events.EventRateLimited
	}
	return events.EventSyncError
}

// errorTypeName returns a human-readable name for an error event type.
func errorTypeName(t events.EventType) string {
	switch t {
	case events.EventAuthFailed:
		return "auth_failed"
	case events.EventRateLimited:
		return "rate_limited"
	case events.EventTransitionFailed:
		return "transition_failed"
	default:
		return "offline"
	}
}

func resolveColumn(remoteStatus string, mappings []store.ColumnMapping) string {
	for _, m := range mappings {
		var statuses []string
		json.Unmarshal([]byte(m.RemoteStatuses), &statuses)
		for _, s := range statuses {
			if strings.EqualFold(s, remoteStatus) {
				return m.ColumnName
			}
		}
	}
	return ""
}
