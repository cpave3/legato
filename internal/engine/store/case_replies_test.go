package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAnIntegrationConnectionIsKeptAndDisconnected(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.GetIntegration(ctx, "argus"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("none err = %v", err)
	}
	in := Integration{Name: "argus", URL: "https://argus.lan", Issuer: "https://argus.lan", TokenEndpoint: "https://argus.lan/token", Resource: "https://argus.lan/mcp", ClientID: "c1"}
	if err := s.SaveIntegration(ctx, in); err != nil {
		t.Fatal(err)
	}
	in.ClientID = "c2"
	if err := s.SaveIntegration(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetIntegration(ctx, "argus")
	if err != nil || got.ClientID != "c2" || got.State != IntegrationConnected || got.Resource != "https://argus.lan/mcp" {
		t.Fatalf("got = %+v, %v", got, err)
	}
	if err := s.SetIntegrationState(ctx, "argus", IntegrationDisconnected); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetIntegration(ctx, "argus"); got.State != IntegrationDisconnected {
		t.Fatalf("state = %s", got.State)
	}
	if err := s.DeleteIntegration(ctx, "argus"); err != nil {
		t.Fatal(err)
	}
}

func TestTheReplyOutboxGivesDueEntriesInOrder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	for i, id := range []string{"cr-1", "cr-2", "cr-3"} {
		r := CaseReply{ID: id, DelegationID: "dl-1", CaseID: "c", TaskID: "t1", Body: `{}`, CreatedAt: now, NextAttemptAt: now.Add(time.Duration(i) * time.Minute)}
		if err := s.InsertCaseReply(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	due, err := s.DueCaseReplies(ctx, now.Add(90*time.Second))
	if err != nil || len(due) != 2 || due[0].ID != "cr-1" || due[1].ID != "cr-2" {
		t.Fatalf("due = %+v, %v", due, err)
	}
	if err := s.MarkCaseReplySent(ctx, "cr-1", now); err != nil {
		t.Fatal(err)
	}
	if err := s.RescheduleCaseReply(ctx, "cr-2", now.Add(time.Hour), "503"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkCaseReplyFailed(ctx, "cr-3", "409 closed"); err != nil {
		t.Fatal(err)
	}
	due, _ = s.DueCaseReplies(ctx, now.Add(2*time.Minute))
	if len(due) != 0 {
		t.Fatalf("due after = %+v", due)
	}
	r, err := s.GetCaseReply(ctx, "cr-2")
	if err != nil || r.Attempts != 1 || r.LastError != "503" || r.State != CaseReplyPending {
		t.Fatalf("rescheduled = %+v, %v", r, err)
	}
	counts, _ := s.CountCaseReplies(ctx)
	if counts[CaseReplySent] != 1 || counts[CaseReplyPending] != 1 || counts[CaseReplyFailed] != 1 {
		t.Fatalf("counts = %v", counts)
	}
}
