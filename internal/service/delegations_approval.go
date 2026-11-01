package service

import (
	"context"
	"fmt"
	"log"

	"github.com/cpave3/legato/internal/engine/store"
)

// ApprovalSourceDelegation is the source of approvals that hold a delegation
// until the person allows it (integrations.argus.delivery: ask).
const ApprovalSourceDelegation = "delegation"

// RegisterResolver lets the person's decisions on delegation approvals reach
// the delegation service.
func (s *DelegationService) RegisterResolver() {
	if s.opts.Approvals != nil {
		s.opts.Approvals.RegisterResolver(ApprovalSourceDelegation, s)
	}
}

func (s *DelegationService) ask(ctx context.Context, d *store.Delegation) error {
	brief := d.Brief
	if len(brief) > 2000 {
		brief = brief[:2000] + "…"
	}
	where := "a new task"
	if d.TaskID != "" {
		where = "task " + d.TaskID
	}
	approval, _, err := s.opts.Approvals.Submit(ctx, ApprovalInput{
		Source: ApprovalSourceDelegation, SourceKey: d.ID, TaskID: d.TaskID,
		Title:   fmt.Sprintf("%s: %s on case %s", integrationTitle(d.Integration), modeLabel(d.Mode), d.CaseID),
		Summary: fmt.Sprintf("Deliver to %s by %s", where, d.Route),
		Details: map[string]string{"case": d.CaseID, "mode": d.Mode, "task": d.TaskID, "route": d.Route, "brief": brief},
		Options: []ApprovalOption{{ID: "deliver", Label: "Deliver", Kind: ApprovalAllow}, {ID: "deny", Label: "Refuse", Kind: ApprovalDeny}},
	})
	if err != nil {
		return err
	}
	return s.db.SetDelegationAwaitingApproval(ctx, d.ID, approval.ID)
}

// Resolve delivers a delegation the person allowed, and fails one they
// refused.
func (s *DelegationService) Resolve(ctx context.Context, approval Approval, decision ApprovalDecision) error {
	d, err := s.db.GetDelegation(ctx, approval.SourceKey)
	if err != nil {
		return err
	}
	if d.State != store.DelegationAwaitingApproval {
		return nil
	}
	if decision.Decision != ApprovalAllow {
		if err := s.db.SetDelegationFailed(ctx, d.ID, "denied", "the person refused this delegation"); err != nil {
			return err
		}
		s.changed(d.TaskID)
		return nil
	}
	if err := s.db.SetDelegationPending(ctx, d.ID); err != nil {
		return err
	}
	id := d.ID
	s.opts.Run(func() {
		s.deliver(context.Background(), id)
		if failed, err := s.db.GetDelegation(context.Background(), id); err == nil && failed.State == store.DelegationFailed {
			log.Printf("delegation %s: allowed, then failed: %s", id, failed.ErrorMessage)
		}
	})
	return nil
}
