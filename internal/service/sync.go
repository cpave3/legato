package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

const conflictWindow = 5 * time.Minute

type syncService struct {
	store       *store.Store
	bus         *events.Bus
	provider    TicketProvider
	jql         string
	projectKeys []string
	interval    time.Duration

	mu   sync.Mutex
	last time.Time
	subs []chan SyncEvent
}

// remoteMeta is the JSON shape stored in task.remote_meta for synced tasks.
type remoteMeta struct {
	RemoteStatus     string `json:"remote_status"`
	RemoteUpdatedAt  string `json:"remote_updated_at"`
	IssueType        string `json:"issue_type"`
	Assignee         string `json:"assignee"`
	Labels           string `json:"labels"`
	EpicKey          string `json:"epic_key"`
	EpicName         string `json:"epic_name"`
	URL              string `json:"url"`
	StaleAt          string `json:"stale_at,omitempty"`
	LocalMoveAt      string `json:"local_move_at,omitempty"`
	RemoteTransition string `json:"remote_transition,omitempty"`
}

func parseRemoteMeta(raw *string) remoteMeta {
	var m remoteMeta
	if raw != nil {
		json.Unmarshal([]byte(*raw), &m)
	}
	return m
}

func encodeRemoteMeta(m remoteMeta) *string {
	b, _ := json.Marshal(m)
	s := string(b)
	return &s
}

// NewSyncService creates a SyncService backed by a TicketProvider.
func NewSyncService(s *store.Store, bus *events.Bus, provider TicketProvider, jql string, projectKeys []string, interval time.Duration) SyncService {
	return &syncService{
		store:       s,
		bus:         bus,
		provider:    provider,
		jql:         jql,
		projectKeys: projectKeys,
		interval:    interval,
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

	// Track which task IDs we've seen from the remote
	seenIDs := make(map[string]bool, len(remoteTickets))
	synced := 0

	provider := "jira"

	for _, rt := range remoteTickets {
		seenIDs[rt.ID] = true

		// Resolve the local column from the remote status
		column := resolveColumn(rt.Status, mappings)
		if column == "" {
			column = "Backlog" // fallback
		}

		existing, err := s.store.GetTask(ctx, rt.ID)
		if errors.Is(err, store.ErrNotFound) {
			// New task — insert
			now := time.Now().UTC().Format(time.RFC3339)
			labels := strings.Join(rt.Labels, ",")
			meta := remoteMeta{
				RemoteStatus:    rt.Status,
				RemoteUpdatedAt: rt.UpdatedAt.UTC().Format(time.RFC3339),
				IssueType:       rt.IssueType,
				Assignee:        rt.Assignee,
				Labels:          labels,
				EpicKey:         rt.EpicKey,
				EpicName:        rt.EpicName,
				URL:             rt.URL,
			}
			remoteID := rt.ID
			task := store.Task{
				ID:            rt.ID,
				Title:         rt.Summary,
				Description:   "",
				DescriptionMD: rt.DescriptionMD,
				Status:        column,
				Priority:      rt.Priority,
				SortOrder:     0,
				Provider:      &provider,
				RemoteID:      &remoteID,
				RemoteMeta:    encodeRemoteMeta(meta),
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			if err := s.store.CreateTask(ctx, task); err != nil {
				return nil, err
			}
			synced++
			continue
		} else if err != nil {
			return nil, err
		}

		// Existing task — check if remote updated
		existingMeta := parseRemoteMeta(existing.RemoteMeta)
		existingUpdated, _ := time.Parse(time.RFC3339, existingMeta.RemoteUpdatedAt)
		if !rt.UpdatedAt.After(existingUpdated) {
			// Clear stale marker since task is still in results
			if existingMeta.StaleAt != "" {
				existingMeta.StaleAt = ""
				existing.RemoteMeta = encodeRemoteMeta(existingMeta)
				s.store.UpdateTask(ctx, *existing)
			}
			continue
		}

		// Update fields from remote
		existing.Title = rt.Summary
		existing.DescriptionMD = rt.DescriptionMD
		existing.Priority = rt.Priority
		existing.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

		existingMeta.RemoteStatus = rt.Status
		existingMeta.RemoteUpdatedAt = rt.UpdatedAt.UTC().Format(time.RFC3339)
		existingMeta.IssueType = rt.IssueType
		existingMeta.Assignee = rt.Assignee
		existingMeta.Labels = strings.Join(rt.Labels, ",")
		existingMeta.EpicKey = rt.EpicKey
		existingMeta.EpicName = rt.EpicName
		existingMeta.URL = rt.URL
		existingMeta.StaleAt = "" // clear stale marker

		// Check if status/column changed
		newColumn := resolveColumn(rt.Status, mappings)
		if newColumn != "" && newColumn != existing.Status {
			// Check conflict window: if local move is recent, preserve local
			if s.isWithinConflictWindow(existingMeta) {
				// Local wins — log the conflict
				s.store.InsertSyncLog(ctx, store.SyncLogEntry{
					TaskID: rt.ID,
					Action: "conflict_local_wins",
					Detail: "local=" + existing.Status + " remote=" + newColumn,
				})
			} else {
				existing.Status = newColumn
			}
		}

		existing.RemoteMeta = encodeRemoteMeta(existingMeta)
		if err := s.store.UpdateTask(ctx, *existing); err != nil {
			return nil, err
		}
		synced++
	}

	// Mark stale tasks (synced tasks present locally but absent from remote results)
	allTasks, err := s.store.ListAllTasks(ctx)
	if err != nil {
		return nil, err
	}
	for _, task := range allTasks {
		if task.Provider == nil || seenIDs[task.ID] {
			continue // skip local tasks and seen tasks
		}
		meta := parseRemoteMeta(task.RemoteMeta)
		if meta.StaleAt == "" {
			meta.StaleAt = time.Now().UTC().Format(time.RFC3339)
			task.RemoteMeta = encodeRemoteMeta(meta)
			s.store.UpdateTask(ctx, task)
		}
	}

	return &SyncResult{TasksSynced: synced}, nil
}

func (s *syncService) isWithinConflictWindow(meta remoteMeta) bool {
	if meta.LocalMoveAt == "" {
		return false
	}
	moveAt, err := time.Parse(time.RFC3339, meta.LocalMoveAt)
	if err != nil {
		return false
	}
	return time.Since(moveAt) < conflictWindow
}

// PushMove updates the local task immediately and queues an async remote transition.
func (s *syncService) PushMove(ctx context.Context, taskID, targetColumn string) error {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	// Update local state immediately
	now := time.Now().UTC().Format(time.RFC3339)
	task.Status = targetColumn
	task.UpdatedAt = now

	// Update local_move_at in remote_meta (only for synced tasks)
	if task.Provider != nil {
		meta := parseRemoteMeta(task.RemoteMeta)
		meta.LocalMoveAt = now
		task.RemoteMeta = encodeRemoteMeta(meta)
	}

	if err := s.store.UpdateTask(ctx, *task); err != nil {
		return err
	}

	// Skip push for local tasks (no provider)
	if task.Provider == nil {
		return nil
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
	go s.executePush(taskID, transitionID, targetColumn)
	return nil
}

func (s *syncService) executePush(taskID, transitionID, targetColumn string) {
	ctx := context.Background()

	err := s.provider.DoTransition(ctx, taskID, transitionID)
	if err != nil {
		// Push failed — log and set warning
		s.store.InsertSyncLog(ctx, store.SyncLogEntry{
			TaskID: taskID,
			Action: "push_failed",
			Detail: err.Error(),
		})

		s.broadcast(SyncEvent{Type: EventSyncFailed, Message: "push failed: " + err.Error()})
		s.bus.Publish(events.Event{Type: events.EventSyncFailed, Payload: err.Error(), At: time.Now()})
		// Publish specific transition failure event
		s.bus.Publish(events.Event{
			Type: events.EventTransitionFailed,
			Payload: events.ErrorPayload{
				ErrorType: "transition_failed",
				Message:   err.Error(),
				TicketKey: taskID,
			},
			At: time.Now(),
		})
		return
	}

	// Push succeeded — update remote status in remote_meta
	task, getErr := s.store.GetTask(ctx, taskID)
	if getErr != nil {
		return
	}

	meta := parseRemoteMeta(task.RemoteMeta)

	// Look up what statuses map to the target column and use the first one
	mappings, _ := s.store.ListColumnMappings(ctx)
	for _, m := range mappings {
		if m.ColumnName == targetColumn {
			var statuses []string
			json.Unmarshal([]byte(m.RemoteStatuses), &statuses)
			if len(statuses) > 0 {
				meta.RemoteStatus = statuses[0]
			}
			break
		}
	}

	task.RemoteMeta = encodeRemoteMeta(meta)
	task.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	s.store.UpdateTask(ctx, *task)

	s.store.InsertSyncLog(ctx, store.SyncLogEntry{
		TaskID: taskID,
		Action: "push_success",
		Detail: "transitioned to " + targetColumn,
	})
}

// retryFailedPushes retries any pending failed push transitions.
func (s *syncService) retryFailedPushes(ctx context.Context) {
	allTasks, err := s.store.ListAllTasks(ctx)
	if err != nil {
		return
	}

	mappings, _ := s.store.ListColumnMappings(ctx)

	for _, task := range allTasks {
		// Skip local tasks
		if task.Provider == nil {
			continue
		}

		meta := parseRemoteMeta(task.RemoteMeta)

		// Check if the task's local column has a different remote status than expected
		expectedColumn := resolveColumn(meta.RemoteStatus, mappings)
		if expectedColumn == task.Status {
			continue // In sync
		}

		// Find the transition ID for the task's current local column
		for _, m := range mappings {
			if m.ColumnName == task.Status && m.RemoteTransition != "" {
				s.executePush(task.ID, m.RemoteTransition, task.Status)
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

func (s *syncService) SearchRemote(ctx context.Context, query string) ([]RemoteSearchResult, error) {
	if len(strings.TrimSpace(query)) < 2 {
		return nil, nil
	}
	jql := buildSearchJQL(s.projectKeys, query)
	tickets, err := s.provider.Search(ctx, jql)
	if err != nil {
		return nil, err
	}
	results := make([]RemoteSearchResult, len(tickets))
	for i, t := range tickets {
		results[i] = RemoteSearchResult{
			ID:        t.ID,
			Summary:   t.Summary,
			Status:    t.Status,
			Priority:  t.Priority,
			IssueType: t.IssueType,
		}
	}
	return results, nil
}

func (s *syncService) ImportRemoteTask(ctx context.Context, ticketID string) (*Card, error) {
	// Check if already tracked locally
	existing, err := s.store.GetTask(ctx, ticketID)
	if err == nil {
		return &Card{
			ID:       existing.ID,
			Title:    existing.Title,
			Priority: existing.Priority,
			Status:   existing.Status,
		}, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	// Fetch from remote
	rt, err := s.provider.GetTicket(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if rt == nil {
		return nil, fmt.Errorf("ticket %s not found on remote", ticketID)
	}

	// Resolve column
	mappings, err := s.store.ListColumnMappings(ctx)
	if err != nil {
		return nil, err
	}
	column := resolveColumn(rt.Status, mappings)
	if column == "" {
		column = "Backlog"
	}

	// Create local task
	now := time.Now().UTC().Format(time.RFC3339)
	provider := "jira"
	remoteID := rt.ID
	labels := strings.Join(rt.Labels, ",")
	meta := remoteMeta{
		RemoteStatus:    rt.Status,
		RemoteUpdatedAt: rt.UpdatedAt.UTC().Format(time.RFC3339),
		IssueType:       rt.IssueType,
		Assignee:        rt.Assignee,
		Labels:          labels,
		EpicKey:         rt.EpicKey,
		EpicName:        rt.EpicName,
		URL:             rt.URL,
	}
	task := store.Task{
		ID:            rt.ID,
		Title:         rt.Summary,
		DescriptionMD: rt.DescriptionMD,
		Status:        column,
		Priority:      rt.Priority,
		Provider:      &provider,
		RemoteID:      &remoteID,
		RemoteMeta:    encodeRemoteMeta(meta),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.store.CreateTask(ctx, task); err != nil {
		return nil, err
	}

	s.bus.Publish(events.Event{
		Type: events.EventCardsRefreshed,
		At:   time.Now(),
	})

	return &Card{
		ID:       rt.ID,
		Title:    rt.Summary,
		Priority: rt.Priority,
		Status:   column,
	}, nil
}

func buildSearchJQL(projectKeys []string, text string) string {
	var parts []string
	if len(projectKeys) > 0 {
		parts = append(parts, "project in ("+strings.Join(projectKeys, ", ")+")")
	}
	if text != "" {
		// Use summary search + key match. summary ~ is more reliable than
		// text ~ (which searches comments too and is slow/finicky).
		// Also support direct key lookup (e.g. "REX-123").
		escaped := strings.ReplaceAll(text, `"`, `\"`)
		parts = append(parts, fmt.Sprintf(`(summary ~ "%s" OR key = "%s")`, escaped, escaped))
	}
	return strings.Join(parts, " AND ") + " ORDER BY updated DESC"
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
