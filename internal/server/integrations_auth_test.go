package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

type fakeIntegrationAuth map[string]service.IntegrationIdentity

func (f fakeIntegrationAuth) Authenticate(_ context.Context, token string) (service.IntegrationIdentity, error) {
	if id, ok := f[token]; ok {
		return id, nil
	}
	return service.IntegrationIdentity{}, service.ErrIntegrationUnauthorized
}

func integrationTestServer(t *testing.T) http.Handler {
	t.Helper()
	srv := New(&mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}, nil, nil, ":0")
	srv.SetAuthToken("full-token")
	srv.SetIntegrationAuth(fakeIntegrationAuth{
		"lgi_both":      {Name: "argus", Scopes: []string{service.ScopeOwnerRead, service.ScopeDelegate}},
		"lgi_owner":     {Name: "argus", Scopes: []string{service.ScopeOwnerRead}},
		"lgi_attention": {Name: "argus", Scopes: []string{service.ScopeAttention}},
	})
	return srv.Handler()
}

func integrationRequest(t *testing.T, h http.Handler, method, path, token string) (int, map[string]string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var body map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return w.Code, body
}

func TestAScopedTokenReachesOnlyIntegrationRoutes(t *testing.T) {
	h := integrationTestServer(t)
	for _, tc := range []struct {
		name, method, path, token string
		status                    int
		code                      string
	}{
		{"board refuses a scoped token", "GET", "/api/board", "lgi_both", 403, "forbidden"},
		{"agents refuse a scoped token", "GET", "/api/agents", "lgi_both", 403, "forbidden"},
		{"owner lookup passes auth", "GET", "/api/integrations/owner?repo=a/b&pr=1", "lgi_owner", 503, "unavailable"},
		{"delegate needs its scope", "POST", "/api/integrations/delegations", "lgi_owner", 403, "missing_scope"},
		{"status needs delegate", "GET", "/api/integrations/delegations/dl-1", "lgi_owner", 403, "missing_scope"},
		{"messages need delegate", "POST", "/api/integrations/delegations/dl-1/messages", "lgi_owner", 403, "missing_scope"},
		{"delegate with its scope passes auth", "POST", "/api/integrations/delegations", "lgi_both", 503, "unavailable"},
		{"attention upsert needs its scope", "PUT", "/api/integrations/attention/case-1", "lgi_both", 403, "missing_scope"},
		{"attention resolve needs its scope", "POST", "/api/integrations/attention/case-1/resolve", "lgi_both", 403, "missing_scope"},
		{"attention sync needs its scope", "POST", "/api/integrations/attention/sync", "lgi_both", 403, "missing_scope"},
		{"attention with its scope passes auth", "PUT", "/api/integrations/attention/case-1", "lgi_attention", 503, "unavailable"},
		{"unknown scoped token", "GET", "/api/integrations/owner?repo=a/b&pr=1", "lgi_unknown", 401, "unauthorized"},
		{"no token on an integration route", "GET", "/api/integrations/owner?repo=a/b&pr=1", "", 401, "unauthorized"},
		{"full token on an integration route", "GET", "/api/integrations/owner?repo=a/b&pr=1", "full-token", 503, "unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := integrationRequest(t, h, tc.method, tc.path, tc.token)
			if status != tc.status || body["error"] != tc.code {
				t.Fatalf("%s %s = %d %v, want %d %s", tc.method, tc.path, status, body, tc.status, tc.code)
			}
			if strings.HasPrefix(tc.path, "/api/integrations/") && body["message"] == "" {
				t.Fatalf("integration error without a message: %v", body)
			}
		})
	}
}

func TestTheWebSocketRefusesAScopedToken(t *testing.T) {
	h := integrationTestServer(t)
	req := httptest.NewRequest("GET", "/ws?token=lgi_both", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("ws with scoped token = %d", w.Code)
	}
}

type fakeOwners struct {
	got     service.OwnerQuery
	matches []service.OwnerMatch
}

func (f *fakeOwners) Lookup(_ context.Context, q service.OwnerQuery) ([]service.OwnerMatch, error) {
	f.got = q
	if q.Repo == "" || (q.PR == 0 && q.Branch == "" && q.SHA == "") {
		return nil, service.ErrOwnerQuery
	}
	return f.matches, nil
}

func TestOwnerLookupRoute(t *testing.T) {
	srv := New(&mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}, nil, nil, ":0")
	srv.SetAuthToken("full-token")
	srv.SetIntegrationAuth(fakeIntegrationAuth{"lgi_owner": {Name: "argus", Scopes: []string{service.ScopeOwnerRead}}})
	owners := &fakeOwners{matches: []service.OwnerMatch{{Task: service.OwnerTask{ID: "t1", Title: "Fix CI", Status: "Doing"}, Agents: []service.OwnerAgent{}, Match: "branch"}}}
	srv.SetOwnerLookup(owners)
	h := srv.Handler()

	req := httptest.NewRequest("GET", "/api/integrations/owner?repo=acme/widget&pr=12&branch=feat-x&sha=0123abcd", nil)
	req.Header.Set("Authorization", "Bearer lgi_owner")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d %s", w.Code, w.Body)
	}
	if owners.got != (service.OwnerQuery{Repo: "acme/widget", PR: 12, Branch: "feat-x", SHA: "0123abcd"}) {
		t.Fatalf("query = %+v", owners.got)
	}
	var body struct {
		Matches []map[string]any `json:"matches"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Matches) != 1 || body.Matches[0]["match"] != "branch" {
		t.Fatalf("body = %s", w.Body)
	}
	if agents, ok := body.Matches[0]["agents"].([]any); !ok || len(agents) != 0 {
		t.Fatalf("agents should be an empty list: %s", w.Body)
	}

	for _, tc := range []struct {
		path   string
		status int
		code   string
	}{
		{"/api/integrations/owner?repo=acme/widget", 400, "bad_request"},
		{"/api/integrations/owner?repo=acme/widget&pr=abc", 400, "bad_request"},
	} {
		if status, body := integrationRequest(t, h, "GET", tc.path, "lgi_owner"); status != tc.status || body["error"] != tc.code {
			t.Errorf("%s = %d %v", tc.path, status, body)
		}
	}
	owners.matches = nil
	if status, body := integrationRequest(t, h, "GET", "/api/integrations/owner?repo=acme/widget&branch=x", "lgi_owner"); status != 404 || body["error"] != "not_found" {
		t.Fatalf("no match = %d %v", status, body)
	}
}

type fakeDelegations struct {
	got     service.DelegationRequest
	err     error
	created bool
}

func (f *fakeDelegations) Delegate(_ context.Context, req service.DelegationRequest) (service.DelegationStatus, bool, error) {
	f.got = req
	if f.err != nil {
		return service.DelegationStatus{}, false, f.err
	}
	return service.DelegationStatus{DelegationID: "dl-1", State: "pending", Route: "message", TaskID: "t1"}, f.created, nil
}

func (f *fakeDelegations) Status(_ context.Context, integration, id string) (service.DelegationStatus, error) {
	if integration != "argus" || id != "dl-1" {
		return service.DelegationStatus{}, &service.DelegationError{Code: "not_found", Message: "no delegation " + id}
	}
	return service.DelegationStatus{DelegationID: "dl-1", State: "delivered", Route: "message", TaskID: "t1", AgentID: "ex-1"}, nil
}

func (f *fakeDelegations) Message(_ context.Context, integration, id, text, key string) (service.DelegationMessageStatus, bool, error) {
	if text == "" {
		return service.DelegationMessageStatus{}, false, &service.DelegationError{Code: "bad_request", Message: "text"}
	}
	if id != "dl-1" {
		return service.DelegationMessageStatus{}, false, &service.DelegationError{Code: "not_delivered", Message: "not yet"}
	}
	return service.DelegationMessageStatus{MessageID: "dm-1", State: "delivered"}, key == "new", nil
}

func delegationServer(t *testing.T, d *fakeDelegations) http.Handler {
	t.Helper()
	srv := New(&mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}, nil, nil, ":0")
	srv.SetAuthToken("full-token")
	srv.SetIntegrationAuth(fakeIntegrationAuth{"lgi_both": {Name: "argus", Scopes: []string{service.ScopeOwnerRead, service.ScopeDelegate}}})
	srv.SetDelegations(d)
	return srv.Handler()
}

func postJSON(t *testing.T, h http.Handler, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer lgi_both")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestDelegationRoutes(t *testing.T) {
	d := &fakeDelegations{created: true}
	h := delegationServer(t, d)
	status, body := postJSON(t, h, "/api/integrations/delegations",
		`{"caseId":"ci:acme/widget#12","mode":"fix","target":{"repo":"acme/widget","pr":12,"branch":"feat-x"},"brief":"b","title":"Argus: CI","idempotencyKey":"argus-1"}`)
	if status != 201 || body["delegationId"] != "dl-1" || body["route"] != "message" {
		t.Fatalf("delegate = %d %v", status, body)
	}
	want := service.DelegationRequest{Integration: "argus", CaseID: "ci:acme/widget#12", Mode: "fix", Target: service.DelegationTarget{Repo: "acme/widget", PR: 12, Branch: "feat-x"}, Brief: "b", Title: "Argus: CI", IdempotencyKey: "argus-1"}
	if d.got != want {
		t.Fatalf("request = %+v", d.got)
	}
	d.created = false
	if status, _ := postJSON(t, h, "/api/integrations/delegations", `{"caseId":"c","mode":"fix","target":{"task":"t1"},"brief":"b","idempotencyKey":"k"}`); status != 200 {
		t.Fatalf("repeat = %d", status)
	}
	for code, want := range map[string]int{"unknown_task": 404, "unknown_repo": 422, "no_checkout": 422, "not_resumable": 422, "idempotency_conflict": 409, "bad_request": 400} {
		d.err = &service.DelegationError{Code: code, Message: "because"}
		status, body := postJSON(t, h, "/api/integrations/delegations", `{"caseId":"c","mode":"fix","target":{"task":"t1"},"brief":"b","idempotencyKey":"k"}`)
		if status != want || body["error"] != code || body["message"] != "because" {
			t.Errorf("%s = %d %v", code, status, body)
		}
	}
	if status, body := postJSON(t, h, "/api/integrations/delegations", `not json`); status != 400 || body["error"] != "bad_request" {
		t.Fatalf("bad json = %d %v", status, body)
	}

	req := httptest.NewRequest("GET", "/api/integrations/delegations/dl-1", nil)
	req.Header.Set("Authorization", "Bearer lgi_both")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"agentId":"ex-1"`) {
		t.Fatalf("status = %d %s", w.Code, w.Body)
	}
	if status, body := integrationRequest(t, h, "GET", "/api/integrations/delegations/dl-other", "lgi_both"); status != 404 || body["error"] != "not_found" {
		t.Fatalf("other = %d %v", status, body)
	}

	if status, body := postJSON(t, h, "/api/integrations/delegations/dl-1/messages", `{"text":"use staging","idempotencyKey":"new"}`); status != 201 || body["messageId"] != "dm-1" {
		t.Fatalf("message = %d %v", status, body)
	}
	if status, _ := postJSON(t, h, "/api/integrations/delegations/dl-1/messages", `{"text":"use staging","idempotencyKey":"old"}`); status != 200 {
		t.Fatalf("repeat message = %d", status)
	}
	if status, body := postJSON(t, h, "/api/integrations/delegations/dl-2/messages", `{"text":"x","idempotencyKey":"k"}`); status != 409 || body["error"] != "not_delivered" {
		t.Fatalf("not delivered = %d %v", status, body)
	}
}
