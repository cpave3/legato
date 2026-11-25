package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// The ticket and board steps (spec LEG-482 §6, LEG-488): ticket.import,
// tickets.list and tasks.list.

// errNoTickets: the step runs where no sync service is, such as a command
// that built the workflows without one.
var errNoTickets = errors.New("tickets are not available here: no ticket provider is set up in this Legato")

// TicketImportExecutor is ticket.import.
type TicketImportExecutor struct {
	importer TicketImporter
	store    *store.Store
}

func NewTicketImportExecutor(importer TicketImporter, db *store.Store) *TicketImportExecutor {
	return &TicketImportExecutor{importer: importer, store: db}
}

func (e *TicketImportExecutor) Type() string { return "ticket.import" }

func (e *TicketImportExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

func (e *TicketImportExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	key, err := workflowString(step.With, "key", true)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	// Ticket keys are upper case, as a provider writes them.
	key = strings.ToUpper(strings.TrimSpace(key))
	provider, err := workflowString(step.With, "provider", false)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	column, err := workflowString(step.With, "column", false)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	workspace, err := workflowString(step.With, "workspace", false)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	opts := ImportRemoteTaskOptions{Column: column}
	if strings.TrimSpace(workspace) != "" {
		w, err := workspaceNamed(ctx, e.store, workspace)
		if err != nil {
			return WorkflowExecutionResult{}, err
		}
		opts.WorkspaceID = &w.ID
	}
	if e.importer == nil {
		return WorkflowExecutionResult{}, errNoTickets
	}
	imported, err := e.importer.ImportTicket(ctx, strings.TrimSpace(provider), key, opts)
	if err != nil {
		return WorkflowExecutionResult{}, fmt.Errorf("importing %s: %w", key, err)
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{
		"task": imported.Card.ID, "created": imported.Created, "restored": imported.Restored,
	}}, nil
}

// workspaceNamed is the board's workspace of that name, matched without
// regard to case.
func workspaceNamed(ctx context.Context, db *store.Store, name string) (store.Workspace, error) {
	workspaces, err := db.ListWorkspaces(ctx)
	if err != nil {
		return store.Workspace{}, err
	}
	names := make([]string, 0, len(workspaces))
	for _, w := range workspaces {
		if strings.EqualFold(w.Name, strings.TrimSpace(name)) {
			return w, nil
		}
		names = append(names, w.Name)
	}
	return store.Workspace{}, fmt.Errorf("the board has no workspace %q; it has %s", name, strings.Join(names, ", "))
}

// listLimit reads a list step's limit: DefaultListLimit when not given, and
// from 1 to MaxListLimit.
func listLimit(with map[string]any) (int, error) {
	value, ok := with["limit"]
	if !ok {
		return workflowengine.DefaultListLimit, nil
	}
	limit, err := workflowInt(value)
	if err != nil {
		return 0, fmt.Errorf("limit %w", err)
	}
	if limit < 1 || limit > workflowengine.MaxListLimit {
		return 0, fmt.Errorf("limit must be from 1 to %d, not %d", workflowengine.MaxListLimit, limit)
	}
	return limit, nil
}

// listOutputs are a list step's outputs: its items as plain JSON values,
// and how many there are.
func listOutputs[T any](items []T) (map[string]any, error) {
	raw, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	data := []any{}
	if len(items) > 0 {
		if err := json.Unmarshal(raw, &data); err != nil {
			return nil, err
		}
	}
	return map[string]any{"items": data, "count": len(items)}, nil
}

// TicketsListExecutor is tickets.list.
type TicketsListExecutor struct{ lister TicketLister }

func NewTicketsListExecutor(lister TicketLister) *TicketsListExecutor {
	return &TicketsListExecutor{lister: lister}
}

func (e *TicketsListExecutor) Type() string { return "tickets.list" }

func (e *TicketsListExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

func (e *TicketsListExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	var q TicketQuery
	text := map[string]*string{"project": &q.Project, "milestone": &q.Milestone, "kind": &q.Kind, "state": &q.State}
	for name, into := range text {
		value, err := workflowString(step.With, name, false)
		if err != nil {
			return WorkflowExecutionResult{}, err
		}
		*into = strings.TrimSpace(value)
	}
	q.Project, q.Milestone = strings.ToUpper(q.Project), strings.ToUpper(q.Milestone)
	var err error
	if q.Frontier, err = workflowBool(step.With, "frontier"); err != nil {
		return WorkflowExecutionResult{}, err
	}
	if q.Ready, err = workflowBool(step.With, "ready"); err != nil {
		return WorkflowExecutionResult{}, err
	}
	if q.Limit, err = listLimit(step.With); err != nil {
		return WorkflowExecutionResult{}, err
	}
	if labels, ok := step.With["labels"]; ok {
		list, ok := labels.([]any)
		if !ok {
			return WorkflowExecutionResult{}, fmt.Errorf("labels must be a list of label names")
		}
		for _, label := range list {
			name, ok := label.(string)
			if !ok || strings.TrimSpace(name) == "" {
				return WorkflowExecutionResult{}, fmt.Errorf("labels must be a list of label names")
			}
			q.Labels = append(q.Labels, strings.TrimSpace(name))
		}
	}
	provider, err := workflowString(step.With, "provider", false)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if e.lister == nil {
		return WorkflowExecutionResult{}, errNoTickets
	}
	items, err := e.lister.ListTickets(ctx, strings.TrimSpace(provider), q)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	outputs, err := listOutputs(items)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: outputs}, nil
}

// TasksListExecutor is tasks.list: the board's tasks, column by column in
// board order, without archived tasks or (unless named) the workflow runs'
// own workspace.
type TasksListExecutor struct{ store *store.Store }

func NewTasksListExecutor(db *store.Store) *TasksListExecutor {
	return &TasksListExecutor{store: db}
}

func (e *TasksListExecutor) Type() string { return "tasks.list" }

func (e *TasksListExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

func (e *TasksListExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	limit, err := listLimit(step.With)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	var where *workflowengine.Condition
	if value, ok := step.With["where"]; ok {
		if where, err = workflowengine.TaskCondition(value); err != nil {
			return WorkflowExecutionResult{}, err
		}
	}
	columns, err := e.columns(ctx, step.With)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	view := store.WorkspaceView{Kind: store.ViewAll}
	workspaceName := map[int]string{}
	workspaces, err := e.store.ListWorkspaces(ctx)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	for _, w := range workspaces {
		workspaceName[w.ID] = w.Name
	}
	if name, _ := workflowString(step.With, "workspace", false); strings.TrimSpace(name) != "" {
		w, err := workspaceNamed(ctx, e.store, name)
		if err != nil {
			return WorkflowExecutionResult{}, err
		}
		view = store.WorkspaceView{Kind: store.ViewWorkspace, WorkspaceID: w.ID}
	}
	items := []map[string]any{}
	for _, column := range columns {
		tasks, err := e.store.ListTasksByStatusAndWorkspace(ctx, column, view)
		if err != nil {
			return WorkflowExecutionResult{}, err
		}
		for i := range tasks {
			item := workflowTaskContext(&tasks[i])
			item["workspace"] = ""
			if tasks[i].WorkspaceID != nil {
				item["workspace"] = workspaceName[*tasks[i].WorkspaceID]
			}
			if where != nil {
				matched, err := where.MatchTask(toJSONMap(item))
				if err != nil {
					return WorkflowExecutionResult{}, err
				}
				if !matched {
					continue
				}
			}
			items = append(items, item)
			if len(items) == limit {
				break
			}
		}
		if len(items) == limit {
			break
		}
	}
	outputs, err := listOutputs(items)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: outputs}, nil
}

// columns are the columns a tasks.list reads, in board order: the one
// named, or every one.
func (e *TasksListExecutor) columns(ctx context.Context, with map[string]any) ([]string, error) {
	mappings, err := e.store.ListColumnMappings(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(mappings))
	for i, m := range mappings {
		names[i] = m.ColumnName
	}
	column, err := workflowString(with, "column", false)
	if err != nil || strings.TrimSpace(column) == "" {
		return names, err
	}
	for _, name := range names {
		if strings.EqualFold(name, strings.TrimSpace(column)) {
			return []string{name}, nil
		}
	}
	return nil, fmt.Errorf("%w: %q; pick one of %s", ErrUnknownColumn, column, strings.Join(names, ", "))
}

// toJSONMap is a value as JSON reads it back, so a condition compares what
// a later step would read.
func toJSONMap(value map[string]any) map[string]any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil {
		return value
	}
	return out
}
