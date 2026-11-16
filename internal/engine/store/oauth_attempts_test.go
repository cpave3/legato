package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestOAuthAttemptsSurviveReopenAndActivateOnlyAfterConfirmation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth.db")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	a, err := s.BeginOAuthAttempt(ctx, OAuthAttempt{ID: "attempt-1", Connection: "mcp-one", Owner: "authority-hash", ReceiverHash: "receiver-hash", StateHash: "state-hash", ExpiresAt: 200}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.GetOAuthAttempt(ctx, a.ID)
	if err != nil || got.Owner != a.Owner || got.StateHash != a.StateHash {
		t.Fatal("transaction did not survive reopening")
	}
	if _, err := s.ActiveOAuthAttempt(ctx, a.Connection); !errors.Is(err, ErrNotFound) {
		t.Fatal("pending transaction became active")
	}
	if err := s.ClaimOAuthAttempt(ctx, a.ID, 101); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimOAuthAttempt(ctx, a.ID, 101); !errors.Is(err, ErrConflict) {
		t.Fatal("second exchange claim was allowed")
	}
	if err := s.StageOAuthAttempt(ctx, a.ID, 102); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ActiveOAuthAttempt(ctx, a.Connection); !errors.Is(err, ErrNotFound) {
		t.Fatal("callback activated without confirmation")
	}
	if err := s.ActivateOAuthAttempt(ctx, a.ID, a.Owner, 103); err != nil {
		t.Fatal(err)
	}
	active, err := s.ActiveOAuthAttempt(ctx, a.Connection)
	if err != nil || active.ID != a.ID {
		t.Fatal("confirmed connection not active")
	}
}

func TestOAuthConfirmationAndDisconnectFenceAllOlderAttempts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	begin := func(id string) OAuthAttempt {
		t.Helper()
		a, err := s.BeginOAuthAttempt(ctx, OAuthAttempt{ID: id, Connection: "one", Owner: "operator", StateHash: id, ReceiverHash: id, ExpiresAt: 200}, 100)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	stage := func(a OAuthAttempt) {
		t.Helper()
		if err := s.ClaimOAuthAttempt(ctx, a.ID, 101); err != nil {
			t.Fatal(err)
		}
		if err := s.StageOAuthAttempt(ctx, a.ID, 102); err != nil {
			t.Fatal(err)
		}
	}
	older, newer := begin("older"), begin("newer")
	stage(older)
	stage(newer)
	if err := s.ActivateOAuthAttempt(ctx, newer.ID, "wrong-authority", 103); !errors.Is(err, ErrConflict) {
		t.Fatal("another operator activated credentials")
	}
	if err := s.ActivateOAuthAttempt(ctx, newer.ID, "operator", 103); err != nil {
		t.Fatal(err)
	}
	if err := s.ActivateOAuthAttempt(ctx, older.ID, "operator", 104); !errors.Is(err, ErrConflict) {
		t.Fatal("older activation replaced newer credentials")
	}
	pending := begin("pending-at-disconnect")
	stage(pending)
	invalidated, err := s.InvalidateOAuthConnection(ctx, "one", 105)
	if err != nil {
		t.Fatal(err)
	}
	if len(invalidated) != 3 {
		t.Fatal("disconnect omitted an attempt from its credential cleanup snapshot")
	}
	if _, err := s.ActiveOAuthAttempt(ctx, "one"); !errors.Is(err, ErrNotFound) {
		t.Fatal("disconnected credentials remained active")
	}
	if err := s.ActivateOAuthAttempt(ctx, pending.ID, "operator", 106); !errors.Is(err, ErrConflict) {
		t.Fatal("late confirmation reconnected after disconnect")
	}
	if err := s.ClaimOAuthAttempt(ctx, pending.ID, 106); !errors.Is(err, ErrConflict) {
		t.Fatal("late response was allowed after disconnect")
	}
	fresh := begin("fresh")
	stage(fresh)
	if err := s.ActivateOAuthAttempt(ctx, fresh.ID, "operator", 107); err != nil {
		t.Fatal(err)
	}
	if active, err := s.ActiveOAuthAttempt(ctx, "one"); err != nil || active.ID != "fresh" {
		t.Fatal("fresh flow could not reconnect")
	}
}

func TestOAuthExchangeHasOneClaimAcrossProcessesAndCannotStartAfterExpiry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth.db")
	one, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer one.Close()
	two, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer two.Close()
	ctx := context.Background()
	if _, err := one.BeginOAuthAttempt(ctx, OAuthAttempt{ID: "one", Connection: "connection", Owner: "operator", StateHash: "one", ReceiverHash: "one", ExpiresAt: 200}, 100); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, s := range []*Store{one, two} {
		go func() { <-start; results <- s.ClaimOAuthAttempt(ctx, "one", 101) }()
	}
	close(start)
	successes, conflicts := 0, 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrConflict):
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal("exchange was not claimed exactly once")
	}
	if _, err := one.BeginOAuthAttempt(ctx, OAuthAttempt{ID: "expired", Connection: "connection", Owner: "operator", StateHash: "expired", ReceiverHash: "expired", ExpiresAt: 101}, 100); err != nil {
		t.Fatal(err)
	}
	if err := two.ClaimOAuthAttempt(ctx, "expired", 101); !errors.Is(err, ErrConflict) {
		t.Fatal("expired transaction could exchange")
	}
}
