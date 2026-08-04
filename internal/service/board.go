package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cpave3/legato/internal/engine/attachments"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// ErrRemoteTaskReadOnly marks fields whose source of truth is a remote provider.
var ErrRemoteTaskReadOnly = errors.New("remote task field is read-only")

type boardService struct {
	store       *store.Store
	bus         *events.Bus
	attachments *attachments.Cache
}

// NewBoardService creates a BoardService backed by the given store and event bus.
func NewBoardService(s *store.Store, bus *events.Bus, attachmentCache ...*attachments.Cache) BoardService {
	cache := attachments.NewCache(attachments.DefaultRoot(), 0)
	if len(attachmentCache) > 0 && attachmentCache[0] != nil {
		cache = attachmentCache[0]
	}
	return &boardService{store: s, bus: bus, attachments: cache}
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
			ID:          t.ID,
			Title:       t.Title,
			Priority:    t.Priority,
			IssueType:   issueType,
			Status:      t.Status,
			Provider:    provider,
			SortOrder:   t.SortOrder,
			HasWarning:  hasPushFailure(ctx, b.store, t.ID),
			HasWorktree: t.WorktreePath != nil && *t.WorktreePath != "",
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

func (b *boardService) TaskActivity(ctx context.Context, id string) (*TaskActivity, error) {
	if _, err := b.store.GetTask(ctx, id); err != nil {
		return nil, err
	}
	reviews, err := b.store.ListReviewToursByTask(ctx, id)
	if err != nil {
		return nil, err
	}
	plans, err := b.store.ListPlansByTask(ctx, id)
	if err != nil {
		return nil, err
	}
	runs, err := b.store.ListWorkflowRunsByRelatedTask(ctx, id)
	if err != nil {
		return nil, err
	}
	activity := &TaskActivity{Reviews: reviews, Plans: plans, WorkflowRuns: make([]TaskWorkflowActivity, 0, len(runs))}
	for _, run := range runs {
		view, viewErr := b.store.GetWorkflowRun(ctx, run.ID)
		if viewErr != nil {
			return nil, viewErr
		}
		activity.WorkflowRuns = append(activity.WorkflowRuns, TaskWorkflowActivity{Run: run, Artifacts: view.Artifacts})
	}
	return activity, nil
}

func (b *boardService) GetCard(ctx context.Context, id string) (*CardDetail, error) {
	t, err := b.store.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	detail := taskToCardDetail(t)
	notes, err := b.store.ListTaskNotes(ctx, id)
	if err != nil {
		return nil, err
	}
	detail.Notes = notes
	if err := b.addAttachments(detail); err != nil {
		return nil, err
	}
	return detail, nil
}

func (b *boardService) MoveCard(ctx context.Context, id string, targetColumn string) error {
	return moveCard(ctx, b.store, b.bus, id, targetColumn, false)
}

func moveCard(ctx context.Context, taskStore *store.Store, bus *events.Bus, id string, targetColumn string, recordLocalMove bool) error {
	t, err := taskStore.GetTask(ctx, id)
	if err != nil {
		return err
	}

	// Validate target column exists
	mappings, err := taskStore.ListColumnMappings(ctx)
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

	// No-op locally if already in target column. Provider-aware moves still
	// refresh conflict metadata so a failed remote transition can be retried.
	if t.Status == targetColumn {
		if recordLocalMove && t.Provider != nil {
			t.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			meta := parseRemoteMeta(t.RemoteMeta)
			meta.LocalMoveAt = t.UpdatedAt
			t.RemoteMeta = encodeRemoteMeta(meta)
			return taskStore.UpdateTask(ctx, *t)
		}
		return nil
	}

	// Find max sort_order in target column to place at end
	targetCards, err := taskStore.ListTasksByStatus(ctx, targetColumn)
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
	if recordLocalMove && t.Provider != nil {
		meta := parseRemoteMeta(t.RemoteMeta)
		meta.LocalMoveAt = t.UpdatedAt
		t.RemoteMeta = encodeRemoteMeta(meta)
	}
	taskSnapshot, _ := json.Marshal(workflowTaskContext(t))
	transition := store.TaskTransition{ID: workflowID("tt-"), TaskID: id, Source: "local", FromColumn: fromColumn, ToColumn: targetColumn, TaskJSON: string(taskSnapshot)}
	if err := taskStore.RecordTaskTransition(ctx, transition, *t); err != nil {
		return err
	}

	if bus != nil {
		bus.Publish(events.Event{
			Type:    events.EventCardMoved,
			Payload: map[string]string{"id": id, "from": fromColumn, "to": targetColumn, "transition_id": transition.ID},
			At:      time.Now(),
		})
	}
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
	archived, err := b.store.ListArchivedTasks(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range archived {
		if query == "" || containsInsensitive(t.ID, query) || containsInsensitive(t.Title, query) {
			results = append(results, taskToCard(t))
		}
	}
	return results, nil
}

func taskToCard(t store.Task) Card {
	issueType := ""
	if t.RemoteMeta != nil {
		var meta map[string]string
		if json.Unmarshal([]byte(*t.RemoteMeta), &meta) == nil {
			issueType = meta["issue_type"]
		}
	}
	return Card{ID: t.ID, Title: t.Title, Priority: t.Priority, IssueType: issueType, Status: t.Status, SortOrder: t.SortOrder}
}

func (b *boardService) ExportCardContext(ctx context.Context, id string, format ExportFormat) (string, error) {
	t, err := b.store.GetTask(ctx, id)
	if err != nil {
		return "", err
	}
	detail := taskToCardDetail(t)
	notes, err := b.store.ListTaskNotes(ctx, id)
	if err != nil {
		return "", err
	}
	detail.Notes = notes
	if err := b.addAttachments(detail); err != nil {
		return "", err
	}

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
		Type: events.EventCardsRefreshed,
		At:   time.Now(),
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
		return fmt.Errorf("%w: cannot edit description of remote task %s", ErrRemoteTaskReadOnly, id)
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
		return fmt.Errorf("%w: cannot edit title of remote task %s", ErrRemoteTaskReadOnly, id)
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

func (b *boardService) SetTaskMetadata(ctx context.Context, taskID, path string, value any) error {
	parts, err := store.ValidateTaskMetadataPath(path)
	if err != nil {
		return err
	}
	if err := b.store.SetTaskMetadata(ctx, taskID, parts, value); err != nil {
		return err
	}
	b.bus.Publish(events.Event{Type: events.EventCardsRefreshed, At: time.Now()})
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
	tasks, err := b.store.ListTasksByStatus(ctx, doneCol)
	if err != nil {
		return 0, err
	}
	count, err := b.store.ArchiveTasksByStatus(ctx, doneCol)
	if err != nil {
		return 0, err
	}
	for _, task := range tasks {
		if err := b.attachments.Remove(task.ID); err != nil {
			return int(count), err
		}
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
	if err := b.attachments.Remove(id); err != nil {
		return err
	}
	b.bus.Publish(events.Event{
		Type: events.EventCardsRefreshed,
		At:   time.Now(),
	})
	return nil
}

func (b *boardService) ListArchivedTasks(ctx context.Context) ([]Card, error) {
	tasks, err := b.store.ListArchivedTasks(ctx)
	if err != nil {
		return nil, err
	}
	workspaces, err := b.store.ListWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	workspaceByID := make(map[int]store.Workspace, len(workspaces))
	for _, workspace := range workspaces {
		workspaceByID[workspace.ID] = workspace
	}
	cards := make([]Card, len(tasks))
	for i, task := range tasks {
		cards[i] = taskToCard(task)
		if task.WorkspaceID != nil {
			if workspace, ok := workspaceByID[*task.WorkspaceID]; ok {
				cards[i].WorkspaceName = workspace.Name
				if workspace.Color != nil {
					cards[i].WorkspaceColor = *workspace.Color
				}
			}
		}
	}
	return cards, nil
}

func (b *boardService) RestoreTask(ctx context.Context, id string) error {
	columns, err := b.store.ListColumnMappings(ctx)
	if err != nil {
		return err
	}
	if len(columns) == 0 {
		return fmt.Errorf("no configured columns")
	}
	if err := b.store.RestoreTask(ctx, id, columns[0].ColumnName); err != nil {
		return err
	}
	b.bus.Publish(events.Event{Type: events.EventCardsRefreshed, At: time.Now()})
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

func (b *boardService) addAttachments(detail *CardDetail) error {
	items, err := b.attachments.List(detail.ID)
	if err != nil {
		return err
	}
	for _, item := range items {
		detail.Attachments = append(detail.Attachments, LocalAttachment{Filename: item.Filename, MimeType: item.MimeType, Path: item.Path})
	}
	return nil
}

func valueOrEmpty(context *store.CodeContext, value func(*store.CodeContext) string) string {
	if context == nil {
		return ""
	}
	return value(context)
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
	detail.Metadata, _ = store.ParseTaskMetadata(t.Metadata)

	if t.WorktreePrimaryDir != nil && t.WorktreePath != nil && t.WorktreeBranch != nil && t.WorktreeBaseBranch != nil {
		detail.Worktree = &store.TaskWorktree{
			PrimaryDir: *t.WorktreePrimaryDir,
			Path:       *t.WorktreePath,
			Branch:     *t.WorktreeBranch,
			BaseBranch: *t.WorktreeBaseBranch,
		}
	}

	codeContext, _ := store.ParseCodeContext(t.CodeContext)
	if codeContext != nil {
		detail.CodeContext = &CodeContextView{
			Repo: codeContext.Repo, Branch: codeContext.Branch, HeadSHA: codeContext.HeadSHA,
			WorkingDir: codeContext.WorkingDir, BaseBranch: codeContext.BaseBranch,
			RepoSource: codeContext.RepoSource, BranchSource: codeContext.BranchSource,
		}
	}
	if t.PRMeta != nil {
		prMeta, err := store.ParsePRMeta(t.PRMeta)
		if err == nil && prMeta != nil {
			detail.PRMeta = &PRMetaView{
				Repo:           valueOrEmpty(codeContext, func(context *store.CodeContext) string { return context.Repo }),
				Branch:         valueOrEmpty(codeContext, func(context *store.CodeContext) string { return context.Branch }),
				PRNumber:       prMeta.PRNumber,
				PRURL:          prMeta.PRURL,
				State:          prMeta.State,
				IsDraft:        prMeta.IsDraft,
				ReviewDecision: prMeta.ReviewDecision,
				CheckStatus:    prMeta.CheckStatus,
				CommentCount:   prMeta.CommentCount,
				Pinned:         prMeta.Pinned,
				MatchMethod:    prMeta.MatchMethod,
				SyncOutcome:    prMeta.SyncOutcome,
				SyncError:      prMeta.SyncError,
			}
		}
	}

	detail.SwarmActiveStep = t.SwarmActiveStep

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
