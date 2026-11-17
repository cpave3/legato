package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/mcpstdio/mcpstdiotest"
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
	rt, stop := startMCPBroker(&config.Config{DataDir: t.TempDir(), Secrets: config.SecretsConfig{MasterKey: "file"}}, db, credentials, nil)
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

// The runtime's registry asks the operator before it runs a local server,
// applies the change once allowed, and its web server tests the server.
func TestTheMCPRuntimeAsksBeforeItRunsALocalServer(t *testing.T) {
	db, _, _, _ := brokerFixture(t)
	approvals := service.NewApprovalService(db, service.ApprovalServiceOptions{})
	credentials, err := service.NewCredentialBroker(db, approvals, service.CredentialBrokerOptions{WaitTimeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	rt, stop := startMCPBroker(&config.Config{DataDir: t.TempDir(), Secrets: config.SecretsConfig{MasterKey: "file"}}, db, credentials, approvals)
	t.Cleanup(stop)
	srv := server.New(nil, nil, nil, ":0")
	srv.SetAuthToken("secret")
	rt.attach(srv)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret")
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	path, marker := mcpstdiotest.Command()
	k, v, _ := strings.Cut(marker, "=")
	body, _ := json.Marshal(map[string]any{"name": "design", "kind": "stdio", "command": path, "dir": t.TempDir(), "env": map[string]string{k: v}})
	rec := call(http.MethodPost, "/api/settings/mcp/connections", string(body))
	var pending struct {
		ApprovalID string `json:"approval_id"`
	}
	if rec.Code != http.StatusAccepted || json.Unmarshal(rec.Body.Bytes(), &pending) != nil {
		t.Fatalf("add: %d %s", rec.Code, rec.Body)
	}
	if _, err := approvals.Decide(context.Background(), pending.ApprovalID, service.ApprovalDecision{Decision: service.ApprovalAllow, By: "operator"}); err != nil {
		t.Fatal(err)
	}
	rec = call(http.MethodPost, "/api/settings/mcp/connections/design/test", "")
	var res service.MCPTestResult
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &res) != nil || res.Error != "" || len(res.Tools) == 0 {
		t.Fatalf("test: %d %s", rec.Code, rec.Body)
	}
}
