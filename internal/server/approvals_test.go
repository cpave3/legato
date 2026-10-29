package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type approvalsResolver struct{ decisions []service.ApprovalDecision }

func (r *approvalsResolver) Resolve(_ context.Context, _ service.Approval, d service.ApprovalDecision) error {
	r.decisions = append(r.decisions, d)
	return nil
}

func newApprovalsServer(t *testing.T) (*Server, *service.ApprovalService, *approvalsResolver) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "approvals-api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	approvals := service.NewApprovalService(db, service.ApprovalServiceOptions{})
	resolver := &approvalsResolver{}
	approvals.RegisterResolver("test", resolver)
	srv := New(nil, nil, nil, "")
	srv.SetApprovalService(approvals)
	return srv, approvals, resolver
}

func submitTestApproval(t *testing.T, approvals *service.ApprovalService, key, task string) *service.Approval {
	t.Helper()
	approval, _, err := approvals.Submit(context.Background(), service.ApprovalInput{
		Source: "test", SourceKey: key, TaskID: task, Title: "Run a command", Summary: "ls",
		Options: []service.ApprovalOption{{ID: "once", Label: "Allow once", Kind: service.ApprovalAllow}, {ID: "deny", Label: "Deny", Kind: service.ApprovalDeny}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return approval
}

func serve(srv *Server, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func TestApprovalsAPIListsPendingApprovalsOldestFirst(t *testing.T) {
	srv, approvals, _ := newApprovalsServer(t)
	first := submitTestApproval(t, approvals, "a", "task-1")
	second := submitTestApproval(t, approvals, "b", "task-2")
	submitTestApproval(t, approvals, "c", "task-1")
	if _, err := approvals.Decide(context.Background(), submitTestApproval(t, approvals, "d", "task-1").ID, service.ApprovalDecision{Decision: service.ApprovalDeny}); err != nil {
		t.Fatal(err)
	}

	rec := serve(srv, http.MethodGet, "/api/approvals?status=pending", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Approvals []service.Approval `json:"approvals"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Approvals) != 3 || body.Approvals[0].ID != first.ID || body.Approvals[1].ID != second.ID {
		t.Fatalf("approvals = %+v", body.Approvals)
	}
	if len(body.Approvals[0].Options) != 2 {
		t.Fatalf("options missing from the list: %s", rec.Body)
	}

	rec = serve(srv, http.MethodGet, "/api/approvals?status=pending&task=task-2", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Approvals) != 1 || body.Approvals[0].ID != second.ID {
		t.Fatalf("task-2 approvals = %+v", body.Approvals)
	}
}

func TestApprovalsAPIDecidesOnceAndReportsAConflictAfter(t *testing.T) {
	srv, approvals, resolver := newApprovalsServer(t)
	approval := submitTestApproval(t, approvals, "a", "task-1")

	rec := serve(srv, http.MethodPost, "/api/approvals/"+approval.ID+"/decision", `{"decision":"allow","option":"once"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var decided service.Approval
	_ = json.Unmarshal(rec.Body.Bytes(), &decided)
	if decided.Status != store.ApprovalApproved || decided.DecidedBy != "web" {
		t.Fatalf("decided = %+v", decided)
	}
	if len(resolver.decisions) != 1 || resolver.decisions[0].Option != "once" {
		t.Fatalf("decisions = %+v", resolver.decisions)
	}

	rec = serve(srv, http.MethodPost, "/api/approvals/"+approval.ID+"/decision", `{"decision":"deny"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second decision status = %d, want 409", rec.Code)
	}
}

func TestApprovalsAPIRefusesABadDecisionAndAnUnknownApproval(t *testing.T) {
	srv, approvals, _ := newApprovalsServer(t)
	approval := submitTestApproval(t, approvals, "a", "task-1")

	if rec := serve(srv, http.MethodPost, "/api/approvals/"+approval.ID+"/decision", `{"decision":"allow","option":"always"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown option status = %d, want 400", rec.Code)
	}
	if rec := serve(srv, http.MethodGet, "/api/approvals/ap-00000000", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown approval status = %d, want 404", rec.Code)
	}
	if rec := serve(srv, http.MethodGet, "/api/approvals/"+approval.ID, ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"title":"Run a command"`) {
		t.Fatalf("get = %d %s", rec.Code, rec.Body)
	}
}

func TestApprovalsAPIDismisses(t *testing.T) {
	srv, approvals, resolver := newApprovalsServer(t)
	approval := submitTestApproval(t, approvals, "a", "task-1")

	rec := serve(srv, http.MethodPost, "/api/approvals/"+approval.ID+"/dismiss", `{"note":"later"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if len(resolver.decisions) != 1 || resolver.decisions[0].Decision != service.ApprovalDismiss || resolver.decisions[0].Feedback != "later" {
		t.Fatalf("decisions = %+v", resolver.decisions)
	}
}

// The navigation summary counts pending approvals, so the web UI mounts its
// tray (and asks for the list) only when something waits.
func TestNavigationSummaryCountsPendingApprovals(t *testing.T) {
	srv, approvals, _ := newApprovalsServer(t)
	if count, err := srv.pendingApprovals(context.Background()); err != nil || count != 0 {
		t.Fatalf("none pending: %d, %v", count, err)
	}
	submitTestApproval(t, approvals, "a", "task-1")
	submitTestApproval(t, approvals, "b", "task-1")
	if count, err := srv.pendingApprovals(context.Background()); err != nil || count != 2 {
		t.Fatalf("pending approvals = %d, %v; want 2", count, err)
	}
	if count, err := New(nil, nil, nil, "").pendingApprovals(context.Background()); err != nil || count != 0 {
		t.Fatalf("without an approval service: %d, %v", count, err)
	}
}
