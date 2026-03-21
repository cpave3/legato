package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

type boardService struct {
	store *store.Store
	bus   *events.Bus
}

// NewBoardService creates a BoardService backed by the given store and event bus.
func NewBoardService(s *store.Store, bus *events.Bus) BoardService {
	return &boardService{store: s, bus: bus}
}

func (b *boardService) ListColumns(ctx context.Context) ([]Column, error) {
	mappings, err := b.store.ListColumnMappings(ctx)
	if err != nil {
		return nil, err
	}
	cols := make([]Column, len(mappings))
	for i, m := range mappings {
		cols[i] = Column{Name: m.ColumnName, SortOrder: m.SortOrder}
	}
	return cols, nil
}

func (b *boardService) ListCards(ctx context.Context, column string) ([]Card, error) {
	return b.ListCardsByWorkspace(ctx, column, store.WorkspaceView{Kind: store.ViewAll})
}

func (b *boardService) ListCardsByWorkspace(ctx context.Context, column string, view store.WorkspaceView) ([]Card, error) {
	// Validate column exists
	mappings, err := b.store.ListColumnMappings(ctx)
	if err != nil {
		return nil, err
	}
	found := false
	for _, m := range mappings {
		if m.ColumnName == column {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("column %q not found", column)
	}

	tasks, err := b.store.ListTasksByStatusAndWorkspace(ctx, column, view)
	if err != nil {
		return nil, err
	}

	// Build workspace lookup for populating card workspace names
	var wsLookup map[int]store.Workspace
	if view.Kind == store.ViewAll {
		workspaces, wsErr := b.store.ListWorkspaces(ctx)
		if wsErr == nil {
			wsLookup = make(map[int]store.Workspace, len(workspaces))
			for _, w := range workspaces {
				wsLookup[w.ID] = w
			}
		}
	}

	cards := make([]Card, len(tasks))
	for i, t := range tasks {
		issueType := ""
		if t.RemoteMeta != nil {
			var meta map[string]string
			if json.Unmarshal([]byte(*t.RemoteMeta), &meta) == nil {
				issueType = meta["issue_type"]
			}
		}
		provider := ""
		if t.Provider != nil {
			provider = *t.Provider
		}
		cards[i] = Card{
			ID:         t.ID,
			Title:      t.Title,
			Priority:   t.Priority,
			IssueType:  issueType,
			Status:     t.Status,
			Provider:   provider,
			SortOrder:  t.SortOrder,
			HasWarning: hasPushFailure(ctx, b.store, t.ID),
		}
		if wsLookup != nil && t.WorkspaceID != nil {
			if ws, ok := wsLookup[*t.WorkspaceID]; ok {
				cards[i].WorkspaceName = ws.Name
				if ws.Color != nil {
					cards[i].WorkspaceColor = *ws.Color
				}
			}
		}
	}
	return cards, nil
}

func (b *boardService) GetCard(ctx context.Context, id string) (*CardDetail, error) {
	t, err := b.store.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	return taskToCardDetail(t), nil
}

func (b *boardService) MoveCard(ctx context.Context, id string, targetColumn string) error {
	t, err := b.store.GetTask(ctx, id)
	if err != nil {
		return err
	}

	// No-op if already in target column
	if t.Status == targetColumn {
		return nil
	}

	// Validate target column exists
	mappings, err := b.store.ListColumnMappings(ctx)
	if err != nil {
		return err
	}
	found := false
	for _, m := range mappings {
		if m.ColumnName == targetColumn {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("column %q not found", targetColumn)
	}

	// Find max sort_order in target column to place at end
	targetCards, err := b.store.ListTasksByStatus(ctx, targetColumn)
	if err != nil {
		return err
	}
	newOrder := 0
	for _, c := range targetCards {
		if c.SortOrder >= newOrder {
			newOrder = c.SortOrder + 1
		}
	}

	fromColumn := t.Status
	t.Status = targetColumn
	t.SortOrder = newOrder
	t.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := b.store.UpdateTask(ctx, *t); err != nil {
		return err
	}

	b.bus.Publish(events.Event{
		Type:    events.EventCardMoved,
		Payload: map[string]string{"id": id, "from": fromColumn, "to": targetColumn},
		At:      time.Now(),
	})
	return nil
}

func (b *boardService) ReorderCard(ctx context.Context, id string, newPosition int) error {
	t, err := b.store.GetTask(ctx, id)
	if err != nil {
		return err
	}

	cards, err := b.store.ListTasksByStatus(ctx, t.Status)
	if err != nil {
		return err
	}

	// Clamp position
	if newPosition < 0 {
		newPosition = 0
	}
	if newPosition >= len(cards) {
		newPosition = len(cards) - 1
	}

	// Rebuild sort order: remove the card, insert at new position
	filtered := make([]store.Task, 0, len(cards)-1)
	for _, c := range cards {
		if c.ID != id {
			filtered = append(filtered, c)
		}
	}

	// Insert at new position
	reordered := make([]store.Task, 0, len(cards))
	for i, c := range filtered {
		if i == newPosition {
			reordered = append(reordered, *t)
		}
		reordered = append(reordered, c)
	}
	// If newPosition is at the end
	if newPosition >= len(filtered) {
		reordered = append(reordered, *t)
	}

	// Update sort orders
	for i, c := range reordered {
		c.SortOrder = i
		c.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := b.store.UpdateTask(ctx, c); err != nil {
			return err
		}
	}

	b.bus.Publish(events.Event{
		Type:    events.EventCardUpdated,
		Payload: map[string]string{"id": id},
		At:      time.Now(),
	})
	return nil
}

func (b *boardService) SearchCards(ctx context.Context, query string) ([]Card, error) {
	mappings, err := b.store.ListColumnMappings(ctx)
	if err != nil {
		return nil, err
	}

	var results []Card
	for _, m := range mappings {
		tasks, err := b.store.ListTasksByStatus(ctx, m.ColumnName)
		if err != nil {
			return nil, err
		}
		for _, t := range tasks {
			if query == "" || containsInsensitive(t.ID, query) || containsInsensitive(t.Title, query) {
				issueType := ""
				if t.RemoteMeta != nil {
					var meta map[string]string
					if json.Unmarshal([]byte(*t.RemoteMeta), &meta) == nil {
						issueType = meta["issue_type"]
					}
				}
				results = append(results, Card{
					ID:        t.ID,
					Title:     t.Title,
					Priority:  t.Priority,
					IssueType: issueType,
					Status:    t.Status,
					SortOrder: t.SortOrder,
				})
			}
		}
	}
	return results, nil
}

func (b *boardService) ExportCardContext(ctx context.Context, id string, format ExportFormat) (string, error) {
	t, err := b.store.GetTask(ctx, id)
	if err != nil {
		return "", err
	}
	detail := taskToCardDetail(t)

	switch format {
	case ExportFormatDescription:
		return formatDescription(detail), nil
	case ExportFormatFull:
		return formatFull(detail), nil
	default:
		return "", fmt.Errorf("unsupported export format: %d", format)
	}
}

func (b *boardService) CreateTask(ctx context.Context, title, description, column, priority string, workspaceID *int) (*Card, error) {
	id := store.GenerateTaskID()
	now := time.Now().UTC().Format(time.RFC3339)

	task := store.Task{
		ID:            id,
		Title:         title,
		Description:   description,
		DescriptionMD: description,
		Status:        column,
		Priority:      priority,
		WorkspaceID:   workspaceID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	// Place at end of column
	existing, err := b.store.ListTasksByStatus(ctx, column)
	if err == nil {
		maxOrder := 0
		for _, t := range existing {
			if t.SortOrder >= maxOrder {
				maxOrder = t.SortOrder + 1
			}
		}
		task.SortOrder = maxOrder
	}

	if err := b.store.CreateTask(ctx, task); err != nil {
		return nil, err
	}

	b.bus.Publish(events.Event{
		Type:    events.EventCardsRefreshed,
		At:      time.Now(),
	})

	return &Card{
		ID:        id,
		Title:     title,
		Priority:  priority,
		Status:    column,
		SortOrder: task.SortOrder,
	}, nil
}

func (b *boardService) DeleteTask(ctx context.Context, id string) error {
	// Verify the task exists
	if _, err := b.store.GetTask(ctx, id); err != nil {
		return err
	}

	if err := b.store.DeleteTask(ctx, id); err != nil {
		return err
	}

	b.bus.Publish(events.Event{
		Type: events.EventCardsRefreshed,
		At:   time.Now(),
	})
	return nil
}

func (b *boardService) UpdateTaskDescription(ctx context.Context, id, description string) error {
	t, err := b.store.GetTask(ctx, id)
	if err != nil {
		return err
	}
	if t.Provider != nil {
		return fmt.Errorf("cannot edit description of remote task %s", id)
	}
	t.Description = description
	t.DescriptionMD = description
	t.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := b.store.UpdateTask(ctx, *t); err != nil {
		return err
	}
	b.bus.Publish(events.Event{
		Type: events.EventCardsRefreshed,
		At:   time.Now(),
	})
	return nil
}

func (b *boardService) UpdateTaskTitle(ctx context.Context, id, title string) error {
	if title == "" {
		return fmt.Errorf("title cannot be empty")
	}
	t, err := b.store.GetTask(ctx, id)
	if err != nil {
		return err
	}
	if t.Provider != nil {
		return fmt.Errorf("cannot edit title of remote task %s", id)
	}
	t.Title = title
	t.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := b.store.UpdateTask(ctx, *t); err != nil {
		return err
	}
	b.bus.Publish(events.Event{
		Type: events.EventCardsRefreshed,
		At:   time.Now(),
	})
	return nil
}

func (b *boardService) UpdateTaskWorkspace(ctx context.Context, id string, workspaceID *int) error {
	if _, err := b.store.GetTask(ctx, id); err != nil {
		return err
	}
	if err := b.store.UpdateTaskWorkspace(ctx, id, workspaceID); err != nil {
		return err
	}
	b.bus.Publish(events.Event{
		Type: events.EventCardsRefreshed,
		At:   time.Now(),
	})
	return nil
}

func (b *boardService) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	ws, err := b.store.ListWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]Workspace, len(ws))
	for i, w := range ws {
		result[i] = Workspace{ID: w.ID, Name: w.Name}
		if w.Color != nil {
			result[i].Color = *w.Color
		}
	}
	return result, nil
}

// doneColumnName finds the column named "Done" (case-insensitive).
func (b *boardService) doneColumnName(ctx context.Context) (string, error) {
	mappings, err := b.store.ListColumnMappings(ctx)
	if err != nil {
		return "", err
	}
	for _, m := range mappings {
		if toLower(m.ColumnName) == "done" {
			return m.ColumnName, nil
		}
	}
	return "", fmt.Errorf("no Done column found")
}

func (b *boardService) ArchiveDoneCards(ctx context.Context) (int, error) {
	doneCol, err := b.doneColumnName(ctx)
	if err != nil {
		return 0, err
	}
	count, err := b.store.ArchiveTasksByStatus(ctx, doneCol)
	if err != nil {
		return 0, err
	}
	b.bus.Publish(events.Event{
		Type: events.EventCardsRefreshed,
		At:   time.Now(),
	})
	return int(count), nil
}

func (b *boardService) ArchiveTask(ctx context.Context, id string) error {
	t, err := b.store.GetTask(ctx, id)
	if err != nil {
		return err
	}
	doneCol, err := b.doneColumnName(ctx)
	if err != nil {
		return err
	}
	if t.Status != doneCol {
		return fmt.Errorf("can only archive tasks in %q column", doneCol)
	}
	if err := b.store.ArchiveTask(ctx, id); err != nil {
		return err
	}
	b.bus.Publish(events.Event{
		Type: events.EventCardsRefreshed,
		At:   time.Now(),
	})
	return nil
}

func (b *boardService) CountDoneCards(ctx context.Context) (int, error) {
	doneCol, err := b.doneColumnName(ctx)
	if err != nil {
		return 0, err
	}
	tasks, err := b.store.ListTasksByStatus(ctx, doneCol)
	if err != nil {
		return 0, err
	}
	return len(tasks), nil
}

func taskToCardDetail(t *store.Task) *CardDetail {
	createdAt, _ := time.Parse(time.RFC3339, t.CreatedAt)
	updatedAt, _ := time.Parse(time.RFC3339, t.UpdatedAt)

	detail := &CardDetail{
		ID:            t.ID,
		Title:         t.Title,
		DescriptionMD: t.DescriptionMD,
		Status:        t.Status,
		Priority:      t.Priority,
		WorkspaceID:   t.WorkspaceID,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}

	if t.Provider != nil {
		detail.Provider = *t.Provider
	}
	if t.RemoteID != nil {
		detail.RemoteID = *t.RemoteID
	}
	if t.RemoteMeta != nil {
		var meta map[string]string
		if json.Unmarshal([]byte(*t.RemoteMeta), &meta) == nil {
			detail.RemoteMeta = meta
		}
	}

	if t.PRMeta != nil {
		prMeta, err := store.ParsePRMeta(t.PRMeta)
		if err == nil && prMeta != nil {
			detail.PRMeta = &PRMetaView{
				Branch:         prMeta.Branch,
				PRNumber:       prMeta.PRNumber,
				PRURL:          prMeta.PRURL,
				State:          prMeta.State,
				IsDraft:        prMeta.IsDraft,
				ReviewDecision: prMeta.ReviewDecision,
				CheckStatus:    prMeta.CheckStatus,
				CommentCount:   prMeta.CommentCount,
			}
		}
	}

	return detail
}

// hasPushFailure checks if the most recent sync_log entry for a task is a push_failed.
func hasPushFailure(ctx context.Context, s *store.Store, taskID string) bool {
	logs, err := s.ListSyncLogs(ctx, taskID)
	if err != nil || len(logs) == 0 {
		return false
	}
	return logs[0].Action == "push_failed"
}

func containsInsensitive(s, substr string) bool {
	return len(s) >= len(substr) &&
		contains(toLower(s), toLower(substr))
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func contains(s, substr string) bool {
	if substr == "" {
		return true
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
