package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

type fakeCaseAttention struct {
	integration, key, revision, reason string
	push                               service.CasePush
	keys                               []string
	err                                error
}

func (f *fakeCaseAttention) Upsert(_ context.Context, integration, key string, push service.CasePush) (service.CaseAttentionResult, error) {
	f.integration, f.key, f.push = integration, key, push
	if f.err != nil {
		return service.CaseAttentionResult{}, f.err
	}
	return service.CaseAttentionResult{ItemID: "att-1", TaskID: "t1", Status: "open"}, nil
}

func (f *fakeCaseAttention) Resolve(_ context.Context, integration, key, revision, reason string) error {
	f.integration, f.key, f.revision, f.reason = integration, key, revision, reason
	return f.err
}

func (f *fakeCaseAttention) Sync(_ context.Context, integration string, keys []string) (int, error) {
	f.integration, f.keys = integration, keys
	return 2, f.err
}

func caseAttentionServer(t *testing.T, cases *fakeCaseAttention) *Server {
	t.Helper()
	srv := New(&mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}, nil, nil, ":0")
	srv.SetAuthToken("full-token")
	srv.SetIntegrationAuth(fakeIntegrationAuth{"lgi_attention": {Name: "argus", Scopes: []string{service.ScopeAttention}}})
	if cases != nil {
		srv.SetCaseAttention(cases)
	}
	return srv
}

func caseRequest(t *testing.T, srv *Server, method, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer lgi_attention")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestTheAttentionRoutesPassArgusCasesOn(t *testing.T) {
	cases := &fakeCaseAttention{}
	srv := caseAttentionServer(t, cases)

	status, body := caseRequest(t, srv, "PUT", "/api/integrations/attention/case-1",
		`{"revision":"r1","signal":"argus_case","title":"CI failing","reason":"flaky","detail":"d","urgency":"high","target":{"repo":"acme/widget","pr":12},"link":"https://argus.lan/cases/1"}`)
	if status != 200 || body["itemId"] != "att-1" || body["taskId"] != "t1" || body["status"] != "open" {
		t.Fatalf("upsert = %d %v", status, body)
	}
	if cases.integration != "argus" || cases.key != "case-1" || cases.push.Target.PR != 12 || cases.push.Urgency != "high" || cases.push.Link != "https://argus.lan/cases/1" {
		t.Fatalf("passed on = %+v", cases)
	}

	status, body = caseRequest(t, srv, "POST", "/api/integrations/attention/case-1/resolve", `{"revision":"r2","reason":"fixed"}`)
	if status != 200 || body["status"] != "resolved" || cases.revision != "r2" || cases.reason != "fixed" {
		t.Fatalf("resolve = %d %v (%+v)", status, body, cases)
	}

	status, body = caseRequest(t, srv, "POST", "/api/integrations/attention/sync", `{"keys":["case-1","case-2"]}`)
	if status != 200 || body["resolved"] != float64(2) || len(cases.keys) != 2 {
		t.Fatalf("sync = %d %v", status, body)
	}
}

func TestTheAttentionRoutesAnswerErrorsWithTheirStatus(t *testing.T) {
	for _, tc := range []struct {
		code   string
		status int
	}{
		{"bad_request", 400},
		{"forbidden", 403},
		{"not_found", 404},
		{"invalid_link", 422},
		{"unavailable", 503},
	} {
		cases := &fakeCaseAttention{err: &service.DelegationError{Code: tc.code, Message: "no"}}
		status, body := caseRequest(t, caseAttentionServer(t, cases), "PUT", "/api/integrations/attention/case-1", `{}`)
		if status != tc.status || body["error"] != tc.code {
			t.Errorf("%s = %d %v", tc.code, status, body)
		}
	}
	if status, body := caseRequest(t, caseAttentionServer(t, &fakeCaseAttention{}), "PUT", "/api/integrations/attention/case-1", `not json`); status != 400 || body["error"] != "bad_request" {
		t.Errorf("bad JSON = %d %v", status, body)
	}
	if status, body := caseRequest(t, caseAttentionServer(t, nil), "POST", "/api/integrations/attention/sync", `{"keys":[]}`); status != 503 || body["error"] != "unavailable" {
		t.Errorf("without the service = %d %v", status, body)
	}
}
