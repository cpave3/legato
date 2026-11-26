package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/cpave3/legato/internal/engine/burnlite"
	"github.com/cpave3/legato/internal/engine/jira"
	"github.com/cpave3/legato/internal/engine/store"
)

// TicketUpdateStepType assigns a ticket and moves it to a status by name
// (LEG-656).
const TicketUpdateStepType = "ticket.update"

// TicketUpdateExecutor is ticket.update. It keeps no effect record, unlike
// ticket.create and ticket.comment: both of its changes read the ticket
// before writing and leave one already there alone, so a retry after a lost
// answer finds the change made and does nothing.
type TicketUpdateExecutor struct {
	store   *store.Store
	writers []TicketWriter
	me      *assigneeCache
}

func NewTicketUpdateExecutor(db *store.Store, writers ...TicketWriter) *TicketUpdateExecutor {
	return &TicketUpdateExecutor{store: db, writers: writers, me: newAssigneeCache()}
}

func (e *TicketUpdateExecutor) Type() string { return TicketUpdateStepType }

func (e *TicketUpdateExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

// assigneeCache keeps whom me resolved to for the latest runs, so the steps
// of one run ask Jira once.
type assigneeCache struct {
	mu    sync.Mutex
	order []string
	who   map[string]TicketPerson
}

// assigneeCacheRuns is how many runs' answers it keeps.
const assigneeCacheRuns = 32

func newAssigneeCache() *assigneeCache { return &assigneeCache{who: map[string]TicketPerson{}} }

// resolve resolves an assignee, me from the cache for the run when it can.
func (c *assigneeCache) resolve(ctx context.Context, runID string, u TicketUpdater, value string) (TicketPerson, error) {
	if !isMe(value) || runID == "" {
		return u.ResolveAssignee(ctx, value)
	}
	key := runID + "\x00" + u.Provider()
	c.mu.Lock()
	who, ok := c.who[key]
	c.mu.Unlock()
	if ok {
		return who, nil
	}
	who, err := u.ResolveAssignee(ctx, value)
	if err != nil {
		return who, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.who[key]; !ok {
		c.order = append(c.order, key)
		if len(c.order) > assigneeCacheRuns {
			delete(c.who, c.order[0])
			c.order = c.order[1:]
		}
	}
	c.who[key] = who
	return who, nil
}

// updaterFor is the writer a step names, as a TicketUpdater.
func updaterFor(ctx context.Context, writers []TicketWriter, provider string) (TicketUpdater, error) {
	writer, err := resolveTicketWriter(ctx, writers, provider)
	if err != nil {
		return nil, err
	}
	u, ok := writer.(TicketUpdater)
	if !ok {
		return nil, fmt.Errorf("%s cannot assign or move tickets here", ticketProviderLabel(writer.Provider()))
	}
	return u, nil
}

// warnable says optional may make an error a warning: no such ticket, a
// change the provider refused or cannot make. An outage, a timeout, a
// provider's internal error, or credentials the provider refused never
// are, so on_failure and retries still see them.
func warnable(err error) bool {
	var refusal *ticketRefusal
	if errors.As(err, &refusal) || errors.Is(err, errTicketNotFound) {
		return true
	}
	var jiraErr *jira.APIError
	if errors.As(err, &jiraErr) {
		return jiraErr.Status >= 400 && jiraErr.Status < 500 && jiraErr.Status != 401
	}
	var toolErr *burnlite.Error
	if errors.As(err, &toolErr) {
		switch toolErr.Code {
		case "not_found", "validation", "already_claimed", "conflict":
			return true
		}
	}
	return false
}

func (e *TicketUpdateExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	provider, err := trimmedString(step.With, "provider")
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	key, err := trimmedString(step.With, "key")
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if key = strings.ToUpper(key); key == "" {
		return WorkflowExecutionResult{}, errors.New("key is required")
	}
	assignee, err := trimmedString(step.With, "assignee")
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	status, err := trimmedString(step.With, "status")
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if assignee == "" && status == "" {
		return WorkflowExecutionResult{}, errors.New("give assignee or status: there is nothing to change")
	}
	optional, err := workflowBool(step.With, "optional")
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	updater, err := updaterFor(ctx, e.writers, provider)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}

	warnings := []string{}
	// carryOn records an error as a warning when optional allows it.
	carryOn := func(doing string, err error) error {
		err = ticketWriteError(doing, err)
		if optional && warnable(err) {
			warnings = append(warnings, err.Error())
			return nil
		}
		return err
	}

	var who *TicketPerson
	if assignee != "" && !isNone(assignee) {
		if updater.Provider() == BurnliteIntegration && !isMe(assignee) {
			// Refused before any call: no claim is made as another holder.
			_, err := updater.ResolveAssignee(ctx, assignee)
			return WorkflowExecutionResult{}, err
		}
		person, err := e.me.resolve(ctx, step.RunID, updater, assignee)
		if err != nil {
			if err := carryOn("finding the assignee "+assignee, err); err != nil {
				return WorkflowExecutionResult{}, err
			}
		} else {
			who = &person
		}
	}

	ticket, err := updater.Ticket(ctx, key)
	if err != nil {
		if err := carryOn("reading "+key, err); err != nil {
			return WorkflowExecutionResult{}, err
		}
		return ticketUpdated(TicketState{Key: key}, false, warnings), nil
	}

	changed := false
	switch {
	case ticket.Closed && status != "":
		// A closed Burnlite task holds no claim. With a status, MoveTo
		// either finds it already there, as on the retry of a step that
		// closed it, or refuses to reopen it.
	case who != nil && ticket.Assignee.ID != who.ID:
		if err := updater.Assign(ctx, ticket, *who); err != nil {
			if err := carryOn(fmt.Sprintf("assigning %s to %s", key, assignee), err); err != nil {
				return WorkflowExecutionResult{}, err
			}
			break
		}
		changed = true
		ticket.Assignee = *who
	case isNone(assignee) && ticket.Assignee.ID != "":
		if err := updater.Assign(ctx, ticket, TicketPerson{}); err != nil {
			if err := carryOn("unassigning "+key, err); err != nil {
				return WorkflowExecutionResult{}, err
			}
			break
		}
		changed = true
		ticket.Assignee = TicketPerson{}
	}

	if status != "" {
		after, moved, err := updater.MoveTo(ctx, ticket, status)
		if err != nil {
			if err := carryOn(fmt.Sprintf("moving %s to %s", key, status), err); err != nil {
				return WorkflowExecutionResult{}, err
			}
		}
		ticket = after
		changed = changed || moved
	}

	if ticket.Assignee.ID != "" && ticket.Assignee.Name == "" {
		// Assigned by account ID: its name is the ticket's to say.
		again, err := updater.Ticket(ctx, key)
		if err != nil {
			return WorkflowExecutionResult{}, fmt.Errorf("assigned %s, then reading it back: %w; retrying the step changes nothing more", key, err)
		}
		ticket.Assignee.Name = again.Assignee.Name
	}
	return ticketUpdated(ticket, changed, warnings), nil
}

func ticketUpdated(t TicketState, changed bool, warnings []string) WorkflowExecutionResult {
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{
		"key": t.Key, "url": t.URL, "status": t.Status, "assignee": t.Assignee.Name, "changed": changed, "warnings": warnings}}
}
