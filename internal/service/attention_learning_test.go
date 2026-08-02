package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
)

func TestLearningAttentionSnapshotIncludesPendingAndStaleItems(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	ctx := context.Background()

	candidate := recordAttentionLearning(t, svc, "candidate")
	stale := recordAttentionLearning(t, svc, "stale")
	if _, err := db.DB().Exec(`UPDATE learnings SET status = 'stale' WHERE id = ?`, stale.Learning.ID); err != nil {
		t.Fatal(err)
	}
	pendingLearning := recordAttentionLearning(t, svc, "verification")
	accepted, err := svc.Accept(ctx, pendingLearning.Learning.ID, 1, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	inc, err := svc.ProposeIncorporation(ctx, accepted.Learning.ID, accepted.Learning.Revision, "documentation", "docs/guide.md", "update")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := svc.SubmitIncorporation(ctx, inc.ID, inc.Revision, []string{"docs/guide.md:1"}, "source", "ready")
	if err != nil {
		t.Fatal(err)
	}
	staleIncLearning := recordAttentionLearning(t, svc, "stale incorporation")
	accepted, err = svc.Accept(ctx, staleIncLearning.Learning.ID, 1, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	staleInc, err := svc.ProposeIncorporation(ctx, accepted.Learning.ID, accepted.Learning.Revision, "prompt_profile", "profile", "update")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB().Exec(`UPDATE learning_incorporations SET status = 'stale', revision = 3 WHERE id = ?`, staleInc.ID); err != nil {
		t.Fatal(err)
	}

	items, err := NewLearningAttentionAdapter(svc).Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("items = %+v", items)
	}
	byKey := map[string]AttentionInput{}
	for _, item := range items {
		byKey[item.SourceKey] = item
	}
	proposal := byKey["learning:"+candidate.Learning.ID]
	if proposal.SourceRevision != "learning:candidate:1" || proposal.Navigation.Target != candidate.Learning.ID || actionIDs(proposal.Actions) != "open,accept,reject" {
		t.Fatalf("proposal = %+v", proposal)
	}
	if got := byKey["learning:"+stale.Learning.ID]; got.Urgency != "high" || actionIDs(got.Actions) != "open" {
		t.Fatalf("stale learning = %+v", got)
	}
	if got := byKey["incorporation:"+pending.ID]; got.SourceRevision != "incorporation:verification_pending:2" || actionIDs(got.Actions) != "open,accept,reject" {
		t.Fatalf("pending incorporation = %+v", got)
	}
	if got := byKey["incorporation:"+staleInc.ID]; got.SourceRevision != "incorporation:stale:3" || got.Urgency != "high" || actionIDs(got.Actions) != "open" {
		t.Fatalf("stale incorporation = %+v", got)
	}
}

func TestLearningAttentionShowsProposedRevisionWording(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	ctx := context.Background()
	seedCurationLearning(t, db, "learn-attention-revise", "user", "", "session-specific claim")
	seedCurationEvidence(t, db, "lev-attention-revise", "OTHER", "", "linked evidence")
	if _, err := db.DB().Exec(`INSERT INTO learning_evidence_links (learning_id,evidence_id,relation) VALUES ('learn-attention-revise','lev-attention-revise','support')`); err != nil {
		t.Fatal(err)
	}
	packet, err := svc.PrepareLearningPacket(ctx, PrepareLearningPacketInput{AttemptID: "attention-revise", MaxItems: 20, MaxBytes: 64 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	doc := CuratorOperationsDocument{SchemaVersion: 1, PacketDigest: packet.Packet.PacketDigest, Operations: []CuratorOperation{{
		Operation: "revise", LearningID: "learn-attention-revise", Claim: "General rule", Rationale: "Reusable reason", Kind: "pitfall", Scope: "user", Reason: "Remove incident detail",
	}}}
	if _, err := svc.SubmitCuratorOperations(ctx, "attention-revise", doc); err != nil {
		t.Fatal(err)
	}
	items, err := NewLearningAttentionAdapter(svc).Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var proposal AttentionInput
	for _, item := range items {
		if strings.HasPrefix(item.SourceKey, "proposal:") {
			proposal = item
			break
		}
	}
	if proposal.Title != "Revise learning: General rule" || proposal.Detail != "Remove incident detail\n\nReusable reason" || actionIDs(proposal.Actions) != "open,accept,reject" {
		t.Fatalf("revision attention = %+v", proposal)
	}
	attention := NewAttentionService(db, nil)
	adapter := NewLearningAttentionAdapter(svc)
	if err := attention.RegisterProducer(adapter); err != nil {
		t.Fatal(err)
	}
	if err := attention.RegisterHandler(adapter.Producer(), adapter); err != nil {
		t.Fatal(err)
	}
	if err := attention.Reconcile(ctx, adapter.Producer()); err != nil {
		t.Fatal(err)
	}
	storedItems, err := attention.List(ctx, AttentionFilter{Producer: adapter.Producer()})
	if err != nil {
		t.Fatal(err)
	}
	stored := findAttentionItem(t, storedItems, proposal.SourceKey)
	if err := attention.Execute(ctx, stored.ID, AttentionActionRequest{ActionID: "accept", ExpectedRevision: stored.SourceRevision, Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	view, err := svc.Show(ctx, "learn-attention-revise")
	if err != nil {
		t.Fatal(err)
	}
	if view.Learning.Claim != "General rule" || view.Learning.Revision != 2 {
		t.Fatalf("accepted revision = %+v", view.Learning)
	}
}

func TestLearningAttentionActionsUseRevisionedLearningService(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	ctx := context.Background()
	attention := NewAttentionService(db, nil)
	adapter := NewLearningAttentionAdapter(svc)
	if err := attention.RegisterProducer(adapter); err != nil {
		t.Fatal(err)
	}
	if err := attention.RegisterHandler(adapter.Producer(), adapter); err != nil {
		t.Fatal(err)
	}

	proposal := recordAttentionLearning(t, svc, "accept me")
	if err := attention.Reconcile(ctx, adapter.Producer()); err != nil {
		t.Fatal(err)
	}
	items, _ := attention.List(ctx, AttentionFilter{Producer: adapter.Producer()})
	item := findAttentionItem(t, items, "learning:"+proposal.Learning.ID)
	if err := attention.Execute(ctx, item.ID, AttentionActionRequest{ActionID: "accept", ExpectedRevision: item.SourceRevision, Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	view, _ := svc.Show(ctx, proposal.Learning.ID)
	if view.Learning.Status != "validated" || view.Learning.Revision != 2 {
		t.Fatalf("learning = %+v", view.Learning)
	}
	items, _ = attention.List(ctx, AttentionFilter{Producer: adapter.Producer()})
	if len(items) != 1 || items[0].SourceKey != "activation:"+proposal.Learning.ID || actionIDs(items[0].Actions) != "open,enable,leave_disabled" {
		t.Fatalf("activation decision = %+v", items)
	}
	item = items[0]
	if err := attention.Execute(ctx, item.ID, AttentionActionRequest{ActionID: "leave_disabled", ExpectedRevision: item.SourceRevision}); err != nil {
		t.Fatal(err)
	}
	view, _ = svc.Show(ctx, proposal.Learning.ID)
	if view.Learning.PromptEnabled || !view.Learning.PromptDecided {
		t.Fatalf("disabled decision = %+v", view.Learning)
	}
	items, _ = attention.List(ctx, AttentionFilter{Producer: adapter.Producer()})
	if len(items) != 0 {
		t.Fatalf("decided activation remains: %+v", items)
	}

	rejected := recordAttentionLearning(t, svc, "reject me")
	if err := attention.Reconcile(ctx, adapter.Producer()); err != nil {
		t.Fatal(err)
	}
	items, _ = attention.List(ctx, AttentionFilter{Producer: adapter.Producer()})
	item = findAttentionItem(t, items, "learning:"+rejected.Learning.ID)
	if err := attention.Execute(ctx, item.ID, AttentionActionRequest{ActionID: "reject", ExpectedRevision: item.SourceRevision, Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	view, _ = svc.Show(ctx, rejected.Learning.ID)
	if view.Learning.Status != "rejected" {
		t.Fatalf("learning = %+v", view.Learning)
	}
}

func TestLearningAttentionVerificationActionUsesCurrentIncorporationRevision(t *testing.T) {
	db := newServiceTestStore(t)
	learning := NewLearningService(db, nil)
	ctx := context.Background()
	view := recordAttentionLearning(t, learning, "verify me")
	view, err := learning.Accept(ctx, view.Learning.ID, 1, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	inc, err := learning.ProposeIncorporation(ctx, view.Learning.ID, view.Learning.Revision, "documentation", "docs/guide.md", "update")
	if err != nil {
		t.Fatal(err)
	}
	inc, err = learning.SubmitIncorporation(ctx, inc.ID, inc.Revision, []string{"docs/guide.md:1"}, "source", "ready")
	if err != nil {
		t.Fatal(err)
	}
	attention := NewAttentionService(db, nil)
	adapter := NewLearningAttentionAdapter(learning)
	if err := attention.RegisterProducer(adapter); err != nil {
		t.Fatal(err)
	}
	if err := attention.RegisterHandler(adapter.Producer(), adapter); err != nil {
		t.Fatal(err)
	}
	if err := attention.Reconcile(ctx, adapter.Producer()); err != nil {
		t.Fatal(err)
	}
	items, _ := attention.List(ctx, AttentionFilter{Producer: adapter.Producer()})
	item := findAttentionItem(t, items, "incorporation:"+inc.ID)
	if err := attention.Execute(ctx, item.ID, AttentionActionRequest{ActionID: "reject", ExpectedRevision: item.SourceRevision, Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	stored, err := db.GetLearningIncorporation(ctx, inc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "failed" || stored.Revision != inc.Revision+1 {
		t.Fatalf("incorporation = %+v", stored)
	}
}

func TestLearningAttentionReconcilesLearningChangedEvents(t *testing.T) {
	db := newServiceTestStore(t)
	bus := events.New()
	learning := NewLearningService(db, bus)
	attention := NewAttentionService(db, bus)
	adapter := NewLearningAttentionAdapter(learning)
	if err := attention.RegisterProducerWithEvents(adapter, events.EventLearningChanged); err != nil {
		t.Fatal(err)
	}
	stop := attention.StartEventReconciliation(context.Background())
	defer stop()

	proposal := recordAttentionLearning(t, learning, "event candidate")
	waitForAttention(t, attention, 1)
	if _, err := learning.Accept(context.Background(), proposal.Learning.ID, 1, "", "", ""); err != nil {
		t.Fatal(err)
	}
	waitForAttention(t, attention, 1)
}

func recordAttentionLearning(t *testing.T, svc *LearningService, claim string) *LearningView {
	t.Helper()
	view, err := svc.Record(context.Background(), LearningRecordInput{Scope: "user", Kind: "preference", Claim: claim, SourceKind: "test", SourceRef: claim, Author: "user"})
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func actionIDs(actions []AttentionAction) string {
	out := ""
	for i, action := range actions {
		if i > 0 {
			out += ","
		}
		out += action.ID
	}
	return out
}

func findAttentionItem(t *testing.T, items []AttentionItem, key string) AttentionItem {
	t.Helper()
	for _, item := range items {
		if item.SourceKey == key {
			return item
		}
	}
	t.Fatalf("attention item %q not found in %+v", key, items)
	return AttentionItem{}
}

func waitForAttention(t *testing.T, svc *AttentionService, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		items, err := svc.List(context.Background(), AttentionFilter{Producer: "learnings"})
		if err == nil && len(items) == count {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	items, _ := svc.List(context.Background(), AttentionFilter{Producer: "learnings"})
	t.Fatalf("attention count = %d, want %d: %+v", len(items), count, items)
}
