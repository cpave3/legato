package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

type recordingResolver struct {
	calls []ApprovalDecision
	err   error
}

func (r *recordingResolver) Resolve(_ context.Context, _ Approval, d ApprovalDecision) error {
	r.calls = append(r.calls, d)
	return r.err
}

func newApprovalFixture(t *testing.T) (*ApprovalService, *store.Store, *events.Bus) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "approvals.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	bus := events.New()
	return NewApprovalService(db, ApprovalServiceOptions{Bus: bus}), db, bus
}

func commandRequest(key string) ApprovalInput {
	return ApprovalInput{
		Source: "test", SourceKey: key, TaskID: "task-1", ExecutionID: "ex-1",
		Title: "Run a command", Summary: "go test ./...",
		Details: map[string]string{"command": "go test ./..."},
		Options: []ApprovalOption{{ID: "once", Label: "Allow once", Kind: ApprovalAllow}, {ID: "deny", Label: "Deny", Kind: ApprovalDeny}},
	}
}

func TestSubmitStoresAPendingApprovalOnceAndAnnouncesIt(t *testing.T) {
	svc, _, bus := newApprovalFixture(t)
	ctx := context.Background()
	changes := bus.Subscribe(events.EventApprovalChanged)

	first, created, err := svc.Submit(ctx, commandRequest("k1"))
	if err != nil || !created {
		t.Fatalf("submit: created=%v err=%v", created, err)
	}
	if len(first.ID) != len("ap-00000000") || first.ID[:3] != "ap-" || first.Status != store.ApprovalPending {
		t.Fatalf("approval = %+v", first)
	}
	if len(first.Options) != 2 || first.Options[0].ID != "once" {
		t.Fatalf("options = %+v", first.Options)
	}
	select {
	case event := <-changes:
		payload := event.Payload.(events.ApprovalChangedPayload)
		if payload.ApprovalID != first.ID || payload.Kind != "created" {
			t.Fatalf("payload = %+v", payload)
		}
	case <-time.After(time.Second):
		t.Fatal("no EventApprovalChanged")
	}

	again, created, err := svc.Submit(ctx, commandRequest("k1"))
	if err != nil || created || again.ID != first.ID {
		t.Fatalf("second submit: created=%v id=%s err=%v", created, again.ID, err)
	}
	select {
	case event := <-changes:
		t.Fatalf("second submit announced %+v", event.Payload)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestDecideAppliesTheFirstDecisionOnly(t *testing.T) {
	svc, _, _ := newApprovalFixture(t)
	ctx := context.Background()
	resolver := &recordingResolver{}
	svc.RegisterResolver("test", resolver)
	approval, _, _ := svc.Submit(ctx, commandRequest("k1"))

	decided, err := svc.Decide(ctx, approval.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "once", By: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if decided.Status != store.ApprovalApproved || decided.Decision == nil || decided.Decision.Option != "once" || decided.DecidedBy != "web" {
		t.Fatalf("decided = %+v", decided)
	}
	if _, err := svc.Decide(ctx, approval.ID, ApprovalDecision{Decision: ApprovalDeny}); !errors.Is(err, ErrApprovalDecided) {
		t.Fatalf("second decision err = %v, want ErrApprovalDecided", err)
	}
	if len(resolver.calls) != 1 || resolver.calls[0].Option != "once" {
		t.Fatalf("resolver calls = %+v", resolver.calls)
	}
}

func TestDecideRefusesAnOptionTheApprovalDoesNotHave(t *testing.T) {
	svc, _, _ := newApprovalFixture(t)
	ctx := context.Background()
	svc.RegisterResolver("test", &recordingResolver{})
	approval, _, _ := svc.Submit(ctx, commandRequest("k1"))

	if _, err := svc.Decide(ctx, approval.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "always"}); !errors.Is(err, ErrApprovalInvalid) {
		t.Fatalf("err = %v, want ErrApprovalInvalid", err)
	}
	still, _ := svc.Get(ctx, approval.ID)
	if still.Status != store.ApprovalPending {
		t.Fatalf("status = %s, want pending", still.Status)
	}
}

func TestDecideWithoutAnOptionTakesTheFirstAllowOption(t *testing.T) {
	svc, _, _ := newApprovalFixture(t)
	ctx := context.Background()
	resolver := &recordingResolver{}
	svc.RegisterResolver("test", resolver)
	approval, _, _ := svc.Submit(ctx, commandRequest("k1"))

	if _, err := svc.Decide(ctx, approval.ID, ApprovalDecision{Decision: ApprovalAllow}); err != nil {
		t.Fatal(err)
	}
	if resolver.calls[0].Option != "once" {
		t.Fatalf("option = %q, want once", resolver.calls[0].Option)
	}
}

func TestDecideRecordsAResolverFailure(t *testing.T) {
	svc, _, _ := newApprovalFixture(t)
	ctx := context.Background()
	svc.RegisterResolver("test", &recordingResolver{err: errors.New("resolvePermission: 404 Not Found")})
	approval, _, _ := svc.Submit(ctx, commandRequest("k1"))

	_, err := svc.Decide(ctx, approval.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "once"})
	if err == nil {
		t.Fatal("want the resolver's error")
	}
	failed, _ := svc.Get(ctx, approval.ID)
	if failed.Status != store.ApprovalFailed || failed.Error == "" {
		t.Fatalf("approval = %+v, want failed with the error", failed)
	}
}

func TestDismissWithdrawAndExpireCloseOnlyPendingApprovals(t *testing.T) {
	svc, _, _ := newApprovalFixture(t)
	ctx := context.Background()
	resolver := &recordingResolver{}
	svc.RegisterResolver("test", resolver)
	dismissed, _, _ := svc.Submit(ctx, commandRequest("dismiss"))
	withdrawn, _, _ := svc.Submit(ctx, commandRequest("withdraw"))
	expired, _, _ := svc.Submit(ctx, commandRequest("expire"))
	other := commandRequest("other-task")
	other.TaskID = "task-2"
	untouched, _, _ := svc.Submit(ctx, other)

	if _, err := svc.Dismiss(ctx, dismissed.ID, "not now", "tui"); err != nil {
		t.Fatal(err)
	}
	if len(resolver.calls) != 1 || resolver.calls[0].Decision != ApprovalDismiss || resolver.calls[0].Feedback != "not now" {
		t.Fatalf("dismiss reached the resolver as %+v", resolver.calls)
	}
	if err := svc.Withdraw(ctx, "test", "withdraw", "answered in the terminal"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ExpireTask(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}

	for id, want := range map[string]string{
		dismissed.ID: store.ApprovalDismissed, withdrawn.ID: store.ApprovalWithdrawn,
		expired.ID: store.ApprovalExpired, untouched.ID: store.ApprovalPending,
	} {
		got, _ := svc.Get(ctx, id)
		if got.Status != want {
			t.Errorf("%s: status %s, want %s", id, got.Status, want)
		}
	}
	if len(resolver.calls) != 1 {
		t.Fatalf("withdraw or expire reached the resolver: %+v", resolver.calls)
	}
	if err := svc.Withdraw(ctx, "test", "no-such-request", ""); err != nil {
		t.Fatalf("withdrawing an unknown request: %v", err)
	}
}
