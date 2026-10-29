package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestSecretRowsAreKeptByName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.PutSecret(ctx, SecretRow{Name: "eleven", Type: "value", Nonce: []byte{1}, Ciphertext: []byte{2}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSecret(ctx, SecretRow{Name: "eleven", Type: "value", Nonce: []byte{3}, Ciphertext: []byte{4}}); err != nil {
		t.Fatal(err)
	}
	row, err := s.GetSecret(ctx, "eleven")
	if err != nil || row.Nonce[0] != 3 || row.Ciphertext[0] != 4 {
		t.Fatalf("row = %+v, %v; want the replaced value", row, err)
	}
	rows, err := s.ListSecrets(ctx)
	if err != nil || len(rows) != 1 || rows[0].Name != "eleven" {
		t.Fatalf("rows = %+v, %v", rows, err)
	}
	if err := s.DeleteSecret(ctx, "eleven"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSecret(ctx, "eleven"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete err = %v", err)
	}
	if err := s.DeleteSecret(ctx, "eleven"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete err = %v", err)
	}
}

func TestLiveGrantsEndByExpiryLastUseAndExecution(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	one, past, future := 1, "2026-01-01T00:00:00Z", "2999-01-01T00:00:00Z"
	for _, g := range []CredentialGrant{
		{ID: "cg-session", ExecutionID: "ex-1", TaskID: "t", Credential: "eleven", Mode: "session"},
		{ID: "cg-window", ExecutionID: "ex-1", TaskID: "t", Credential: "eleven", Mode: "window", ExpiresAt: &future},
		{ID: "cg-expired", ExecutionID: "ex-1", TaskID: "t", Credential: "eleven", Mode: "window", ExpiresAt: &past},
		{ID: "cg-once", ExecutionID: "ex-1", TaskID: "t", Credential: "eleven", Mode: "request", UsesLeft: &one},
		{ID: "cg-other", ExecutionID: "ex-2", TaskID: "t2", Credential: "eleven", Mode: "session"},
	} {
		if err := s.InsertCredentialGrant(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)

	live := grantIDs(t, s, "ex-1", now)
	if len(live) != 3 || !live["cg-session"] || !live["cg-window"] || !live["cg-once"] {
		t.Fatalf("live = %v", live)
	}
	if err := s.UseCredentialGrant(ctx, "cg-once"); err != nil {
		t.Fatal(err)
	}
	if live := grantIDs(t, s, "ex-1", now); live["cg-once"] {
		t.Fatal("a single-use grant is still live after its use")
	}
	if err := s.UseCredentialGrant(ctx, "cg-once"); !errors.Is(err, ErrConflict) {
		t.Fatalf("using a spent grant: %v", err)
	}
	if err := s.EndCredentialGrants(ctx, "ex-1"); err != nil {
		t.Fatal(err)
	}
	if live := grantIDs(t, s, "ex-1", now); len(live) != 0 {
		t.Fatalf("grants of an ended execution: %v", live)
	}
	if live := grantIDs(t, s, "ex-2", now); !live["cg-other"] {
		t.Fatal("ending one execution ended another's grant")
	}
}

func grantIDs(t *testing.T, s *Store, execution string, now time.Time) map[string]bool {
	t.Helper()
	grants, err := s.LiveCredentialGrants(context.Background(), execution, "eleven", now)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, g := range grants {
		out[g.ID] = true
	}
	return out
}

func TestTheAuditLogIsAHashChainThatShowsAnEdit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, event := range []string{"request", "grant", "use"} {
		if _, err := s.AppendCredentialAudit(ctx, CredentialAuditEntry{Event: event, ExecutionID: "ex-1", TaskID: "t", Credential: "eleven", Operation: "read", Resource: "https://api.elevenlabs.io"}); err != nil {
			t.Fatal(err)
		}
	}
	if bad, err := s.VerifyCredentialAudit(ctx); err != nil || bad != 0 {
		t.Fatalf("verify = %d, %v", bad, err)
	}
	entries, err := s.ListCredentialAudit(ctx, CredentialAuditFilter{})
	if err != nil || len(entries) != 3 || entries[0].Event != "use" || entries[2].PrevHash != "" || entries[1].PrevHash != entries[2].Hash {
		t.Fatalf("entries = %+v, %v; want newest first, chained", entries, err)
	}

	if _, err := s.db.ExecContext(ctx, "UPDATE credential_audit SET resource = 'https://evil.example' WHERE seq = ?", entries[1].Seq); err != nil {
		t.Fatal(err)
	}
	if bad, err := s.VerifyCredentialAudit(ctx); err != nil || bad != entries[1].Seq {
		t.Fatalf("verify after an edit = %d, %v; want row %d", bad, err, entries[1].Seq)
	}
}

func TestConcurrentAppendsKeepOneChain(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.AppendCredentialAudit(ctx, CredentialAuditEntry{Event: "use", Credential: "eleven"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if bad, err := s.VerifyCredentialAudit(ctx); err != nil || bad != 0 {
		t.Fatalf("verify = %d, %v", bad, err)
	}
}

func TestAuditCountsAndFilters(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, e := range []CredentialAuditEntry{
		{Event: "use", Credential: "eleven", TaskID: "t1"},
		{Event: "use", Credential: "eleven", TaskID: "t2"},
		{Event: "refuse", Credential: "eleven", TaskID: "t1"},
		{Event: "use", Credential: "github", TaskID: "t1"},
	} {
		if _, err := s.AppendCredentialAudit(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.CountCredentialAudit(ctx, CredentialAuditFilter{Event: "use", Credential: "eleven", Since: time.Now().Add(-time.Hour)})
	if err != nil || n != 2 {
		t.Fatalf("uses of eleven = %d, %v", n, err)
	}
	entries, _ := s.ListCredentialAudit(ctx, CredentialAuditFilter{TaskID: "t1", Limit: 10})
	if len(entries) != 3 {
		t.Fatalf("t1 entries = %d", len(entries))
	}
}

