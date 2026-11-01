package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

type memorySecrets struct {
	mu     sync.Mutex
	values map[string]string
}

func (m *memorySecrets) Read(_ context.Context, name string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.values[name]
	if !ok {
		return "", fmt.Errorf("no secret %s", name)
	}
	return v, nil
}

func (m *memorySecrets) Write(_ context.Context, name, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[name] = value
	return nil
}

// fakeArgus is Argus's token endpoint and replies route. Refresh tokens
// rotate on every use, and a used one gets invalid_grant.
type fakeArgus struct {
	mu          sync.Mutex
	refresh     string
	access      string
	generation  int
	replies     []map[string]any
	keys        []string
	replyStatus []int // answers for the next replies, then 202
	refreshes   int
}

func (f *fakeArgus) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != f.refresh || r.Form.Get("resource") != "https://argus.test/mcp" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		f.generation++
		f.refreshes++
		f.refresh = fmt.Sprintf("refresh-%d", f.generation)
		f.access = fmt.Sprintf("access-%d", f.generation)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": f.access, "refresh_token": f.refresh, "expires_in": 3600, "token_type": "Bearer"})
	})
	mux.HandleFunc("POST /api/cases/{case}/replies", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+f.access || f.access == "" {
			w.WriteHeader(401)
			return
		}
		status := 202
		if len(f.replyStatus) > 0 {
			status, f.replyStatus = f.replyStatus[0], f.replyStatus[1:]
		}
		if status == 202 {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			body["case"] = r.PathValue("case")
			f.replies = append(f.replies, body)
		}
		f.keys = append(f.keys, r.Header.Get("Idempotency-Key"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status == 409 {
			_, _ = w.Write([]byte(`{"error":"closed","message":"the case is closed"}`))
		}
	})
	return mux
}

type replyFixture struct {
	svc     *CaseReplyService
	store   *store.Store
	argus   *fakeArgus
	secrets *memorySecrets
	now     time.Time
}

func newReplyFixture(t *testing.T) *replyFixture {
	t.Helper()
	db := newIntegrationStore(t)
	argus := &fakeArgus{refresh: "refresh-0"}
	srv := httptest.NewServer(argus.handler(t))
	t.Cleanup(srv.Close)
	ctx := context.Background()
	if err := db.SaveIntegration(ctx, store.Integration{Name: "argus", URL: srv.URL, TokenEndpoint: srv.URL + "/token", Resource: "https://argus.test/mcp", ClientID: "legato"}); err != nil {
		t.Fatal(err)
	}
	secrets := &memorySecrets{values: map[string]string{ArgusRefreshSecret: "refresh-0"}}
	f := &replyFixture{store: db, argus: argus, secrets: secrets, now: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)}
	f.svc = NewCaseReplyService(db, CaseReplyOptions{Integration: "argus", URL: srv.URL, Secrets: secrets, Now: func() time.Time { return f.now }})
	_ = db.CreateTask(ctx, store.Task{ID: "t1", Title: "t1", Status: "Doing", CreatedAt: "2026-11-01T00:00:00Z", UpdatedAt: "2026-11-01T00:00:00Z"})
	_ = db.InsertDelegation(ctx, store.Delegation{ID: "dl-1", Integration: "argus", IdempotencyKey: "k", RequestHash: "h", CaseID: "ci:acme/widget#12", Mode: "fix", Target: "{}", Brief: "b", TaskID: "t1", Route: "message", State: store.DelegationDelivered})
	return f
}

func TestAReplyFromTheDelegatedTaskIsSentToArgus(t *testing.T) {
	f := newReplyFixture(t)
	res, err := f.svc.Reply(context.Background(), "t1", "ex-7", CaseReplyInput{DelegationID: "dl-1", Outcome: "finding", Text: "The test is flaky."})
	if err != nil || res.State != "sent" {
		t.Fatalf("reply = %+v, %v", res, err)
	}
	if len(f.argus.replies) != 1 {
		t.Fatalf("replies = %+v", f.argus.replies)
	}
	got := f.argus.replies[0]
	agent, _ := got["agent"].(map[string]any)
	if got["case"] != "ci:acme/widget#12" || got["delegationId"] != "dl-1" || got["outcome"] != "finding" || got["text"] != "The test is flaky." ||
		agent["taskId"] != "t1" || agent["agentId"] != "ex-7" {
		t.Fatalf("reply body = %+v", got)
	}
	if f.argus.keys[0] != res.ID || !strings.HasPrefix(res.ID, "cr-") {
		t.Fatalf("idempotency key = %v, id %s", f.argus.keys, res.ID)
	}
	// The rotated refresh token was kept before it was used.
	if v, _ := f.secrets.Read(context.Background(), ArgusRefreshSecret); v != "refresh-1" {
		t.Fatalf("stored refresh = %s", v)
	}
	// A second reply reuses the access token.
	if _, err := f.svc.Reply(context.Background(), "t1", "ex-7", CaseReplyInput{DelegationID: "dl-1", Outcome: "done", Text: "Fixed."}); err != nil || f.argus.refreshes != 1 {
		t.Fatalf("second reply err %v, refreshes %d", err, f.argus.refreshes)
	}
}

func TestAReplyIsRefusedUnlessTheDelegationIsTheCallersTask(t *testing.T) {
	f := newReplyFixture(t)
	ctx := context.Background()
	for _, tc := range []struct {
		task string
		in   CaseReplyInput
	}{
		{"t2", CaseReplyInput{DelegationID: "dl-1", Outcome: "done", Text: "x"}},
		{"t1", CaseReplyInput{DelegationID: "dl-nope", Outcome: "done", Text: "x"}},
		{"t1", CaseReplyInput{DelegationID: "dl-1", Outcome: "maybe", Text: "x"}},
		{"t1", CaseReplyInput{DelegationID: "dl-1", Outcome: "done", Text: ""}},
	} {
		if _, err := f.svc.Reply(ctx, tc.task, "ex-1", tc.in); err == nil {
			t.Errorf("%s %+v accepted", tc.task, tc.in)
		}
	}
	if counts, _ := f.store.CountCaseReplies(ctx); len(counts) != 0 {
		t.Fatalf("stored = %v", counts)
	}
}

func TestAReplyArgusCannotTakeNowIsQueuedAndSentLaterWithTheSameKey(t *testing.T) {
	f := newReplyFixture(t)
	ctx := context.Background()
	f.argus.replyStatus = []int{503}
	res, err := f.svc.Reply(ctx, "t1", "ex-1", CaseReplyInput{DelegationID: "dl-1", Outcome: "question", Text: "Which database?"})
	if err != nil || res.State != "queued" {
		t.Fatalf("reply = %+v, %v", res, err)
	}
	f.svc.SendDue(ctx)
	if len(f.argus.replies) != 0 {
		t.Fatal("sent before its next attempt was due")
	}
	f.now = f.now.Add(31 * time.Second)
	f.svc.SendDue(ctx)
	if len(f.argus.replies) != 1 || f.argus.keys[0] != f.argus.keys[1] || f.argus.keys[0] != res.ID {
		t.Fatalf("replies %d keys %v", len(f.argus.replies), f.argus.keys)
	}
	r, _ := f.store.GetCaseReply(ctx, res.ID)
	if r.State != store.CaseReplySent {
		t.Fatalf("entry = %+v", r)
	}
}

func TestARefusedReplyIsFinal(t *testing.T) {
	f := newReplyFixture(t)
	ctx := context.Background()
	f.argus.replyStatus = []int{409}
	_, err := f.svc.Reply(ctx, "t1", "ex-1", CaseReplyInput{DelegationID: "dl-1", Outcome: "done", Text: "x"})
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("err = %v", err)
	}
	f.now = f.now.Add(time.Hour)
	f.svc.SendDue(ctx)
	if counts, _ := f.store.CountCaseReplies(ctx); counts[store.CaseReplyFailed] != 1 || len(f.argus.keys) != 1 {
		t.Fatalf("counts %v, attempts %d", counts, len(f.argus.keys))
	}
}

func TestADeadRefreshTokenDisconnectsAndHoldsReplies(t *testing.T) {
	f := newReplyFixture(t)
	ctx := context.Background()
	f.argus.refresh = "something-else" // the stored token was rotated elsewhere and lost
	res, err := f.svc.Reply(ctx, "t1", "ex-1", CaseReplyInput{DelegationID: "dl-1", Outcome: "done", Text: "x"})
	if err != nil || res.State != "queued" {
		t.Fatalf("reply = %+v, %v", res, err)
	}
	in, _ := f.store.GetIntegration(ctx, "argus")
	if in.State != store.IntegrationDisconnected {
		t.Fatalf("state = %s", in.State)
	}
	// Reconnecting stores a token Argus knows; the held reply goes out.
	f.argus.refresh = "fresh"
	_ = f.secrets.Write(ctx, ArgusRefreshSecret, "fresh")
	_ = f.store.SaveIntegration(ctx, *in)
	f.now = f.now.Add(time.Minute)
	f.svc.SendDue(ctx)
	if len(f.argus.replies) != 1 {
		t.Fatalf("replies = %d", len(f.argus.replies))
	}
}

func TestATokenRotatedByAnotherProcessIsReadAgain(t *testing.T) {
	f := newReplyFixture(t)
	ctx := context.Background()
	// Another process refreshed: Argus and the store hold refresh-5, while
	// this service last read refresh-0.
	if _, err := f.svc.Reply(ctx, "t1", "ex-1", CaseReplyInput{DelegationID: "dl-1", Outcome: "done", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	f.argus.mu.Lock()
	f.argus.refresh, f.argus.access = "refresh-5", ""
	f.argus.mu.Unlock()
	_ = f.secrets.Write(ctx, ArgusRefreshSecret, "refresh-5")
	if res, err := f.svc.Reply(ctx, "t1", "ex-1", CaseReplyInput{DelegationID: "dl-1", Outcome: "done", Text: "y"}); err != nil || res.State != "sent" {
		t.Fatalf("reply = %+v, %v", res, err)
	}
}

func TestAReplyUnsentAfterADayFails(t *testing.T) {
	f := newReplyFixture(t)
	ctx := context.Background()
	f.argus.replyStatus = []int{503, 503, 503, 503, 503, 503, 503, 503, 503, 503, 503, 503, 503, 503, 503, 503, 503, 503, 503, 503, 503, 503, 503, 503, 503, 503, 503, 503, 503, 503, 503, 503}
	res, _ := f.svc.Reply(ctx, "t1", "ex-1", CaseReplyInput{DelegationID: "dl-1", Outcome: "done", Text: "x"})
	for i := 0; i < 40; i++ {
		f.now = f.now.Add(time.Hour)
		f.svc.SendDue(ctx)
	}
	r, _ := f.store.GetCaseReply(ctx, res.ID)
	if r.State != store.CaseReplyFailed || !strings.Contains(r.LastError, "24 hours") {
		t.Fatalf("entry = %+v", r)
	}
}

func (m *memorySecrets) Remove(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, name)
	return nil
}
