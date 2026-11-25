package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/cpave3/legato/internal/engine/burnlite"
	"github.com/cpave3/legato/internal/engine/store"
)

// The ticket steps (LEG-640): a workflow raises a ticket in Jira or
// Burnlite, or comments on one, with Legato's own settings.
const (
	TicketCreateStepType  = "ticket.create"
	TicketCommentStepType = "ticket.comment"
)

// maxTicketSummary is Jira's limit on a summary; Burnlite's is the same.
const maxTicketSummary = 255

var (
	jiraOnlyTicketFields     = []string{"type", "priority", "fields"}
	burnliteOnlyTicketFields = []string{"kind", "size", "uncertainty", "milestone"}
	// jiraFieldsLegatoSets are the fields ticket.create sets itself, which
	// fields may not name.
	jiraFieldsLegatoSets = []string{"project", "issuetype", "summary", "description", "labels", "priority"}
	burnliteSizes        = []string{"Small", "Medium", "Large", "X-Large"}
	burnliteUncertainty  = []string{"Low", "Medium", "High", "Extreme"}
)

// resolveTicketWriter is the writer a step names, or the only one set up.
func resolveTicketWriter(ctx context.Context, writers []TicketWriter, name string) (TicketWriter, error) {
	if name != "" {
		if name != "jira" && name != BurnliteIntegration {
			return nil, fmt.Errorf("provider must be jira or burnlite, not %q", name)
		}
		for _, w := range writers {
			if w.Provider() == name {
				if err := w.Configured(ctx); err != nil {
					return nil, err
				}
				return w, nil
			}
		}
		return nil, fmt.Errorf("%s is not available here", ticketProviderLabel(name))
	}
	var ready []TicketWriter
	for _, w := range writers {
		if w.Configured(ctx) == nil {
			ready = append(ready, w)
		}
	}
	switch len(ready) {
	case 1:
		return ready[0], nil
	case 0:
		return nil, errors.New("no ticket provider is set up: set up Jira (jira.base_url, jira.email, jira.api_token) or connect Legato to Burnlite")
	}
	return nil, errors.New("both Jira and Burnlite are set up: set provider to jira or burnlite")
}

func ticketProviderLabel(provider string) string {
	if provider == BurnliteIntegration {
		return "Burnlite"
	}
	return "Jira"
}

func ticketNoun(provider string) string {
	if provider == BurnliteIntegration {
		return "a Burnlite task"
	}
	return "a Jira issue"
}

// ticketWriteError words a provider's error: a read-only Burnlite
// connection is told to connect again.
func ticketWriteError(doing string, err error) error {
	if errors.Is(err, burnlite.ErrInsufficientScope) {
		return fmt.Errorf("%s: Legato's Burnlite connection may only read; connect Legato to Burnlite again (Settings › Integrations, or legato integration connect burnlite) to let it write tickets: %w", doing, err)
	}
	return fmt.Errorf("%s: %w", doing, err)
}

func trimmedString(with map[string]any, key string) (string, error) {
	s, err := workflowString(with, key, false)
	return strings.TrimSpace(s), err
}

func given(with map[string]any, key string) bool {
	v, ok := with[key]
	if !ok || v == nil {
		return false
	}
	if s, isString := v.(string); isString {
		return strings.TrimSpace(s) != ""
	}
	return true
}

func oneOf(key, value string, allowed []string) error {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return fmt.Errorf("%s must be one of %s, not %q", key, strings.Join(allowed, ", "), value)
}

// TicketCreateExecutor is ticket.create.
type TicketCreateExecutor struct {
	store   *store.Store
	writers []TicketWriter
}

func NewTicketCreateExecutor(db *store.Store, writers ...TicketWriter) *TicketCreateExecutor {
	return &TicketCreateExecutor{store: db, writers: writers}
}

func (e *TicketCreateExecutor) Type() string { return TicketCreateStepType }

func (e *TicketCreateExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

func ticketCreated(t CreatedTicket, created bool) WorkflowExecutionResult {
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{
		"key": t.Key, "id": t.ID, "url": t.URL, "provider": t.Provider, "created": created}}
}

func (e *TicketCreateExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	record, err := e.store.GetWorkflowStepEffect(ctx, step.RunID, step.StepID, TicketCreateStepType)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if record != nil && record.State == store.WorkflowEffectDone {
		return ticketCreated(CreatedTicket{Provider: record.Provider, Key: record.Key, ID: record.RemoteID, URL: record.URL}, false), nil
	}
	draft, providerName, err := readTicketDraft(step.With)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	writer, err := resolveTicketWriter(ctx, e.writers, providerName)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if err := checkTicketDraft(writer.Provider(), step.With, &draft); err != nil {
		return WorkflowExecutionResult{}, err
	}
	doing := fmt.Sprintf("creating %s in %s", ticketNoun(writer.Provider()), draft.Project)

	effect, begun, err := e.store.BeginWorkflowStepEffect(ctx, store.WorkflowStepEffect{RunID: step.RunID, Path: step.StepID,
		Effect: TicketCreateStepType, AttemptID: step.AttemptID, Provider: writer.Provider(), Target: draft.Project, Summary: draft.Summary})
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if !begun {
		// An earlier attempt began and never learned whether it created
		// the ticket: look for it where that attempt made it.
		found, err := e.findEarlier(ctx, effect)
		if err != nil {
			return WorkflowExecutionResult{}, err
		}
		if found != nil {
			if err := e.store.FinishWorkflowStepEffect(ctx, step.RunID, step.StepID, TicketCreateStepType, found.Key, found.ID, found.URL); err != nil {
				return WorkflowExecutionResult{}, err
			}
			return ticketCreated(*found, false), nil
		}
	}

	ticket, err := writer.Create(ctx, draft)
	if err != nil {
		if ticketRefused(err) {
			if dropErr := e.store.DropWorkflowStepEffect(ctx, step.RunID, step.StepID, TicketCreateStepType); dropErr != nil {
				return WorkflowExecutionResult{}, errors.Join(ticketWriteError(doing, err), dropErr)
			}
			return WorkflowExecutionResult{}, ticketWriteError(doing, err)
		}
		return WorkflowExecutionResult{}, fmt.Errorf("%w; it may have been created: retrying the step looks for it before creating another", ticketWriteError(doing, err))
	}
	if err := e.store.FinishWorkflowStepEffect(ctx, step.RunID, step.StepID, TicketCreateStepType, ticket.Key, ticket.ID, ticket.URL); err != nil {
		return WorkflowExecutionResult{}, fmt.Errorf("created %s but could not record it: %w", ticket.Key, err)
	}
	return ticketCreated(ticket, true), nil
}

// findEarlier is the ticket an earlier attempt created, nil when it made
// none, or an error when it cannot tell.
func (e *TicketCreateExecutor) findEarlier(ctx context.Context, effect store.WorkflowStepEffect) (*CreatedTicket, error) {
	var writer TicketWriter
	for _, w := range e.writers {
		if w.Provider() == effect.Provider {
			writer = w
		}
	}
	if writer == nil {
		return nil, fmt.Errorf("an earlier attempt may have created a ticket in %s, which is not available here to look in", ticketProviderLabel(effect.Provider))
	}
	found, err := writer.FindCreated(ctx, effect.Target, effect.Summary, effect.StartedAt)
	if err != nil {
		return nil, ticketWriteError(fmt.Sprintf("an earlier attempt may have created %s in %s; looking for it", ticketNoun(effect.Provider), effect.Target), err)
	}
	switch len(found) {
	case 0:
		return nil, nil
	case 1:
		return &found[0], nil
	}
	keys := make([]string, len(found))
	for i, t := range found {
		keys[i] = t.Key
	}
	return nil, fmt.Errorf("an earlier attempt may have created %s in %s, and %s each match %q: the step cannot tell which one it created, and a retry will not either; cancel the run and carry on by hand, or start a new run",
		ticketNoun(effect.Provider), effect.Target, strings.Join(keys, ", "), effect.Summary)
}

// readTicketDraft reads what every provider takes, and the provider named.
func readTicketDraft(with map[string]any) (TicketDraft, string, error) {
	var d TicketDraft
	provider, err := trimmedString(with, "provider")
	if err != nil {
		return d, "", err
	}
	if d.Project, err = trimmedString(with, "project"); err != nil {
		return d, "", err
	}
	if d.Project == "" {
		return d, "", errors.New("project is required")
	}
	summary, err := trimmedString(with, "summary")
	if err != nil {
		return d, "", err
	}
	d.Summary = strings.Join(strings.Fields(summary), " ")
	if d.Summary == "" {
		return d, "", errors.New("summary is required")
	}
	if n := len([]rune(d.Summary)); n > maxTicketSummary {
		return d, "", fmt.Errorf("summary is %d characters; a ticket's summary is at most %d", n, maxTicketSummary)
	}
	if d.Description, err = workflowString(with, "description", false); err != nil {
		return d, "", err
	}
	if d.Labels, err = ticketLabels(with["labels"]); err != nil {
		return d, "", err
	}
	return d, provider, nil
}

func ticketLabels(value any) ([]string, error) {
	var labels []string
	switch v := value.(type) {
	case nil:
		return nil, nil
	case []string:
		labels = v
	case []any:
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, errors.New("labels must be a list of strings")
			}
			labels = append(labels, s)
		}
	default:
		return nil, errors.New("labels must be a list of strings")
	}
	var out []string
	for _, l := range labels {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out, nil
}

// checkTicketDraft reads and checks what only one provider takes, and
// refuses what the other takes.
func checkTicketDraft(provider string, with map[string]any, d *TicketDraft) error {
	label := ticketProviderLabel(provider)
	if provider == BurnliteIntegration {
		for _, key := range jiraOnlyTicketFields {
			if given(with, key) {
				return fmt.Errorf("%s is for Jira; Burnlite takes no %s", key, key)
			}
		}
		var err error
		for key, dst := range map[string]*string{"kind": &d.Kind, "size": &d.Size, "uncertainty": &d.Uncertainty, "milestone": &d.Milestone} {
			if *dst, err = trimmedString(with, key); err != nil {
				return err
			}
		}
		if d.Kind == "" {
			d.Kind = "work"
		}
		if err := oneOf("kind", d.Kind, []string{"work", "meta"}); err != nil {
			return err
		}
		if d.Kind == "meta" {
			if d.Size != "" || d.Uncertainty != "" {
				return errors.New("a Meta task takes no size or uncertainty")
			}
			return nil
		}
		if d.Size == "" || d.Uncertainty == "" {
			return errors.New("a Work task needs a size and an uncertainty")
		}
		if err := oneOf("size", d.Size, burnliteSizes); err != nil {
			return err
		}
		return oneOf("uncertainty", d.Uncertainty, burnliteUncertainty)
	}

	for _, key := range burnliteOnlyTicketFields {
		if given(with, key) {
			return fmt.Errorf("%s is for Burnlite; %s takes no %s", key, label, key)
		}
	}
	var err error
	if d.Type, err = trimmedString(with, "type"); err != nil {
		return err
	}
	if d.Type == "" {
		d.Type = "Bug"
	}
	if d.Priority, err = trimmedString(with, "priority"); err != nil {
		return err
	}
	for _, l := range d.Labels {
		if strings.IndexFunc(l, unicode.IsSpace) >= 0 {
			return fmt.Errorf("Jira labels cannot hold spaces: %q", l)
		}
	}
	switch fields := with["fields"].(type) {
	case nil:
	case map[string]any:
		var clash []string
		for _, name := range jiraFieldsLegatoSets {
			if _, ok := fields[name]; ok {
				clash = append(clash, name)
			}
		}
		if len(clash) > 0 {
			sort.Strings(clash)
			return fmt.Errorf("fields may not set %s: give it as its own field of the step", strings.Join(clash, ", "))
		}
		d.Fields = fields
	default:
		return errors.New("fields must be an object of Jira field names and values")
	}
	return nil
}

// TicketCommentExecutor is ticket.comment.
type TicketCommentExecutor struct {
	store   *store.Store
	writers []TicketWriter
}

func NewTicketCommentExecutor(db *store.Store, writers ...TicketWriter) *TicketCommentExecutor {
	return &TicketCommentExecutor{store: db, writers: writers}
}

func (e *TicketCommentExecutor) Type() string { return TicketCommentStepType }

func (e *TicketCommentExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

func ticketCommented(c TicketComment) WorkflowExecutionResult {
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"url": c.URL, "id": c.ID, "key": c.Key}}
}

func (e *TicketCommentExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	record, err := e.store.GetWorkflowStepEffect(ctx, step.RunID, step.StepID, TicketCommentStepType)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if record != nil && record.State == store.WorkflowEffectDone {
		return ticketCommented(TicketComment{Provider: record.Provider, Key: record.Key, ID: record.RemoteID, URL: record.URL}), nil
	}
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
	body, err := workflowString(step.With, "body", false)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if strings.TrimSpace(body) == "" {
		return WorkflowExecutionResult{}, errors.New("body is required")
	}
	writer, err := resolveTicketWriter(ctx, e.writers, provider)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}

	effect, begun, err := e.store.BeginWorkflowStepEffect(ctx, store.WorkflowStepEffect{RunID: step.RunID, Path: step.StepID,
		Effect: TicketCommentStepType, AttemptID: step.AttemptID, Provider: writer.Provider(), Target: key})
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if !begun {
		// A comment is not searched for: the operator checks, and the next
		// retry adds it.
		if err := e.store.DropWorkflowStepEffect(ctx, step.RunID, step.StepID, TicketCommentStepType); err != nil {
			return WorkflowExecutionResult{}, err
		}
		return WorkflowExecutionResult{}, fmt.Errorf("an earlier attempt may have added this comment to %s and did not learn whether it did: check %s, and if the comment is not there, retry the step to add it", effect.Target, effect.Target)
	}

	doing := "commenting on " + key
	comment, err := writer.Comment(ctx, key, body)
	if err != nil {
		if ticketRefused(err) {
			if dropErr := e.store.DropWorkflowStepEffect(ctx, step.RunID, step.StepID, TicketCommentStepType); dropErr != nil {
				return WorkflowExecutionResult{}, errors.Join(ticketWriteError(doing, err), dropErr)
			}
			return WorkflowExecutionResult{}, ticketWriteError(doing, err)
		}
		return WorkflowExecutionResult{}, fmt.Errorf("%w; the comment may have been added: check %s before retrying", ticketWriteError(doing, err), key)
	}
	if err := e.store.FinishWorkflowStepEffect(ctx, step.RunID, step.StepID, TicketCommentStepType, comment.Key, comment.ID, comment.URL); err != nil {
		return WorkflowExecutionResult{}, fmt.Errorf("commented on %s but could not record it: %w", key, err)
	}
	return ticketCommented(comment), nil
}
