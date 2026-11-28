package cli_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/broker"
	"github.com/cpave3/legato/internal/service"
)

func TestHostBrokerKeepsClaudeControlCredentialsOutOfRelay(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	execID, token := brokerSession(t, s, "host-claude")
	// brokerSession normally seeds a generic shell; the bridge must refuse it.
	h := cli.NewBrokerHandler(s, cli.BrokerDeps{})
	if _, err := call(h, token, "claude.bridge.poll", map[string]any{"relay_id": "relay-1"}); code(err) != "forbidden" {
		t.Fatalf("non-Claude caller: %v", err)
	}
	_ = execID
}

func TestHostOwnedClaudeRelayAnnouncesOnlyPublicEndpoint(t *testing.T) {
	s := newTestStore(t)
	seedRunningClaudeAgent(t, s, "host-claude")
	session, _ := s.GetAgentSessionByTaskID(context.Background(), "host-claude")
	linkNativeTestExecution(t, s, session)
	token := "lgb_claude_relay"
	if err := s.SetExecutionBrokerToken(context.Background(), "native-host-claude", service.HashBrokerToken(token)); err != nil {
		t.Fatal(err)
	}
	h := cli.NewBrokerHandler(s, cli.BrokerDeps{ClaudeRuntimeDir: t.TempDir()})
	raw, err := call(h, token, "claude.bridge.poll", map[string]any{"relay_id": "relay-1"})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(raw)
	var out struct {
		Generation string `json:"generation"`
		Commands   []struct {
			Line string `json:"line"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Commands) != 1 {
		t.Fatalf("commands=%s", payload)
	}
	var ready struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(out.Commands[0].Line), &ready); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(ready.URL)
	if err != nil || u.User != nil {
		t.Fatalf("ready contains credentials")
	}
	session, _ = s.GetAgentSessionByTaskID(context.Background(), "host-claude")
	if session.ChimeraEndpoint == nil {
		t.Fatal("missing host binding")
	}
	control, _ := url.Parse(*session.ChimeraEndpoint)
	password, _ := control.User.Password()
	if password == "" || strings.Contains(string(payload), password) {
		t.Fatal("host key missing or leaked")
	}
	resp, err := http.Get(ready.URL + "/v1/instance")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("public control=%d", resp.StatusCode)
	}
	incidents, err := s.ListNativeSecurityIncidents(context.Background())
	if err != nil || len(incidents) != 1 || incidents[0].ExecutionID != "native-host-claude" {
		t.Fatalf("refusal incident=%v %v", incidents, err)
	}
	publicSession, err := call(h, token, "endpoint.session", nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(publicSession)
	if strings.Contains(string(encoded), password) {
		t.Fatal("endpoint.session leaked key")
	}
	if _, err := call(h, token, "endpoint.bind", map[string]string{"endpoint": "http://127.0.0.1:12345/evil"}); code(err) != "forbidden" {
		t.Fatalf("agent replaced authority: %v", err)
	}
	_, _ = h(context.Background(), broker.Request{Token: token, Op: "claude.bridge.close", Args: json.RawMessage(`{"relay_id":"relay-1"}`)})
}
