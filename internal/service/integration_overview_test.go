package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestTheIntegrationsOverviewShowsTokensAndArgus(t *testing.T) {
	db := newIntegrationStore(t)
	ctx := context.Background()
	tokens := NewIntegrationTokenService(db)
	secret, err := tokens.Create(ctx, "argus", []string{ScopeOwnerRead, ScopeDelegate, ScopeAttention})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tokens.Create(ctx, "old", []string{ScopeOwnerRead}); err != nil {
		t.Fatal(err)
	}
	if err := tokens.Revoke(ctx, "old"); err != nil {
		t.Fatal(err)
	}
	_ = db.SaveIntegration(ctx, store.Integration{Name: "argus", URL: "http://localhost:5173", TokenEndpoint: "http://localhost:5173/token", ClientID: "c"})
	now := time.Now().UTC()
	_ = db.InsertCaseReply(ctx, store.CaseReply{ID: "cr-1", DelegationID: "dl-1", CaseID: "c", TaskID: "t1", Body: "{}", CreatedAt: now, NextAttemptAt: now})
	if _, err := db.UpsertIntegrationAttention(ctx, store.IntegrationAttention{Integration: "argus", Key: "case-1", Revision: "r1", Signal: "argus_case", Title: "t", Reason: "r", Urgency: "normal", Link: "http://localhost:5173/cases/1"}); err != nil {
		t.Fatal(err)
	}

	o, err := NewIntegrationOverview(db, &ArgusSettings{URL: "http://localhost:5173", Delivery: "auto", Agent: "claude-code"}).Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Tokens) != 2 {
		t.Fatalf("tokens = %+v", o.Tokens)
	}
	byName := map[string]store.IntegrationToken{}
	for _, tok := range o.Tokens {
		byName[tok.Name] = tok
	}
	if len(byName["argus"].Scopes) != 3 || byName["argus"].RevokedAt != nil || byName["old"].RevokedAt == nil {
		t.Fatalf("tokens = %+v", o.Tokens)
	}
	a := o.Argus
	if !a.Configured || a.URL != "http://localhost:5173" || a.Delivery != "auto" || a.Agent != "claude-code" ||
		a.Connection.State != store.IntegrationConnected || a.Connection.Replies[store.CaseReplyPending] != 1 || a.OpenCases != 1 {
		t.Fatalf("argus = %+v", a)
	}
	raw, _ := json.Marshal(o)
	if strings.Contains(string(raw), secret) || strings.Contains(string(raw), "hash") {
		t.Fatalf("the overview gives away a token: %s", raw)
	}
}

func TestTheOverviewSaysWhenArgusIsNotConfigured(t *testing.T) {
	o, err := NewIntegrationOverview(newIntegrationStore(t), nil).Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if o.Argus.Configured || o.Argus.Connection.State != "none" || o.Tokens == nil {
		t.Fatalf("overview = %+v", o)
	}
}
