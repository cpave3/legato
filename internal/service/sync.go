package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/attachments"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

const (
	conflictWindow         = 5 * time.Minute
	trackedTicketBatchSize = 50
)

type syncService struct {
	store       *store.Store
	bus         *events.Bus
	sources     []TicketSource
	attachments *attachments.Cache

	mu       sync.Mutex
	last     time.Time
	lastPull map[string]time.Time
	subs     []chan SyncEvent
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
	Size             string `json:"size,omitempty"`
	Uncertainty      string `json:"uncertainty,omitempty"`
	BlockedBy        string `json:"blocked_by,omitempty"`
	Resolution       string `json:"resolution,omitempty"`
}

// setTicketFields copies what a ticket says about itself into the meta.
func (m *remoteMeta) setTicketFields(rt RemoteTicket) {
	m.RemoteStatus = rt.Status
	m.RemoteUpdatedAt = rt.UpdatedAt.UTC().Format(time.RFC3339Nano)
	m.IssueType = rt.IssueType
	m.Assignee = rt.Assignee
	m.Labels = strings.Join(rt.Labels, ",")
	m.EpicKey = rt.EpicKey
	m.EpicName = rt.EpicName
	m.URL = rt.URL
	m.Size = rt.Size
	m.Uncertainty = rt.Uncertainty
	m.BlockedBy = strings.Join(rt.BlockedBy, ",")
	m.Resolution = rt.Resolution
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

// NewSyncService creates a SyncService backed by Jira alone.
func NewSyncService(s *store.Store, bus *events.Bus, provider TicketProvider, jql string, projectKeys []string, interval time.Duration, statusSync bool, attachmentCache ...*attachments.Cache) SyncService {
	var cache *attachments.Cache
	if len(attachmentCache) > 0 {
		cache = attachmentCache[0]
	}
	return NewTicketSync(s, bus, cache, NewJiraSource(provider, jql, projectKeys, interval, statusSync))
}

// NewTicketSync creates a SyncService over every ticket source. Each task is
// synced by the source whose name is its provider. cache may be nil.
func NewTicketSync(s *store.Store, bus *events.Bus, cache *attachments.Cache, sources ...TicketSource) SyncService {
	for _, source := range sources {
		if b, ok := source.(storeBinder); ok {
			b.bindStore(s)
		}
	}
	return &syncService{store: s, bus: bus, sources: sources, attachments: cache, lastPull: map[string]time.Time{}}
}

// jira is the Jira source, nil when there is none.
func (s *syncService) jira() *jiraSource {
	for _, source := range s.sources {
		if j, ok := source.(*jiraSource); ok && j.provider != nil {
			return j
		}
	}
	return nil
}

func (s *syncService) source(name string) TicketSource {
	for _, source := range s.sources {
		if source.Name() == name {
			return source
		}
	}
	return nil
}

func (s *syncService) Sync(ctx context.Context) (*SyncResult, error) {
	return s.sync(ctx, false)
}

// sync pulls every enabled source, or with due only those whose interval
// has passed since they were last pulled. One that fails leaves the others.
func (s *syncService) sync(ctx context.Context, due bool) (*SyncResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.broadcast(SyncEvent{Type: EventSyncStarted, Message: "sync started"})
	s.bus.Publish(events.Event{Type: events.EventSyncStarted, At: time.Now()})

	result := &SyncResult{Sources: map[string]SourceSyncResult{}}
	var errs []error
	for _, source := range s.sources {
		name := source.Name()
		if !source.Enabled(ctx) {
			result.Sources[name] = SourceSyncResult{Disabled: true}
			continue
		}
		if due && time.Since(s.lastPull[name]) < source.Interval() {
			continue
		}
		synced, err := s.pullSource(ctx, source)
		if err != nil {
			result.Sources[name] = SourceSyncResult{Err: err}
			errs = append(errs, err)
			s.publishSyncError(err)
			continue
		}
		s.lastPull[name] = time.Now()
		result.Sources[name] = SourceSyncResult{TasksSynced: synced}
		result.TasksSynced += synced
	}
	if len(errs) > 0 && result.TasksSynced == 0 && len(errs) == s.enabledCount(ctx) {
		return result, errors.Join(errs...)
	}

	// Retry any failed push transitions
	s.retryFailedPushes(ctx)

	s.last = time.Now()

	s.broadcast(SyncEvent{Type: EventCardsRefreshed, Message: "cards refreshed"})
	s.bus.Publish(events.Event{Type: events.EventCardsRefreshed, At: time.Now()})

	s.broadcast(SyncEvent{Type: EventSyncCompleted, Message: "sync completed"})
	s.bus.Publish(events.Event{Type: events.EventSyncCompleted, At: time.Now()})

	return result, errors.Join(errs...)
}

func (s *syncService) enabledCount(ctx context.Context) int {
	n := 0
	for _, source := range s.sources {
		if source.Enabled(ctx) {
			n++
		}
	}
	return n
}

func (s *syncService) publishSyncError(err error) {
	s.broadcast(SyncEvent{Type: EventSyncFailed, Message: err.Error()})
	s.bus.Publish(events.Event{Type: events.EventSyncFailed, Payload: err.Error(), At: time.Now()})
	errType := classifyError(err)
	s.bus.Publish(events.Event{
		Type: errType,
		Payload: events.ErrorPayload{
			ErrorType: errorTypeName(errType),
			Message:   err.Error(),
		},
		At: time.Now(),
	})
}

// pullSource refreshes the tasks that are the source's, and says how many
// changed.
func (s *syncService) pullSource(ctx context.Context, source TicketSource) (int, error) {
	allTasks, err := s.store.ListAllTasks(ctx)
	if err != nil {
		return 0, err
	}
	name := source.Name()
	trackedTasks := make([]store.Task, 0, len(allTasks))
	for _, task := range allTasks {
		if task.ArchivedAt == nil && task.Provider != nil && *task.Provider == name {
			trackedTasks = append(trackedTasks, task)
		}
	}
	if len(trackedTasks) == 0 {
		return 0, nil
	}

	batch, err := source.Pull(ctx, trackedTasks)
	if err != nil {
		return 0, err
	}

	// Load column mappings for local-only columns
	mappings, err := s.store.ListColumnMappings(ctx)
	if err != nil {
		return 0, err
	}

	synced := 0
	for _, rt := range batch.Tickets {
		existing, err := s.store.GetTask(ctx, rt.ID)
		if errors.Is(err, store.ErrNotFound) {
			// Not tracked locally — skip. New tasks are only added via manual import.
			continue
		} else if err != nil {
			return 0, err
		}

		// Skip archived tasks, and a task with this key from another source.
		if existing.ArchivedAt != nil || existing.Provider == nil || *existing.Provider != name {
			continue
		}

		if err := s.reconcileAttachments(ctx, source, rt); err != nil {
			return 0, err
		}

		// Existing task — check if remote updated
		existingMeta := parseRemoteMeta(existing.RemoteMeta)
		existingUpdated, _ := time.Parse(time.RFC3339, existingMeta.RemoteUpdatedAt)
		if !rt.UpdatedAt.After(existingUpdated) {
			// Clear stale marker since task is still in results
			if existingMeta.StaleAt != "" {
				existingMeta.StaleAt = ""
				existing.RemoteMeta = encodeRemoteMeta(existingMeta)
				_ = s.store.UpdateTaskRemoteMeta(ctx, existing.ID, existing.RemoteMeta)
			}
			continue
		}

		// Update fields from remote
		existing.Title = rt.Summary
		existing.RemoteDescriptionMD = rt.DescriptionMD
		existing.Priority = rt.Priority
		existing.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

		existingMeta.setTicketFields(rt)
		existingMeta.StaleAt = "" // clear stale marker

		// Check if status/column changed
		newColumn, err := source.Column(ctx, rt)
		if err != nil {
			return 0, err
		}
		if source.MovesCards() && newColumn != "" && newColumn != existing.Status {
			// Columns with no remote statuses are local-only. Jira updates may
			// refresh the task fields, but must not move the card out of them.
			if isLocalOnlyColumn(existing.Status, mappings) {
				// Preserve the local column indefinitely.
			} else if s.isWithinConflictWindow(existingMeta) {
				// Check conflict window: if local move is recent, preserve local.
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
		if err := s.store.UpdateTaskFromProvider(ctx, *existing); err != nil {
			return 0, err
		}
		synced++
	}

	// Mark tasks the provider no longer has as stale.
	missing := make(map[string]bool, len(batch.Missing))
	for _, key := range batch.Missing {
		missing[key] = true
	}
	for _, task := range trackedTasks {
		if !missing[task.ID] {
			continue
		}
		meta := parseRemoteMeta(task.RemoteMeta)
		if meta.StaleAt == "" {
			meta.StaleAt = time.Now().UTC().Format(time.RFC3339)
			task.RemoteMeta = encodeRemoteMeta(meta)
			s.store.UpdateTaskRemoteMeta(ctx, task.ID, task.RemoteMeta)
		}
	}

	return synced, nil
}

func (s *syncService) reconcileAttachments(ctx context.Context, source TicketSource, ticket RemoteTicket) error {
	if s.attachments == nil {
		return nil
	}
	downloader, ok := downloader(source)
	if !ok {
		return nil
	}
	items := make([]attachments.Metadata, len(ticket.Attachments))
	for i, item := range ticket.Attachments {
		items[i] = attachments.Metadata{ID: item.ID, Filename: item.Filename, MimeType: item.MimeType, Size: item.Size}
	}
	return s.attachments.Reconcile(ctx, ticket.ID, items, downloader)
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
	if err := moveCard(ctx, s.store, s.bus, taskID, targetColumn, true); err != nil {
		return err
	}

	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	// Only Jira takes card moves; other providers' cards move locally.
	jira := s.jira()
	if task.Provider == nil || jira == nil || *task.Provider != jira.Name() {
		return nil
	}
	if !jira.statusSync {
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

	jira := s.jira()
	if jira == nil {
		return
	}
	err := jira.provider.DoTransition(ctx, taskID, transitionID)
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
	jira := s.jira()
	if jira == nil || !jira.statusSync {
		return
	}
	allTasks, err := s.store.ListAllTasks(ctx)
	if err != nil {
		return
	}

	mappings, _ := s.store.ListColumnMappings(ctx)

	for _, task := range allTasks {
		// Only Jira's tasks are pushed
		if task.Provider == nil || *task.Provider != jira.Name() {
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
		ticker := time.NewTicker(s.tick())
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.sync(ctx, true)
			case <-ctx.Done():
				return
			}
		}
	}()
	return cancel
}

// tick is how often the scheduler looks for a source that is due: the
// shortest interval.
func (s *syncService) tick() time.Duration {
	tick := time.Duration(0)
	for _, source := range s.sources {
		if i := source.Interval(); i > 0 && (tick == 0 || i < tick) {
			tick = i
		}
	}
	if tick == 0 {
		tick = time.Minute
	}
	return tick
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
	var results []RemoteSearchResult
	var firstErr error
	searched := 0
	for _, source := range s.sources {
		if !source.Enabled(ctx) {
			continue
		}
		searched++
		tickets, err := source.Search(ctx, query)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", source.Label(), err)
			}
			continue
		}
		for _, t := range tickets {
			results = append(results, RemoteSearchResult{
				ID:        t.ID,
				Summary:   t.Summary,
				Status:    t.Status,
				Priority:  t.Priority,
				IssueType: t.IssueType,
				Provider:  source.Name(),
			})
		}
	}
	if searched == 0 {
		return nil, ErrNoTicketProvider
	}
	// Results from the providers that answered, unless none did.
	if len(results) == 0 && firstErr != nil {
		if searched == 1 {
			return nil, errors.Unwrap(firstErr)
		}
		return nil, firstErr
	}
	return results, nil
}

// importSource is the source named, or the only enabled one when none is.
func (s *syncService) importSource(ctx context.Context, name string) (TicketSource, error) {
	if name != "" {
		source := s.source(name)
		if source == nil || !source.Enabled(ctx) {
			return nil, fmt.Errorf("%s is not connected: %w", name, ErrNoTicketProvider)
		}
		return source, nil
	}
	var enabled []TicketSource
	var names []string
	for _, source := range s.sources {
		if source.Enabled(ctx) {
			enabled = append(enabled, source)
			names = append(names, source.Name())
		}
	}
	switch len(enabled) {
	case 0:
		return nil, ErrNoTicketProvider
	case 1:
		return enabled[0], nil
	}
	return nil, fmt.Errorf("say which provider the ticket is from: %s", strings.Join(names, ", "))
}

func (s *syncService) ImportRemoteTask(ctx context.Context, providerName, ticketID string, workspaceID *int) (*Card, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	source, err := s.importSource(ctx, providerName)
	if err != nil {
		return nil, err
	}

	// Check if already tracked locally
	existing, err := s.store.GetTask(ctx, ticketID)
	if err == nil {
		if existing.Provider == nil || *existing.Provider != source.Name() {
			return nil, fmt.Errorf("%s: %w", ticketID, ErrTicketKeyTaken)
		}
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
	rt, err := source.Get(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if rt == nil {
		return nil, fmt.Errorf("ticket %s not found on remote", ticketID)
	}

	// Resolve column
	column, err := source.Column(ctx, *rt)
	if err != nil {
		return nil, err
	}
	if column == "" {
		column = "Backlog"
	}

	// Create local task
	now := time.Now().UTC().Format(time.RFC3339)
	provider := source.Name()
	remoteID := rt.ID
	var meta remoteMeta
	meta.setTicketFields(*rt)
	task := store.Task{
		ID:                  rt.ID,
		Title:               rt.Summary,
		RemoteDescriptionMD: rt.DescriptionMD,
		Status:              column,
		Priority:            rt.Priority,
		Provider:            &provider,
		RemoteID:            &remoteID,
		RemoteMeta:          encodeRemoteMeta(meta),
		WorkspaceID:         workspaceID,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := s.reconcileAttachments(ctx, source, *rt); err != nil {
		return nil, err
	}
	if err := s.store.CreateTask(ctx, task); err != nil {
		if s.attachments != nil {
			_ = s.attachments.Remove(rt.ID)
		}
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

func isLocalOnlyColumn(column string, mappings []store.ColumnMapping) bool {
	for _, mapping := range mappings {
		if mapping.ColumnName != column {
			continue
		}
		var statuses []string
		return json.Unmarshal([]byte(mapping.RemoteStatuses), &statuses) == nil && len(statuses) == 0
	}
	return false
}
