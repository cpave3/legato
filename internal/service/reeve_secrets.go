package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// ApprovalSourceReeveSecret is the Reeve's request for a secret: the person
// pastes the value into a form, and it goes to a secret store, never to
// the Reeve and never into the approval's record.
const ApprovalSourceReeveSecret = "reeve_secret"

var secretRequestName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// ErrSecretRequestClosed: the request was answered, dismissed or expired.
var ErrSecretRequestClosed = errors.New("this request for a secret is no longer open")

// SecretSink keeps a secret by name and returns the reference that names
// it (legato://name).
type SecretSink interface {
	Put(ctx context.Context, name, value string) (ref string, err error)
}

// SecretRequestDetails is what a request's approval holds: no value.
type SecretRequestDetails struct {
	Name    string `json:"name"`
	Purpose string `json:"purpose"`
	Ref     string `json:"ref,omitempty"`
}

// ReeveSecrets asks the person for secrets the Reeve needs.
type ReeveSecrets struct {
	Approvals *ApprovalService
	Sink      SecretSink
	Wait      time.Duration
}

// Register makes the approval service accept answers to requests (the
// value goes through Fulfil, never through a decision).
func (r *ReeveSecrets) Register() {
	r.Approvals.RegisterResolver(ApprovalSourceReeveSecret, secretRequestResolver{})
}

type secretRequestResolver struct{}

func (secretRequestResolver) Resolve(context.Context, Approval, ApprovalDecision) error { return nil }

// secretStoredBy marks the decision Fulfil makes: a request is answered
// only by storing a value, never by a plain allow.
const secretStoredBy = "legato:secret-store"

// Validate refuses an allow that did not come from Fulfil.
func (secretRequestResolver) Validate(_ Approval, decision ApprovalDecision) error {
	if decision.Decision == ApprovalAllow && decision.By != secretStoredBy {
		return errors.New("a request for a secret is answered by pasting the value in the form")
	}
	return nil
}

// SecretOutcome is what request_secret answers.
type SecretOutcome struct {
	State      string `json:"state"` // stored, declined, pending
	Ref        string `json:"ref,omitempty"`
	ApprovalID string `json:"approval_id"`
}

// Request asks the person to paste a secret for a Reeve session and waits
// for it (60 s): the reference to use it by, never its value.
func (r *ReeveSecrets) Request(ctx context.Context, taskID, name, purpose string) (SecretOutcome, error) {
	name = strings.TrimSpace(name)
	if !secretRequestName.MatchString(name) {
		return SecretOutcome{}, fmt.Errorf("%q is not a secret name: letters, digits, '.', '_' and '-'", name)
	}
	approval, _, err := r.Approvals.Submit(ctx, ApprovalInput{
		Source: ApprovalSourceReeveSecret, SourceKey: taskID + ":" + name, TaskID: taskID,
		Title:   r.Approvals.ReeveName(ctx).Start() + " asks for a secret: " + name,
		Summary: purpose, Details: SecretRequestDetails{Name: name, Purpose: strings.TrimSpace(purpose)},
		Options: []ApprovalOption{{ID: "stored", Label: "Store the secret", Kind: ApprovalAllow}, {ID: "deny", Label: "Decline", Kind: ApprovalDeny}},
	})
	if err != nil {
		return SecretOutcome{}, err
	}
	out := SecretOutcome{State: "pending", ApprovalID: approval.ID}
	wait := r.Wait
	if wait == 0 {
		wait = 60 * time.Second
	}
	decided, err := r.Approvals.Wait(ctx, approval.ID, wait)
	if err != nil || decided == nil {
		return out, nil
	}
	switch decided.Status {
	case store.ApprovalApproved:
		var details SecretRequestDetails
		_ = json.Unmarshal(decided.Details, &details)
		out.State, out.Ref = "stored", "legato://"+name
		if decided.Decision != nil && decided.Decision.Feedback != "" {
			out.Ref = decided.Decision.Feedback
		}
	case store.ApprovalDenied, store.ApprovalDismissed, store.ApprovalExpired:
		out.State = "declined"
	}
	return out, nil
}

// Fulfil stores the value the person pasted for a request, and closes it.
// The value is kept only in the secret store.
func (r *ReeveSecrets) Fulfil(ctx context.Context, approvalID, value string) (string, error) {
	approval, err := r.Approvals.Get(ctx, approvalID)
	if err != nil {
		return "", err
	}
	if approval.Source != ApprovalSourceReeveSecret || approval.Status != store.ApprovalPending {
		return "", ErrSecretRequestClosed
	}
	if strings.TrimSpace(value) == "" {
		return "", errors.New("paste the secret's value")
	}
	var details SecretRequestDetails
	if err := json.Unmarshal(approval.Details, &details); err != nil {
		return "", err
	}
	ref, err := r.Sink.Put(ctx, details.Name, value)
	if err != nil {
		return "", err
	}
	// The decision names where the secret went; the value is not in it.
	if _, err := r.Approvals.Decide(ctx, approvalID, ApprovalDecision{Decision: ApprovalAllow, Option: "stored", Feedback: ref, By: secretStoredBy}); err != nil {
		return "", err
	}
	return ref, nil
}
