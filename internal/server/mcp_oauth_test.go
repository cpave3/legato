package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cpave3/legato/internal/engine/egress"
	"github.com/cpave3/legato/internal/engine/oauthclient"
	"github.com/cpave3/legato/internal/service"
)

// fakeMCPOAuth records what the routes asked of the flow service, with the
// owner each call was made for.
type fakeMCPOAuth struct {
	mu        sync.Mutex
	calls     []string
	owners    []string
	begun     []service.OAuthFlowRequest
	delivered []oauthclient.AuthorizationResponse
	keys      []string
	beginErr  error
	statusErr error
}

func (f *fakeMCPOAuth) record(call, owner string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	f.owners = append(f.owners, owner)
}

func (f *fakeMCPOAuth) seen() ([]string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...), append([]string(nil), f.owners...)
}

func (f *fakeMCPOAuth) Begin(_ context.Context, r service.OAuthFlowRequest) (service.OAuthFlowBegin, error) {
	f.record("begin "+r.Connection, r.Owner)
	f.mu.Lock()
	f.begun = append(f.begun, r)
	f.mu.Unlock()
	if f.beginErr != nil {
		return service.OAuthFlowBegin{}, f.beginErr
	}
	out := service.OAuthFlowBegin{ID: "att-1", AuthorizeURL: "https://auth.example.test/authorize?state=s", ExpiresAt: 1900000000}
	if r.Mode == "app" {
		out.ReturnKey = "rk"
	}
	return out, nil
}

func (f *fakeMCPOAuth) Status(_ context.Context, id, owner string) (service.OAuthFlowStatus, error) {
	f.record("status "+id, owner)
	if f.statusErr != nil {
		return service.OAuthFlowStatus{}, f.statusErr
	}
	return service.OAuthFlowStatus{ID: id, Connection: "notion", Status: "awaiting_confirmation", ExpiresAt: 1900000000,
		Evidence: &service.OAuthEvidence{Issuer: "https://auth.example.test", Resource: "https://mcp.example.test/mcp", Account: "ana@example.test"}}, nil
}

func (f *fakeMCPOAuth) Deliver(_ context.Context, id, owner, key string, response oauthclient.AuthorizationResponse) (service.OAuthFlowStatus, error) {
	f.record("deliver "+id, owner)
	f.mu.Lock()
	f.keys = append(f.keys, key)
	f.delivered = append(f.delivered, response)
	f.mu.Unlock()
	return service.OAuthFlowStatus{ID: id, Connection: "notion", Status: "awaiting_confirmation"}, nil
}

func (f *fakeMCPOAuth) Confirm(_ context.Context, id, owner string) error {
	f.record("confirm "+id, owner)
	return nil
}

func (f *fakeMCPOAuth) Cancel(_ context.Context, id, owner string) error {
	f.record("cancel "+id, owner)
	return nil
}

func (f *fakeMCPOAuth) Disconnect(_ context.Context, connection string) (service.OAuthDisconnect, error) {
	f.record("disconnect "+connection, "")
	return service.OAuthDisconnect{Upstream: "revoked"}, nil
}

func mcpOAuthServer(t *testing.T) (*Server, string, *fakeMCPOAuth) {
	t.Helper()
	srv, reeve := mcpServer(t)
	flows := &fakeMCPOAuth{}
	srv.SetMCPOAuth(flows)
	return srv, reeve, flows
}

// callJSON is a call as the web UI and the desktop app make it.
func callJSON(srv *Server, token, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestTheOperatorAuthorizesAnMCPConnectionFromSettings(t *testing.T) {
	srv, _, flows := mcpOAuthServer(t)
	rec := callJSON(srv, "secret", http.MethodPost, "/api/mcp/connections/notion/oauth/begin", `{"mode":"web"}`)
	var begun map[string]any
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &begun) != nil || begun["id"] != "att-1" || begun["authorize_url"] == nil || begun["expires_at"] == nil {
		t.Fatalf("begin: %d %s", rec.Code, rec.Body)
	}
	if _, ok := begun["return_key"]; ok {
		t.Fatalf("a web page was given the return capability: %s", rec.Body)
	}
	rec = callJSON(srv, "secret", http.MethodGet, "/api/mcp/oauth/attempts/att-1", "")
	var status service.OAuthFlowStatus
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &status) != nil || status.Evidence == nil || status.Evidence.Account != "ana@example.test" {
		t.Fatalf("status: %d %s", rec.Code, rec.Body)
	}
	if rec := callJSON(srv, "secret", http.MethodPost, "/api/mcp/oauth/attempts/att-1/confirm", `{}`); rec.Code != http.StatusOK {
		t.Fatalf("confirm: %d %s", rec.Code, rec.Body)
	}
	if rec := callJSON(srv, "secret", http.MethodPost, "/api/mcp/oauth/attempts/att-1/cancel", `{}`); rec.Code != http.StatusNoContent {
		t.Fatalf("cancel: %d %s", rec.Code, rec.Body)
	}
	rec = callJSON(srv, "secret", http.MethodPost, "/api/mcp/connections/notion/oauth/disconnect", `{}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"upstream":"revoked"`) {
		t.Fatalf("disconnect: %d %s", rec.Code, rec.Body)
	}
	calls, owners := flows.seen()
	want := []string{"begin notion", "status att-1", "confirm att-1", "status att-1", "cancel att-1", "disconnect notion"}
	if fmt.Sprint(calls) != fmt.Sprint(want) {
		t.Fatalf("calls %v", calls)
	}
	for _, owner := range owners[:5] {
		if owner == "" || owner != owners[0] || strings.Contains(owner, "secret") {
			t.Fatalf("owners %q", owners)
		}
	}
	if flows.begun[0].Mode != "web" || flows.begun[0].RedirectURI != "" {
		t.Fatalf("begin %+v", flows.begun[0])
	}
}

// The owner of an attempt is the authenticated operator credential, never
// anything the request body says.
func TestAnAttemptsOwnerComesFromTheCredentialNotTheBody(t *testing.T) {
	srv, _, flows := mcpOAuthServer(t)
	if rec := callJSON(srv, "secret", http.MethodPost, "/api/mcp/connections/notion/oauth/begin", `{"mode":"web","owner":"someone"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("owner in the body: %d %s", rec.Code, rec.Body)
	}
	callJSON(srv, "secret", http.MethodPost, "/api/mcp/connections/notion/oauth/begin", `{"mode":"web"}`)
	srv.SetAuthToken("rotated")
	callJSON(srv, "rotated", http.MethodGet, "/api/mcp/oauth/attempts/att-1", "")
	_, owners := flows.seen()
	if len(owners) != 2 || owners[0] == owners[1] {
		t.Fatalf("a new operator token kept the old one's attempts: %q", owners)
	}
}

func TestOnlyTheOperatorAuthorizesMCPConnections(t *testing.T) {
	srv, reeve, flows := mcpOAuthServer(t)
	for _, token := range []string{reeve, "lgi_main"} {
		for _, call := range []struct{ method, path string }{
			{http.MethodPost, "/api/mcp/connections/notion/oauth/begin"},
			{http.MethodGet, "/api/mcp/oauth/attempts/att-1"},
			{http.MethodPost, "/api/mcp/oauth/attempts/att-1/deliver"},
			{http.MethodPost, "/api/mcp/oauth/attempts/att-1/confirm"},
			{http.MethodPost, "/api/mcp/oauth/attempts/att-1/cancel"},
			{http.MethodPost, "/api/mcp/connections/notion/oauth/disconnect"},
		} {
			if rec := callJSON(srv, token, call.method, call.path, `{"mode":"web"}`); rec.Code != http.StatusForbidden {
				t.Errorf("%s %s with %.8s: %d %s", call.method, call.path, token, rec.Code, rec.Body)
			}
		}
	}
	if calls, _ := flows.seen(); len(calls) != 0 {
		t.Fatalf("calls %v", calls)
	}
}

// A form or text post, which a page of another origin can send without a
// preflight, changes nothing.
func TestOAuthChangesMustBeSentAsJSON(t *testing.T) {
	srv, _, flows := mcpOAuthServer(t)
	for _, ct := range []string{"", "text/plain", "application/x-www-form-urlencoded", "multipart/form-data; boundary=x"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/mcp/oauth/attempts/att-1/confirm", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer secret")
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Errorf("%q: %d %s", ct, rec.Code, rec.Body)
		}
	}
	if calls, _ := flows.seen(); len(calls) != 0 {
		t.Fatalf("calls %v", calls)
	}
}

func TestTheAppDeliversTheAuthorizationResponse(t *testing.T) {
	srv, _, flows := mcpOAuthServer(t)
	rec := callJSON(srv, "secret", http.MethodPost, "/api/mcp/connections/notion/oauth/begin", `{"mode":"app","redirect_uri":"http://127.0.0.1:40000/callback"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"return_key":"rk"`) {
		t.Fatalf("begin: %d %s", rec.Code, rec.Body)
	}
	rec = callJSON(srv, "secret", http.MethodPost, "/api/mcp/oauth/attempts/att-1/deliver", `{"return_key":"rk","state":"s","iss":"https://auth.example.test","code":"c"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "awaiting_confirmation") {
		t.Fatalf("deliver: %d %s", rec.Code, rec.Body)
	}
	want := oauthclient.AuthorizationResponse{Code: "c", State: "s", Issuer: "https://auth.example.test"}
	if flows.keys[0] != "rk" || flows.delivered[0] != want || flows.begun[0].RedirectURI != "http://127.0.0.1:40000/callback" {
		t.Fatalf("delivered %q %+v %+v", flows.keys, flows.delivered, flows.begun)
	}
	huge := `{"return_key":"rk","state":"` + strings.Repeat("s", 20<<10) + `","code":"c"}`
	if rec := callJSON(srv, "secret", http.MethodPost, "/api/mcp/oauth/attempts/att-1/deliver", huge); rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized: %d", rec.Code)
	}
	if len(flows.delivered) != 1 {
		t.Fatal("an oversized response was delivered")
	}
}

func TestOAuthFailuresAreExplainedWithoutUpstreamText(t *testing.T) {
	srv, _, flows := mcpOAuthServer(t)
	for _, c := range []struct {
		err  error
		code int
		want string
	}{
		{service.ErrOAuthRegistrationUnsupported, http.StatusUnprocessableEntity, "pre-registered client"},
		{service.ErrOAuthFlowInput, http.StatusBadRequest, ""},
		{service.ErrOAuthFlowRefused, http.StatusConflict, "start again"},
		{fmt.Errorf("dial: %w <html>provider says no</html>", egress.ErrDenied), http.StatusBadGateway, "network policy"},
		{fmt.Errorf("%w: <html>provider says no</html>", oauthclient.ErrNoServerMetadata), http.StatusBadGateway, "OAuth metadata"},
		{errors.New("<html>provider says no</html>"), http.StatusBadGateway, "could not be reached"},
	} {
		flows.beginErr = c.err
		rec := callJSON(srv, "secret", http.MethodPost, "/api/mcp/connections/notion/oauth/begin", `{"mode":"web"}`)
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.want) || strings.Contains(rec.Body.String(), "provider says no") {
			t.Errorf("%v: %d %s", c.err, rec.Code, rec.Body)
		}
	}
	flows.statusErr = service.ErrOAuthFlowRefused
	if rec := callJSON(srv, "secret", http.MethodGet, "/api/mcp/oauth/attempts/att-9", ""); rec.Code != http.StatusConflict {
		t.Fatalf("someone else's attempt: %d %s", rec.Code, rec.Body)
	}
}
