package service

import (
	"context"
	"errors"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// fixedVerifier accepts every signature, as a device whose check said
// verification.
type fixedVerifier struct{ verification string }

func (v fixedVerifier) VerifyDecision(_ context.Context, _ Approval, d ApprovalDecision) (SignedBy, error) {
	return SignedBy{Device: d.Signature.Device, Verification: v.verification}, nil
}

func verifiedRequest(key string) ApprovalInput {
	return ApprovalInput{Source: "test", SourceKey: key, TaskID: "t", Title: "Run as root",
		Options: []ApprovalOption{{ID: "allow", Label: "Allow", Kind: ApprovalAllow}, {ID: "deny", Label: "Deny", Kind: ApprovalDeny}},
		Signed: true, Verified: true}
}

func TestAnApprovalThatNeedsVerificationRefusesABareTouch(t *testing.T) {
	approvals, _, _ := newApprovalFixture(t)
	approvals.RegisterResolver("test", acceptingResolver{})
	ctx := context.Background()
	a, _, err := approvals.Submit(ctx, verifiedRequest("k1"))
	if err != nil {
		t.Fatal(err)
	}
	if !a.Verified || !a.Signed {
		t.Fatalf("approval = %+v", a.Approval)
	}
	approvals.SetSignatureVerifier(fixedVerifier{VerificationPresence})
	sig := &ApprovalSignature{Device: "key-1", Challenge: "c", Value: "v"}
	if _, err := approvals.Decide(ctx, a.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "allow", Signature: sig}); !errors.Is(err, ErrApprovalNeedsVerification) {
		t.Fatalf("a presence-only allow: %v", err)
	}
	approvals.SetSignatureVerifier(fixedVerifier{VerificationVerified})
	decided, err := approvals.Decide(ctx, a.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "allow", Signature: sig})
	if err != nil || decided.Status != store.ApprovalApproved || decided.Decision.Verification != VerificationVerified {
		t.Fatalf("a verified allow: %+v, %v", decided, err)
	}
}

func TestADenialNeedsNoVerification(t *testing.T) {
	approvals, _, _ := newApprovalFixture(t)
	approvals.RegisterResolver("test", acceptingResolver{})
	a, _, _ := approvals.Submit(context.Background(), verifiedRequest("k1"))
	if _, err := approvals.Decide(context.Background(), a.ID, ApprovalDecision{Decision: ApprovalDeny, By: "web"}); err != nil {
		t.Fatalf("an unsigned denial: %v", err)
	}
}
