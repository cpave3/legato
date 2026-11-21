package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func boardCards(t *testing.T, svc *mockBoardService) []CardResponse {
	t.Helper()
	w := httptest.NewRecorder()
	New(svc, nil, nil, ":0").Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/board", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var resp BoardResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Columns[0].Cards
}

func TestBoardCardsCarryTheTicketKeyAndAddress(t *testing.T) {
	svc := &mockBoardService{
		columns: []service.Column{{Name: "Doing"}},
		cards: map[string][]service.Card{"Doing": {
			{ID: "k3x9q2ma", Title: "Forms", Status: "Doing", Provider: "burnlite", RemoteID: "LEG-386", URL: "https://burnlite.dev/LEG-386"},
			{ID: "local1", Title: "Local", Status: "Doing"},
		}},
	}
	cards := boardCards(t, svc)
	if cards[0].RemoteID != "LEG-386" || cards[0].SourceURL != "https://burnlite.dev/LEG-386" {
		t.Fatalf("card = %+v", cards[0])
	}
	raw, _ := json.Marshal(cards[1])
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	if _, ok := fields["remote_id"]; ok {
		t.Fatalf("local card has remote_id: %s", raw)
	}
	if _, ok := fields["source_url"]; ok {
		t.Fatalf("local card has source_url: %s", raw)
	}
}

type needsYouProducer struct{ items []service.AttentionInput }

func (p *needsYouProducer) Producer() string { return "test-needs-you" }
func (p *needsYouProducer) Snapshot(context.Context) ([]service.AttentionInput, error) {
	return p.items, nil
}

func TestBoardCardsSayWhatNeedsTheOperator(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "needs-you.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	attention := service.NewAttentionService(db, nil)
	producer := &needsYouProducer{items: []service.AttentionInput{
		{SignalKind: service.AttentionSignalOperatorQuestion, SourceKey: "q1", SourceRevision: "1", TaskID: "t1", Title: "Which?", Reason: "asks", Category: "agent", Urgency: "normal", Navigation: service.AttentionNavigation{Kind: "agent", Target: "t1"}},
		{SignalKind: service.AttentionSignalOperatorAction, SourceKey: "a1", SourceRevision: "1", TaskID: "t1", Title: "Run it", Reason: "asks", Category: "agent", Urgency: "normal", Navigation: service.AttentionNavigation{Kind: "agent", Target: "t1"}},
		{SignalKind: service.AttentionSignalPlanDecision, SourceKey: "plan-1", SourceRevision: "1", TaskID: "t3", Title: "Plan", Reason: "decide", Category: "approval", Urgency: "normal", Navigation: service.AttentionNavigation{Kind: "plan", Target: "plan-1"}},
	}}
	if err := attention.RegisterProducer(producer); err != nil {
		t.Fatal(err)
	}
	if err := attention.Reconcile(ctx, producer.Producer()); err != nil {
		t.Fatal(err)
	}
	approvals := service.NewApprovalService(db, service.ApprovalServiceOptions{})
	approval, _, err := approvals.Submit(ctx, service.ApprovalInput{Source: service.ApprovalSourceAgentPermission, SourceKey: "req-1", TaskID: "t2", Title: "Run a command", Summary: "go test ./...", Blocking: true, Options: []service.ApprovalOption{{ID: "allow", Label: "Allow", Kind: service.ApprovalAllow}}})
	if err != nil {
		t.Fatal(err)
	}

	svc := &mockBoardService{
		columns: []service.Column{{Name: "Doing"}},
		cards: map[string][]service.Card{"Doing": {
			{ID: "t1", Title: "Items", Status: "Doing"},
			{ID: "t2", Title: "Approval", Status: "Doing"},
			{ID: "t3", Title: "Plan", Status: "Doing"},
			{ID: "t4", Title: "Nothing", Status: "Doing"},
		}},
	}
	srv := New(svc, nil, nil, ":0")
	srv.SetAttentionService(attention)
	srv.SetApprovalService(approvals)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/board", nil))
	var resp BoardResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	cards := resp.Columns[0].Cards
	if got := cards[0].NeedsYou; got == nil || got.Items["question"] != 1 || got.Items["action"] != 1 || got.Approvals != nil || got.Plans != nil {
		t.Fatalf("items card = %+v", got)
	}
	if got := cards[1].NeedsYou; got == nil || got.Approvals == nil || got.Approvals.Count != 1 || got.Approvals.ID != approval.ID || got.Approvals.Label != "permission to run a command" {
		t.Fatalf("approval card = %+v", got)
	}
	if got := cards[2].NeedsYou; got == nil || got.Plans == nil || got.Plans.PlanID != "plan-1" {
		t.Fatalf("plan card = %+v", got)
	}
	if cards[3].NeedsYou != nil {
		t.Fatalf("a card with nothing open says %+v", cards[3].NeedsYou)
	}
}
