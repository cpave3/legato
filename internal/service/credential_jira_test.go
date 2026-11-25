package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

const jiraToken = "atl-REAL-TOKEN"

func jiraCredential(settings config.CredentialSettings) config.CredentialConfig {
	return config.CredentialConfig{Name: "jira", Secret: "env://JIRA_TOKEN_SRC", Profile: JiraProfile, Settings: settings}
}

// jiraFixture is a broker with one jira credential, its token in
// JIRA_TOKEN_SRC, and the Jira integration's settings as jira gives them.
func jiraFixture(t *testing.T, jira JiraDefaults, c config.CredentialConfig) (brokerFixture, *CredentialChannels) {
	t.Helper()
	t.Setenv("JIRA_TOKEN_SRC", jiraToken)
	f := newBrokerFixtureWith(t, CredentialBrokerOptions{Jira: jira}, c)
	channels := NewCredentialChannels(f.broker, CredentialChannelsOptions{
		ProxyURL:     func() string { return "http://127.0.0.1:7788" },
		SSHSocketDir: filepath.Join(t.TempDir(), "ssh"),
	})
	return f, channels
}

// fakeJira records what reaches it and answers 401 unless it is signed as
// user with the real token.
type fakeJira struct {
	mu    sync.Mutex
	seen  []*http.Request
	user  string
	calls int
}

func (j *fakeJira) serve(t *testing.T) *httptest.Server {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		j.mu.Lock()
		j.calls++
		j.seen = append(j.seen, r.Clone(context.Background()))
		j.mu.Unlock()
		if user, password, ok := r.BasicAuth(); !ok || user != j.user || password != jiraToken {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"issues":[]}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// proxyFor serves the channels' proxy with a credential's upstream moved to
// a local TLS server.
func proxyFor(t *testing.T, channels *CredentialChannels, name string, upstream *httptest.Server) *httptest.Server {
	t.Helper()
	p := channels.Proxy()
	u, _ := url.Parse(upstream.URL)
	c := p.Credentials[name]
	c.Upstream = u
	p.Credentials[name] = c
	p.Transport = upstream.Client().Transport
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	return srv
}

// asAgent sends a request as an agent's curl -u "$JIRA_EMAIL:$JIRA_API_TOKEN" does.
func asAgent(t *testing.T, env map[string]string, method, rawURL, body string) (int, map[string]string) {
	t.Helper()
	req, _ := http.NewRequest(method, rawURL, strings.NewReader(body))
	req.SetBasicAuth(env["JIRA_EMAIL"], env["JIRA_API_TOKEN"])
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var out map[string]string
	_ = json.Unmarshal(data, &out)
	return resp.StatusCode, out
}

func TestAJiraSearchGoesThroughTheProxySignedWithTheRealToken(t *testing.T) {
	f, channels := jiraFixture(t, JiraDefaults{}, jiraCredential(config.CredentialSettings{Upstream: "https://acme.atlassian.net", Username: "bot@example.com"}))
	ctx := context.Background()
	env, err := channels.SessionEnv(ctx, "task-1", "ex-1")
	if err != nil {
		t.Fatal(err)
	}
	jira := &fakeJira{user: "bot@example.com"}
	proxy := proxyFor(t, channels, "jira", jira.serve(t))

	status, body := asAgent(t, env, "GET", proxy.URL+"/c/jira/rest/api/3/search/jql?jql=project%3DAG&fields=summary", "")
	if status != 200 || jira.calls != 1 {
		t.Fatalf("status %d body %v calls %d", status, body, jira.calls)
	}
	seen := jira.seen[0]
	if seen.URL.Path != "/rest/api/3/search/jql" || seen.URL.Query().Get("jql") != "project=AG" {
		t.Fatalf("upstream saw %s?%s", seen.URL.Path, seen.URL.RawQuery)
	}
	if want := "Basic " + base64.StdEncoding.EncodeToString([]byte("bot@example.com:"+jiraToken)); seen.Header.Get("Authorization") != want {
		t.Fatalf("Authorization = %q", seen.Header.Get("Authorization"))
	}
	pending, _ := f.approvals.List(ctx, store.ApprovalFilter{Status: store.ApprovalPending})
	if len(pending) != 0 {
		t.Fatalf("asked the operator: %+v", pending)
	}
	proxy.Close()
	uses, _ := f.store.ListCredentialAudit(ctx, store.CredentialAuditFilter{Event: "use"})
	if len(uses) != 1 || uses[0].Operation != "search" {
		t.Fatalf("audit uses = %+v", uses)
	}
}

func TestAJiraIssueReadGoesWithoutAsking(t *testing.T) {
	f, channels := jiraFixture(t, JiraDefaults{}, jiraCredential(config.CredentialSettings{Upstream: "https://acme.atlassian.net", Username: "bot@example.com"}))
	ctx := context.Background()
	env, _ := channels.SessionEnv(ctx, "task-1", "ex-1")
	jira := &fakeJira{user: "bot@example.com"}
	proxy := proxyFor(t, channels, "jira", jira.serve(t))

	status, body := asAgent(t, env, "GET", proxy.URL+"/c/jira/rest/api/3/issue/AG-12?fields=summary,comment", "")
	proxy.Close()
	if status != 200 || jira.calls != 1 {
		t.Fatalf("status %d body %v calls %d", status, body, jira.calls)
	}
	uses, _ := f.store.ListCredentialAudit(ctx, store.CredentialAuditFilter{Event: "use"})
	if len(uses) != 1 || uses[0].Operation != "read" || uses[0].Resource != "acme.atlassian.net/issue/AG-12" {
		t.Fatalf("audit uses = %+v", uses)
	}
}

func TestAJiraCreateIsHeldForTheOperator(t *testing.T) {
	f, channels := jiraFixture(t, JiraDefaults{}, jiraCredential(config.CredentialSettings{Upstream: "https://acme.atlassian.net", Username: "bot@example.com"}))
	ctx := context.Background()
	env, _ := channels.SessionEnv(ctx, "task-1", "ex-1")
	jira := &fakeJira{user: "bot@example.com"}
	proxy := proxyFor(t, channels, "jira", jira.serve(t))

	create := `{"fields":{"project":{"key":"AG"},"summary":"Checkout fails","issuetype":{"name":"Bug"}}}`
	status, body := asAgent(t, env, "POST", proxy.URL+"/c/jira/rest/api/3/issue", create)
	if status != 403 || body["status"] != "pending" || !strings.HasPrefix(body["approval"], "ap-") || jira.calls != 0 {
		t.Fatalf("status %d body %v calls %d", status, body, jira.calls)
	}
	approval, err := f.approvals.Get(ctx, body["approval"])
	if err != nil || approval.Status != store.ApprovalPending || !strings.Contains(string(approval.Details), "acme.atlassian.net/project/AG") {
		t.Fatalf("approval = %+v, %v", approval, err)
	}
}

func TestTheJiraTokenNeverReachesTheSandbox(t *testing.T) {
	_, channels := jiraFixture(t, JiraDefaults{}, jiraCredential(config.CredentialSettings{Upstream: "https://acme.atlassian.net", Username: "bot@example.com"}))
	env, err := channels.SessionEnv(context.Background(), "task-1", "ex-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(env["JIRA_API_TOKEN"], "lgk_") || env["JIRA_BASE_URL"] != "http://127.0.0.1:7788/c/jira" || env["JIRA_EMAIL"] != "bot@example.com" {
		t.Fatalf("env = %v", env)
	}
	for name, value := range env {
		if strings.Contains(value, jiraToken) || strings.Contains(value, base64.StdEncoding.EncodeToString([]byte("bot@example.com:"+jiraToken))) {
			t.Fatalf("%s carries the token", name)
		}
	}
	// The variable the token is read from is emptied in the session.
	if v, ok := env["JIRA_TOKEN_SRC"]; !ok || v != "" {
		t.Fatalf("JIRA_TOKEN_SRC = %q, %v; want it set empty", v, ok)
	}
}

func TestAJiraCredentialTakesItsSiteAndEmailFromTheJiraIntegration(t *testing.T) {
	_, channels := jiraFixture(t, JiraDefaults{BaseURL: "https://acme.atlassian.net/", Email: "me@acme.com"}, jiraCredential(config.CredentialSettings{}))
	env, _ := channels.SessionEnv(context.Background(), "task-1", "ex-1")
	if env["JIRA_EMAIL"] != "me@acme.com" {
		t.Fatalf("JIRA_EMAIL = %q", env["JIRA_EMAIL"])
	}
	c := channels.Proxy().Credentials["jira"]
	if c.Upstream.String() != "https://acme.atlassian.net" || c.Username != "me@acme.com" || c.Inspector.Host != "acme.atlassian.net" {
		t.Fatalf("proxy credential = %+v", c)
	}

	// The credential's own settings win.
	_, channels = jiraFixture(t, JiraDefaults{BaseURL: "https://acme.atlassian.net", Email: "me@acme.com"},
		jiraCredential(config.CredentialSettings{Upstream: "https://other.atlassian.net", Username: "bot@other.com"}))
	c = channels.Proxy().Credentials["jira"]
	if c.Upstream.Host != "other.atlassian.net" || c.Username != "bot@other.com" {
		t.Fatalf("proxy credential = %+v", c)
	}
}

func TestAJiraCredentialWithNoSiteOrEmailIsRefused(t *testing.T) {
	approvals, db, _ := newApprovalFixture(t)
	for want, jira := range map[string]JiraDefaults{
		"jira.base_url": {Email: "me@acme.com"},
		"jira.email":    {BaseURL: "https://acme.atlassian.net"},
	} {
		_, err := NewCredentialBroker(db, approvals, CredentialBrokerOptions{Jira: jira, Credentials: []config.CredentialConfig{jiraCredential(config.CredentialSettings{})}})
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), `"jira"`) {
			t.Errorf("without %s: err = %v", want, err)
		}
	}
}

func TestDescribingAJiraCredentialNamesItsVariables(t *testing.T) {
	summaries, _, err := DescribeCredentials([]config.CredentialConfig{jiraCredential(config.CredentialSettings{})}, nil, nil, JiraDefaults{BaseURL: "https://acme.atlassian.net", Email: "me@acme.com"})
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || strings.Join(summaries[0].Env, ",") != "JIRA_API_TOKEN,JIRA_BASE_URL,JIRA_EMAIL" {
		t.Fatalf("summaries = %+v", summaries)
	}
}

// A jira credential saved in the settings list, as PUT
// /api/config/lists/credentials/jira saves it, names only the token the Jira
// integration already uses and signs in to that integration's site.
func TestAJiraCredentialSavedInTheSettingsListUsesTheJiraIntegration(t *testing.T) {
	settings, db := newSettingsFixture(t, "")
	ctx := context.Background()
	for key, value := range map[string]string{"jira.base_url": `"https://acme.atlassian.net"`, "jira.email": `"me@acme.com"`, "jira.api_token": `"legato://jira-api-token"`} {
		if err := settings.Set(ctx, key, json.RawMessage(value), "web"); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
	if err := settings.SetEntry(ctx, "credentials", "jira", json.RawMessage(`{"profile": "jira", "secret": "legato://jira-api-token"}`), "web"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load()
	resolve := func(context.Context, string) (string, error) { return jiraToken, nil }
	if err := ApplySettings(ctx, db, cfg, resolve); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Credentials) != 1 || cfg.Credentials[0].Secret != "legato://jira-api-token" {
		t.Fatalf("credentials = %+v", cfg.Credentials)
	}
	broker, err := NewCredentialBroker(db, nil, CredentialBrokerOptions{Credentials: cfg.Credentials, Jira: JiraDefaults{BaseURL: cfg.Jira.BaseURL, Email: cfg.Jira.Email}})
	if err != nil {
		t.Fatal(err)
	}
	c := NewCredentialChannels(broker, CredentialChannelsOptions{ProxyURL: func() string { return "http://127.0.0.1:7788" }}).Proxy().Credentials["jira"]
	if c.Upstream.String() != "https://acme.atlassian.net" || c.Username != "me@acme.com" {
		t.Fatalf("proxy credential = %+v", c)
	}
}

// legato credential get and exec, and the broker's credential.value, read a
// credential's value through Value. A credential that reaches agents only
// through the proxy (or the SSH agent) has no value to give, whatever the
// operation and whatever its rules allow.
func TestTheValueOfAProxiedCredentialIsNeverGivenToAnAgent(t *testing.T) {
	f, _ := jiraFixture(t, JiraDefaults{}, jiraCredential(config.CredentialSettings{Upstream: "https://acme.atlassian.net", Username: "bot@example.com"}))
	ctx := context.Background()
	for _, operation := range []string{"read", "search", "create"} {
		secret, decision, err := f.broker.Value(ctx, CredentialUse{TaskID: "task-1", ExecutionID: "ex-1", Credential: "jira", Operation: operation})
		if err == nil || decision.Allowed || len(secret.Value) != 0 {
			t.Fatalf("%s: secret %q, decision %+v, err %v; want it refused", operation, secret.Value, decision, err)
		}
	}
	refused, _ := f.store.ListCredentialAudit(ctx, store.CredentialAuditFilter{Event: "refuse"})
	if len(refused) != 3 {
		t.Fatalf("audit refusals = %+v", refused)
	}
	pending, _ := f.approvals.List(ctx, store.ApprovalFilter{Status: store.ApprovalPending})
	if len(pending) != 0 {
		t.Fatalf("asked the operator: %+v", pending)
	}

	eleven, _ := channelsFixture(t, elevenProxied())
	if _, decision, err := eleven.broker.Value(ctx, CredentialUse{TaskID: "task-1", ExecutionID: "ex-1", Credential: "eleven", Operation: "voices.read"}); err == nil || decision.Allowed {
		t.Fatalf("elevenlabs: decision %+v, err %v; want it refused", decision, err)
	}
}
