package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// A web server the MCP runtime is attached to serves the operator's
// connections and their authorization.
func TestTheMCPRuntimeServesSettings(t *testing.T) {
	db, _, _, _ := brokerFixture(t)
	approvals := service.NewApprovalService(db, service.ApprovalServiceOptions{})
	credentials, err := service.NewCredentialBroker(db, approvals, service.CredentialBrokerOptions{WaitTimeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	rt, stop := startMCPBroker(&config.Config{DataDir: t.TempDir()}, db, credentials)
	t.Cleanup(stop)
	srv := server.New(nil, nil, nil, ":0")
	srv.SetAuthToken("secret")
	rt.attach(srv)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Content-Type", "application/json")
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	if rec := call(http.MethodPost, "/api/settings/mcp/connections", `{"name":"notion","url":"https://mcp.example.test/mcp"}`); rec.Code != http.StatusCreated {
		t.Fatalf("add: %d %s", rec.Code, rec.Body)
	}
	// An attempt nobody began is refused by the flow service, not unserved.
	if rec := call(http.MethodGet, "/api/mcp/oauth/attempts/none", ""); rec.Code != http.StatusConflict {
		t.Fatalf("status: %d %s", rec.Code, rec.Body)
	}
}

// Without a credential broker there is no runtime, and attaching it is
// harmless.
func TestNoMCPRuntimeAttachesNothing(t *testing.T) {
	var rt *mcpBrokerRuntime
	srv := server.New(nil, nil, nil, ":0")
	rt.attach(srv)
}
