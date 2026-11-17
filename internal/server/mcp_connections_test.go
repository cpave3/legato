package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

func mcpServer(t *testing.T) (*Server, string) {
	t.Helper()
	srv, db := reeveServer(t)
	srv.SetIntegrationAuth(fakeIntegrationAuth{
		"lgi_main": {Name: "main", Kind: service.IntegrationKindMain, InstanceID: "srv-0123456789abcdef", Scopes: []string{service.ScopeHandoff, service.ScopeReeve}},
	})
	srv.SetMCPConnectionService(service.NewMCPConnectionService(db, service.MCPConnectionOptions{}))
	return srv, reeveToken(t, srv, db)
}

func TestTheOperatorAdministersMCPConnections(t *testing.T) {
	srv, _ := mcpServer(t)
	rec := callAs(srv, "secret", http.MethodPost, "/api/settings/mcp/connections", `{"name":"notion","url":"https://mcp.example.test/mcp"}`)
	var added service.MCPConnectionState
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &added) != nil || added.Authorization != "required" || added.Problem == nil {
		t.Fatalf("add: %d %s", rec.Code, rec.Body)
	}
	if rec := callAs(srv, "secret", http.MethodPost, "/api/settings/mcp/connections", `{"name":"notion","url":"https://mcp.example.test/mcp"}`); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate: %d %s", rec.Code, rec.Body)
	}
	if rec := callAs(srv, "secret", http.MethodPost, "/api/settings/mcp/connections", `{"name":"bad name","url":"https://mcp.example.test/mcp"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid: %d %s", rec.Code, rec.Body)
	}
	if rec := callAs(srv, "secret", http.MethodPost, "/api/settings/mcp/connections", `{"name":"x","url":"https://mcp.example.test/mcp","access_token":"t"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("token field accepted: %d %s", rec.Code, rec.Body)
	}
	rec = callAs(srv, "secret", http.MethodGet, "/api/settings/mcp/connections", "")
	var list struct {
		Connections []service.MCPConnectionState `json:"connections"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &list) != nil || len(list.Connections) != 1 {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	body := fmt.Sprintf(`{"enabled":false,"expected_revision":%d}`, added.Revision)
	rec = callAs(srv, "secret", http.MethodPatch, "/api/settings/mcp/connections/notion", body)
	var off service.MCPConnectionState
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &off) != nil || off.Enabled || off.Problem.Code != "disabled" {
		t.Fatalf("disable: %d %s", rec.Code, rec.Body)
	}
	if rec := callAs(srv, "secret", http.MethodPatch, "/api/settings/mcp/connections/notion", body); rec.Code != http.StatusConflict {
		t.Fatalf("stale: %d %s", rec.Code, rec.Body)
	}
	if rec := callAs(srv, "secret", http.MethodGet, "/api/settings/mcp/connections/notion", ""); rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body)
	}
	if rec := callAs(srv, "secret", http.MethodDelete, "/api/settings/mcp/connections/notion", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body)
	}
	if rec := callAs(srv, "secret", http.MethodGet, "/api/settings/mcp/connections/notion", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("get removed: %d %s", rec.Code, rec.Body)
	}
}

// Paired instances' tokens and agents' (the Reeve's) tokens gain no
// administration of MCP connections.
func TestOnlyTheOperatorAdministersMCPConnections(t *testing.T) {
	srv, reeve := mcpServer(t)
	if rec := callAs(srv, "secret", http.MethodPost, "/api/settings/mcp/connections", `{"name":"notion","url":"https://mcp.example.test/mcp"}`); rec.Code != http.StatusCreated {
		t.Fatalf("add: %d %s", rec.Code, rec.Body)
	}
	for _, token := range []string{reeve, "lgi_main"} {
		for _, call := range []struct{ method, path, body string }{
			{http.MethodPost, "/api/settings/mcp/connections", `{"name":"other","url":"https://mcp.example.test/mcp"}`},
			{http.MethodPatch, "/api/settings/mcp/connections/notion", `{"enabled":false,"expected_revision":1}`},
			{http.MethodDelete, "/api/settings/mcp/connections/notion", ""},
		} {
			if rec := callAs(srv, token, call.method, call.path, call.body); rec.Code != http.StatusForbidden {
				t.Errorf("%s %s %s with %.8s: %d %s", call.method, call.path, call.body, token, rec.Code, rec.Body)
			}
		}
	}
	if rec := callAs(srv, "lgi_main", http.MethodGet, "/api/settings/mcp/connections", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("a paired main listed connections: %d", rec.Code)
	}
	rec := callAs(srv, "secret", http.MethodGet, "/api/settings/mcp/connections/notion", "")
	var got service.MCPConnectionState
	if json.Unmarshal(rec.Body.Bytes(), &got) != nil || !got.Enabled || got.Revision != 1 {
		t.Fatalf("connection changed: %s", rec.Body)
	}
}

type fakeMCPTester struct{ tested []string }

func (f *fakeMCPTester) Test(_ context.Context, name string) (service.MCPTestResult, error) {
	f.tested = append(f.tested, name)
	return service.MCPTestResult{Tools: []service.MCPTestTool{{Name: "list_blocks"}}, Stderr: "starting"}, nil
}

// A local server is not added by the call: the operator approves it, and
// the answer says which approval to look for.
func TestAddingALocalServerAnswersWithTheApprovalItWaitsFor(t *testing.T) {
	srv, db := reeveServer(t)
	approvals := service.NewApprovalService(db, service.ApprovalServiceOptions{})
	conns := service.NewMCPConnectionService(db, service.MCPConnectionOptions{})
	conns.SetApprovals(approvals)
	srv.SetMCPConnectionService(conns)
	body := fmt.Sprintf(`{"name":"design","kind":"stdio","command":"/usr/bin/node","args":["tools/design/mcp.mjs"],"dir":%q,"env":{"PATH":"/opt/node/bin"}}`, t.TempDir())
	rec := callAs(srv, "secret", http.MethodPost, "/api/settings/mcp/connections", body)
	var pending struct {
		Status     string `json:"status"`
		ApprovalID string `json:"approval_id"`
	}
	if rec.Code != http.StatusAccepted || json.Unmarshal(rec.Body.Bytes(), &pending) != nil || pending.Status != "needs_approval" || pending.ApprovalID == "" {
		t.Fatalf("add: %d %s", rec.Code, rec.Body)
	}
	if rec := callAs(srv, "secret", http.MethodGet, "/api/settings/mcp/connections/design", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("saved before approval: %d %s", rec.Code, rec.Body)
	}
	if _, err := approvals.Decide(context.Background(), pending.ApprovalID, service.ApprovalDecision{Decision: service.ApprovalAllow, By: "operator"}); err != nil {
		t.Fatal(err)
	}
	rec = callAs(srv, "secret", http.MethodGet, "/api/settings/mcp/connections/design", "")
	var got service.MCPConnectionState
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got.Kind != "stdio" || got.Command != "/usr/bin/node" || got.Env["PATH"] != "/opt/node/bin" {
		t.Fatalf("get: %d %s", rec.Code, rec.Body)
	}
	change := fmt.Sprintf(`{"args":["other.mjs"],"expected_revision":%d}`, got.Revision)
	if rec := callAs(srv, "secret", http.MethodPatch, "/api/settings/mcp/connections/design", change); rec.Code != http.StatusAccepted {
		t.Fatalf("change: %d %s", rec.Code, rec.Body)
	}
}

func TestTheOperatorTestsAConnectionAndTheReeveCannot(t *testing.T) {
	srv, reeve := mcpServer(t)
	tester := &fakeMCPTester{}
	srv.SetMCPTester(tester)
	rec := callAs(srv, "secret", http.MethodPost, "/api/settings/mcp/connections/design/test", "")
	var res service.MCPTestResult
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &res) != nil || len(res.Tools) != 1 || res.Stderr != "starting" {
		t.Fatalf("test: %d %s", rec.Code, rec.Body)
	}
	for _, token := range []string{reeve, "lgi_main"} {
		if rec := callAs(srv, token, http.MethodPost, "/api/settings/mcp/connections/design/test", ""); rec.Code != http.StatusForbidden {
			t.Errorf("test with %.8s: %d %s", token, rec.Code, rec.Body)
		}
	}
	if len(tester.tested) != 1 {
		t.Fatalf("tested %v", tester.tested)
	}
}
