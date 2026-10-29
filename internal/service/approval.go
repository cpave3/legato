package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// What a decision does. A decision to allow takes one of the approval's allow
// options; answer gives a question its answers; dismiss closes the request
// without either.
const (
	ApprovalAllow   = "allow"
	ApprovalDeny    = "deny"
	ApprovalAnswer  = "answer"
	ApprovalDismiss = "dismiss"
)

var (
	// ErrApprovalDecided: the approval is no longer pending. Another decision
	// came first, or the request was withdrawn or expired.
	ErrApprovalDecided = errors.New("approval is no longer pending")
	// ErrApprovalInvalid: the decision does not fit the approval.
	ErrApprovalInvalid = errors.New("invalid decision")
)

// ApprovalOption is one way to decide an approval.
type ApprovalOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Kind        string `json:"kind"` // allow or deny
	Description string `json:"description,omitempty"`
}

// ApprovalInput is a request a source submits.
type ApprovalInput struct {
	Source      string
	SourceKey   string
	TaskID      string
	ExecutionID string
	Title       string
	Summary     string
	Details     any
	Options     []ApprovalOption
	// Blocking says the requester waits for the decision.
	Blocking bool
}

// ApprovalDecision is what the person decided, and where.
type ApprovalDecision struct {
	Decision string              `json:"decision"`
	Option   string              `json:"option,omitempty"`
	Feedback string              `json:"feedback,omitempty"`
	Answers  map[string][]string `json:"answers,omitempty"`
	By       string              `json:"by,omitempty"`
}

// Approval is an approval request as callers see it.
type Approval struct {
	store.Approval
	Details  json.RawMessage   `json:"details"`
	Options  []ApprovalOption  `json:"options"`
	Decision *ApprovalDecision `json:"decision,omitempty"`
}

// ApprovalResolver applies decisions to the source that raised a request.
type ApprovalResolver interface {
	Resolve(ctx context.Context, approval Approval, decision ApprovalDecision) error
}

// ApprovalValidator is an optional check a resolver makes before a decision
// is recorded, so a decision the source would refuse leaves the request open.
type ApprovalValidator interface {
	Validate(approval Approval, decision ApprovalDecision) error
}

// ApprovalServiceOptions configures an ApprovalService.
type ApprovalServiceOptions struct {
	Bus *events.Bus
	// Broadcast tells other Legato processes that an approval changed.
	Broadcast func(approvalID, taskID, kind string)
	// Created is called once for each approval this process created.
	Created func(Approval)
	// PollInterval is how often Wait reads an approval. Default 250ms.
	PollInterval time.Duration
}

// ApprovalService keeps the requests that need the person.
type ApprovalService struct {
	store *store.Store
	opts  ApprovalServiceOptions

	mu        sync.RWMutex
	resolvers map[string]ApprovalResolver
}

func NewApprovalService(db *store.Store, opts ApprovalServiceOptions) *ApprovalService {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 250 * time.Millisecond
	}
	return &ApprovalService{store: db, opts: opts, resolvers: map[string]ApprovalResolver{}}
}

// RegisterResolver sets the resolver for one source.
func (s *ApprovalService) RegisterResolver(source string, resolver ApprovalResolver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolvers[source] = resolver
}

func (s *ApprovalService) resolver(source string) ApprovalResolver {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.resolvers[source]
}

// Submit stores a request, or returns the one already stored for the same
// source and source key. created reports whether this call stored it.
func (s *ApprovalService) Submit(ctx context.Context, in ApprovalInput) (*Approval, bool, error) {
	if in.Source == "" || in.SourceKey == "" || in.Title == "" {
		return nil, false, fmt.Errorf("%w: an approval needs a source, a source key and a title", ErrApprovalInvalid)
	}
	details, err := json.Marshal(in.Details)
	if err != nil || in.Details == nil {
		details = []byte("{}")
	}
	options := in.Options
	if options == nil {
		options = []ApprovalOption{}
	}
	optionsJSON, err := json.Marshal(options)
	if err != nil {
		return nil, false, err
	}
	row, created, err := s.store.InsertApproval(ctx, store.Approval{
		ID: newApprovalID(), Source: in.Source, SourceKey: in.SourceKey,
		TaskID: in.TaskID, ExecutionID: in.ExecutionID,
		Title: in.Title, Summary: in.Summary,
		DetailsJSON: string(details), OptionsJSON: string(optionsJSON), Blocking: in.Blocking,
	})
	if err != nil {
		return nil, false, err
	}
	approval := decodeApproval(*row)
	if created {
		s.changed(approval, "created")
		if s.opts.Created != nil {
			s.opts.Created(approval)
		}
	}
	return &approval, created, nil
}

func (s *ApprovalService) Get(ctx context.Context, id string) (*Approval, error) {
	row, err := s.store.GetApproval(ctx, id)
	if err != nil {
		return nil, err
	}
	approval := decodeApproval(*row)
	return &approval, nil
}

// GetBySource returns the approval of one source's request.
func (s *ApprovalService) GetBySource(ctx context.Context, source, sourceKey string) (*Approval, error) {
	row, err := s.store.GetApprovalBySource(ctx, source, sourceKey)
	if err != nil {
		return nil, err
	}
	approval := decodeApproval(*row)
	return &approval, nil
}

// List returns approvals, oldest first.
func (s *ApprovalService) List(ctx context.Context, filter store.ApprovalFilter) ([]Approval, error) {
	rows, err := s.store.ListApprovals(ctx, filter)
	if err != nil {
		return nil, err
	}
	out := make([]Approval, len(rows))
	for i, row := range rows {
		out[i] = decodeApproval(row)
	}
	return out, nil
}

// CountPending is how many approvals wait for the person.
func (s *ApprovalService) CountPending(ctx context.Context) (int, error) {
	return s.store.CountApprovals(ctx, store.ApprovalPending)
}

// Decide records a decision and applies it to the request's source. Only the
// first decision on an approval counts; later ones get ErrApprovalDecided.
func (s *ApprovalService) Decide(ctx context.Context, id string, decision ApprovalDecision) (*Approval, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Status != store.ApprovalPending {
		return nil, ErrApprovalDecided
	}
	status, err := decisionStatus(current, &decision)
	if err != nil {
		return nil, err
	}
	resolver := s.resolver(current.Source)
	if resolver == nil {
		return nil, fmt.Errorf("no resolver for approvals from %q", current.Source)
	}
	if validator, ok := resolver.(ApprovalValidator); ok {
		if err := validator.Validate(*current, decision); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrApprovalInvalid, err)
		}
	}
	decisionJSON, _ := json.Marshal(decision)
	row, err := s.store.CloseApproval(ctx, id, store.ApprovalClose{Status: status, DecisionJSON: string(decisionJSON), DecidedBy: decision.By})
	if errors.Is(err, store.ErrConflict) {
		return nil, ErrApprovalDecided
	}
	if err != nil {
		return nil, err
	}
	decided := decodeApproval(*row)
	if err := resolver.Resolve(ctx, decided, decision); err != nil {
		if failedRow, markErr := s.store.MarkApprovalFailed(ctx, id, err.Error()); markErr == nil {
			s.changed(decodeApproval(*failedRow), store.ApprovalFailed)
		}
		return nil, err
	}
	s.changed(decided, decided.Status)
	return &decided, nil
}

// Dismiss closes a request without allowing or denying it in the person's
// words; the source decides what a dismissal means for it.
func (s *ApprovalService) Dismiss(ctx context.Context, id, note, by string) (*Approval, error) {
	return s.Decide(ctx, id, ApprovalDecision{Decision: ApprovalDismiss, Feedback: note, By: by})
}

// Withdraw closes a pending request that was settled somewhere else. An
// unknown or closed request is left as it is.
func (s *ApprovalService) Withdraw(ctx context.Context, source, sourceKey, reason string) error {
	row, err := s.store.GetApprovalBySource(ctx, source, sourceKey)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.closeQuietly(ctx, row.ID, store.ApprovalWithdrawn, reason)
}

// WithdrawExecution withdraws an execution's pending requests from one source,
// except those whose source keys are in keep.
func (s *ApprovalService) WithdrawExecution(ctx context.Context, source, executionID string, keep map[string]bool, reason string) error {
	rows, err := s.store.ListApprovals(ctx, store.ApprovalFilter{Status: store.ApprovalPending, ExecutionID: executionID, Source: source})
	if err != nil {
		return err
	}
	for _, row := range rows {
		if keep[row.SourceKey] {
			continue
		}
		if err := s.closeQuietly(ctx, row.ID, store.ApprovalWithdrawn, reason); err != nil {
			return err
		}
	}
	return nil
}

// ExpireTask expires every pending request of a task, as when its agent ends.
func (s *ApprovalService) ExpireTask(ctx context.Context, taskID string) error {
	rows, err := s.store.ListApprovals(ctx, store.ApprovalFilter{Status: store.ApprovalPending, TaskID: taskID})
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := s.closeQuietly(ctx, row.ID, store.ApprovalExpired, "the agent ended"); err != nil {
			return err
		}
	}
	return nil
}

// Wait reads an approval until it is no longer pending or timeout passes,
// and returns it as it then is.
func (s *ApprovalService) Wait(ctx context.Context, id string, timeout time.Duration) (*Approval, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(s.opts.PollInterval)
	defer tick.Stop()
	for {
		approval, err := s.Get(ctx, id)
		if err != nil || approval.Status != store.ApprovalPending {
			return approval, err
		}
		select {
		case <-ctx.Done():
			return approval, ctx.Err()
		case <-deadline.C:
			return approval, nil
		case <-tick.C:
		}
	}
}

func (s *ApprovalService) closeQuietly(ctx context.Context, id, status, reason string) error {
	row, err := s.store.CloseApproval(ctx, id, store.ApprovalClose{Status: status, Error: reason})
	if errors.Is(err, store.ErrConflict) {
		return nil
	}
	if err != nil {
		return err
	}
	s.changed(decodeApproval(*row), status)
	return nil
}

func (s *ApprovalService) changed(approval Approval, kind string) {
	if s.opts.Bus != nil {
		s.opts.Bus.Publish(events.Event{Type: events.EventApprovalChanged, Payload: events.ApprovalChangedPayload{
			ApprovalID: approval.ID, TaskID: approval.TaskID, Source: approval.Source, Kind: kind,
		}, At: time.Now()})
	}
	if s.opts.Broadcast != nil {
		s.opts.Broadcast(approval.ID, approval.TaskID, kind)
	}
}

// decisionStatus checks a decision against the approval and gives the status
// it closes the approval with. An allow decision without an option takes the
// first allow option.
func decisionStatus(approval *Approval, decision *ApprovalDecision) (string, error) {
	switch decision.Decision {
	case ApprovalAllow:
		if decision.Option == "" {
			for _, option := range approval.Options {
				if option.Kind == ApprovalAllow {
					decision.Option = option.ID
					break
				}
			}
		}
		for _, option := range approval.Options {
			if option.ID == decision.Option && option.Kind == ApprovalAllow {
				return store.ApprovalApproved, nil
			}
		}
		return "", fmt.Errorf("%w: %q is not a way to allow this request", ErrApprovalInvalid, decision.Option)
	case ApprovalDeny:
		decision.Option = ""
		return store.ApprovalDenied, nil
	case ApprovalAnswer:
		if len(decision.Answers) == 0 {
			return "", fmt.Errorf("%w: an answer needs answers", ErrApprovalInvalid)
		}
		return store.ApprovalAnswered, nil
	case ApprovalDismiss:
		return store.ApprovalDismissed, nil
	}
	return "", fmt.Errorf("%w: unknown decision %q", ErrApprovalInvalid, decision.Decision)
}

func decodeApproval(row store.Approval) Approval {
	approval := Approval{Approval: row, Details: json.RawMessage(row.DetailsJSON), Options: []ApprovalOption{}}
	if len(approval.Details) == 0 {
		approval.Details = json.RawMessage("{}")
	}
	_ = json.Unmarshal([]byte(row.OptionsJSON), &approval.Options)
	if row.DecisionJSON != "" {
		var decision ApprovalDecision
		if json.Unmarshal([]byte(row.DecisionJSON), &decision) == nil {
			approval.Decision = &decision
		}
	}
	return approval
}

func newApprovalID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "ap-" + hex.EncodeToString(b)
}
