package service

import (
	"context"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestIntegrationProblemsReachAttention(t *testing.T) {
	db := newIntegrationStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	_ = db.InsertDelegation(ctx, store.Delegation{ID: "dl-1", Integration: "argus", IdempotencyKey: "k1", RequestHash: "h", CaseID: "ci:a/b#1", Mode: "fix", Target: "{}", Brief: "b", TaskID: "t1", Route: "start", State: store.DelegationPending})
	_ = db.SetDelegationFailed(ctx, "dl-1", "spawn_failed", "tmux is gone")
	_ = db.InsertDelegation(ctx, store.Delegation{ID: "dl-2", Integration: "argus", IdempotencyKey: "k2", RequestHash: "h", CaseID: "ci:a/b#2", Mode: "fix", Target: "{}", Brief: "b", TaskID: "t1", Route: "message", State: store.DelegationPending})
	_ = db.SetDelegationFailed(ctx, "dl-2", "denied", "the person refused this delegation")
	_ = db.InsertCaseReply(ctx, store.CaseReply{ID: "cr-1", DelegationID: "dl-3", CaseID: "ci:a/b#3", TaskID: "t2", Body: "{}", CreatedAt: now, NextAttemptAt: now})
	_ = db.MarkCaseReplyFailed(ctx, "cr-1", "not taken by Argus within 24 hours")
	_ = db.SaveIntegration(ctx, store.Integration{Name: "argus", URL: "https://argus.lan", TokenEndpoint: "https://argus.lan/token", ClientID: "c"})
	_ = db.SetIntegrationState(ctx, "argus", store.IntegrationDisconnected)

	items, err := NewIntegrationAttentionAdapter(db).Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]AttentionInput{}
	for _, item := range items {
		got[item.SignalKind+" "+item.SourceKey] = item
	}
	if len(items) != 3 {
		t.Fatalf("items = %+v", items)
	}
	if d := got[AttentionSignalDelegationFailed+" dl-1"]; d.TaskID != "t1" || d.Navigation.Kind != "task" {
		t.Fatalf("delegation item = %+v", d)
	}
	if r := got[AttentionSignalCaseReplyFailed+" cr-1"]; r.TaskID != "t2" {
		t.Fatalf("reply item = %+v", r)
	}
	if c := got[AttentionSignalIntegrationDisconnected+" argus"]; c.Urgency != "high" {
		t.Fatalf("connection item = %+v", c)
	}
	for _, signal := range []string{AttentionSignalDelegationFailed, AttentionSignalCaseReplyFailed, AttentionSignalIntegrationDisconnected} {
		if !knownAttentionSignals[signal] {
			t.Errorf("%s is not allowed", signal)
		}
	}
}

func TestALostBurnliteConnectionReachesAttention(t *testing.T) {
	db := newIntegrationStore(t)
	ctx := context.Background()
	_ = db.SaveIntegration(ctx, store.Integration{Name: "burnlite", URL: "https://burnlite.example.com", TokenEndpoint: "https://burnlite.example.com/api/oauth/token", ClientID: "c"})
	items, _ := NewIntegrationAttentionAdapter(db).Snapshot(ctx)
	if len(items) != 0 {
		t.Fatalf("a connected Burnlite raised %+v", items)
	}
	_ = db.SetIntegrationState(ctx, "burnlite", store.IntegrationDisconnected)
	items, err := NewIntegrationAttentionAdapter(db).Snapshot(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("items = %+v, %v", items, err)
	}
	item := items[0]
	if item.SignalKind != AttentionSignalIntegrationDisconnected || item.SourceKey != "burnlite" || item.Urgency != "high" ||
		item.Navigation.Target != "integrations" || item.Title != "Legato lost its connection to Burnlite" {
		t.Fatalf("item = %+v", item)
	}
}
