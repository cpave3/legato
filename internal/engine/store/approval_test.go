package store

import (
	"context"
	"errors"
	"testing"
)

func pendingApproval(id, sourceKey string) Approval {
	return Approval{
		ID: id, Source: "agent_permission", SourceKey: sourceKey,
		TaskID: "task-1", ExecutionID: "ex-1",
		Title: "Run a command", Summary: "go test ./...",
		DetailsJSON: `{}`, OptionsJSON: `[{"id":"allow","label":"Allow"}]`,
	}
}

func TestInsertApprovalReturnsTheExistingOneForTheSameSource(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first, created, err := s.InsertApproval(ctx, pendingApproval("ap-00000001", "ex-1/s/request-1"))
	if err != nil || !created {
		t.Fatalf("first insert: created=%v err=%v", created, err)
	}
	if first.Status != ApprovalPending || first.Revision != 1 {
		t.Fatalf("new approval = %+v, want pending at revision 1", first)
	}

	again, created, err := s.InsertApproval(ctx, pendingApproval("ap-00000002", "ex-1/s/request-1"))
	if err != nil {
		t.Fatal(err)
	}
	if created || again.ID != "ap-00000001" {
		t.Fatalf("second insert: created=%v id=%s, want the first approval", created, again.ID)
	}
}

func TestCloseApprovalChangesOnlyAPendingApproval(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, _, err := s.InsertApproval(ctx, pendingApproval("ap-00000001", "k")); err != nil {
		t.Fatal(err)
	}

	decided, err := s.CloseApproval(ctx, "ap-00000001", ApprovalClose{Status: ApprovalApproved, DecisionJSON: `{"option":"allow"}`, DecidedBy: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if decided.Status != ApprovalApproved || decided.DecidedBy != "web" || decided.Revision != 2 || decided.DecidedAt == nil {
		t.Fatalf("decided = %+v", decided)
	}

	if _, err := s.CloseApproval(ctx, "ap-00000001", ApprovalClose{Status: ApprovalDenied}); !errors.Is(err, ErrConflict) {
		t.Fatalf("second close err = %v, want ErrConflict", err)
	}
	if _, err := s.CloseApproval(ctx, "ap-missing", ApprovalClose{Status: ApprovalDenied}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing close err = %v, want ErrNotFound", err)
	}
}

func TestListApprovalsFiltersByStatusAndTaskOldestFirst(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for i, key := range []string{"a", "b", "c"} {
		a := pendingApproval("ap-0000000"+string(rune('1'+i)), key)
		if key == "c" {
			a.TaskID = "task-2"
		}
		if _, _, err := s.InsertApproval(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CloseApproval(ctx, "ap-00000002", ApprovalClose{Status: ApprovalWithdrawn}); err != nil {
		t.Fatal(err)
	}

	pending, err := s.ListApprovals(ctx, ApprovalFilter{Status: ApprovalPending})
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 || pending[0].ID != "ap-00000001" || pending[1].ID != "ap-00000003" {
		t.Fatalf("pending = %+v", ids(pending))
	}
	forTask, err := s.ListApprovals(ctx, ApprovalFilter{Status: ApprovalPending, TaskID: "task-2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(forTask) != 1 || forTask[0].ID != "ap-00000003" {
		t.Fatalf("task-2 pending = %+v", ids(forTask))
	}
	all, err := s.ListApprovals(ctx, ApprovalFilter{})
	if err != nil || len(all) != 3 {
		t.Fatalf("all = %d, %v", len(all), err)
	}
}

func ids(approvals []Approval) []string {
	out := make([]string, len(approvals))
	for i, a := range approvals {
		out[i] = a.ID
	}
	return out
}
