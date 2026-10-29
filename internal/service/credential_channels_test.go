package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/store"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func elevenProxied() config.CredentialConfig {
	return config.CredentialConfig{Name: "eleven", Secret: "env://BROKER_TEST_KEY", Profile: "elevenlabs"}
}

func channelsFixture(t *testing.T, credentials ...config.CredentialConfig) (brokerFixture, *CredentialChannels) {
	t.Helper()
	f := newBrokerFixture(t, credentials...)
	channels := NewCredentialChannels(f.broker, CredentialChannelsOptions{
		ProxyURL:     func() string { return "http://127.0.0.1:7788" },
		SSHSocketDir: filepath.Join(t.TempDir(), "ssh"),
	})
	return f, channels
}

func TestAnAgentSessionGetsAPlaceholderAndTheProxyAddress(t *testing.T) {
	f, channels := channelsFixture(t, elevenProxied())
	ctx := context.Background()
	env, err := channels.SessionEnv(ctx, "task-1", "ex-1")
	if err != nil {
		t.Fatal(err)
	}
	placeholder := env["ELEVENLABS_API_KEY"]
	if !strings.HasPrefix(placeholder, "lgk_") || len(placeholder) < 40 {
		t.Fatalf("placeholder = %q", placeholder)
	}
	if env["ELEVENLABS_BASE_URL"] != "http://127.0.0.1:7788/c/eleven" {
		t.Fatalf("base url = %q", env["ELEVENLABS_BASE_URL"])
	}
	if _, ok := env["SSH_AUTH_SOCK"]; ok {
		t.Fatal("SSH_AUTH_SOCK set with no ssh credential")
	}
	name, execution, ok, err := channels.ResolvePlaceholder(ctx, placeholder)
	if err != nil || !ok || name != "eleven" || execution.ExecutionID != "ex-1" || execution.TaskID != "task-1" {
		t.Fatalf("resolve = %s %+v %v %v", name, execution, ok, err)
	}
	if strings.Contains(env["ELEVENLABS_API_KEY"], "sk-eleven") {
		t.Fatal("the session got the key")
	}
	_ = f
}

func TestNoProxyMeansNoProxyVariables(t *testing.T) {
	f := newBrokerFixture(t, elevenProxied())
	channels := NewCredentialChannels(f.broker, CredentialChannelsOptions{ProxyURL: func() string { return "" }})
	env, err := channels.SessionEnv(context.Background(), "task-1", "ex-1")
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range env {
		if strings.HasPrefix(name, "ELEVENLABS_") || value != "" {
			t.Fatalf("env = %v; want only emptied variables", env)
		}
	}
}

func TestAnAPIKeyCredentialGetsVariablesNamedAfterIt(t *testing.T) {
	_, channels := channelsFixture(t, config.CredentialConfig{Name: "open-ai", Secret: "env://BROKER_TEST_KEY", Profile: "api-key",
		Settings: config.CredentialSettings{Upstream: "https://api.openai.com/v1", Key: config.CredentialKeyPlace{Header: "Authorization", Format: "Bearer {secret}"}}})
	env, _ := channels.SessionEnv(context.Background(), "task-1", "ex-1")
	if !strings.HasPrefix(env["OPEN_AI_API_KEY"], "lgk_") || env["OPEN_AI_BASE_URL"] != "http://127.0.0.1:7788/c/open-ai" {
		t.Fatalf("env = %v", env)
	}
	proxy := channels.Proxy()
	c := proxy.Credentials["open-ai"]
	if c.Upstream.String() != "https://api.openai.com/v1" || c.Key.Format != "Bearer {secret}" || c.Inspector.Host != "api.openai.com" {
		t.Fatalf("proxy credential = %+v", c)
	}
}

func TestAnAPIKeyCredentialWithNoUpstreamIsRefused(t *testing.T) {
	approvals, db, _ := newApprovalFixture(t)
	_, err := NewCredentialBroker(db, approvals, CredentialBrokerOptions{Credentials: []config.CredentialConfig{{Name: "x", Secret: "env://A", Profile: "api-key"}}})
	if err == nil || !strings.Contains(err.Error(), `"x"`) {
		t.Fatalf("err = %v", err)
	}
}

// proxyAgainst serves the channels' proxy with eleven's upstream moved to a
// local TLS server.
func proxyAgainst(t *testing.T, channels *CredentialChannels, upstream *httptest.Server) *httptest.Server {
	t.Helper()
	p := channels.Proxy()
	u, _ := url.Parse(upstream.URL)
	c := p.Credentials["eleven"]
	c.Upstream = u
	p.Credentials["eleven"] = c
	p.Transport = upstream.Client().Transport
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	return srv
}

func elevenUpstream(t *testing.T, calls *int) *httptest.Server {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		if r.Header.Get("xi-api-key") != "sk-eleven" {
			w.WriteHeader(401)
			return
		}
		_, _ = io.WriteString(w, "audio-bytes")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func post(t *testing.T, rawURL, key, body string) (int, map[string]string) {
	t.Helper()
	req, _ := http.NewRequest("POST", rawURL, strings.NewReader(body))
	req.Header.Set("xi-api-key", key)
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

func TestElevenLabsSpeechUnderTheDailyLimitGoesWithoutAsking(t *testing.T) {
	f, channels := channelsFixture(t, elevenProxied())
	ctx := context.Background()
	env, _ := channels.SessionEnv(ctx, "task-1", "ex-1")
	calls := 0
	proxy := proxyAgainst(t, channels, elevenUpstream(t, &calls))

	f.broker.RecordMeasured(ctx, "eleven", map[string]int64{"characters": 1000})
	status, _ := post(t, proxy.URL+"/c/eleven/v1/text-to-speech/abc", env["ELEVENLABS_API_KEY"], `{"text":"`+strings.Repeat("a", 500)+`"}`)
	if status != 200 || calls != 1 {
		t.Fatalf("status %d calls %d", status, calls)
	}
	pending, _ := f.approvals.List(ctx, store.ApprovalFilter{Status: store.ApprovalPending})
	if len(pending) != 0 {
		t.Fatalf("asked the person: %+v", pending)
	}
	total, _ := f.store.CredentialMeterTotal(ctx, "eleven", "characters", time.Now().Add(-time.Hour))
	if total != 1500 {
		t.Fatalf("characters today = %d", total)
	}
	bytes, _ := f.store.CredentialMeterTotal(ctx, "eleven", "bytes", time.Now().Add(-time.Hour))
	if bytes != int64(len(`{"text":"`)+500+len(`"}`)+len("audio-bytes")) {
		t.Fatalf("bytes = %d", bytes)
	}

	uses, _ := f.store.ListCredentialAudit(ctx, store.CredentialAuditFilter{Event: "use"})
	u := uses[0]
	if u.Operation != "tts.speak" || u.Resource != "api.elevenlabs.io/voice/abc" || !strings.Contains(u.Detail, "POST /v1/text-to-speech/abc") || !strings.Contains(u.Amount, `"characters":500`) {
		t.Fatalf("audit use = %+v", u)
	}
	if bad, err := f.store.VerifyCredentialAudit(ctx); err != nil || bad != 0 {
		t.Fatalf("audit chain breaks at %d, %v", bad, err)
	}
}

func TestElevenLabsKeyManagementIsRefused(t *testing.T) {
	_, channels := channelsFixture(t, elevenProxied())
	env, _ := channels.SessionEnv(context.Background(), "task-1", "ex-1")
	calls := 0
	proxy := proxyAgainst(t, channels, elevenUpstream(t, &calls))
	status, body := post(t, proxy.URL+"/c/eleven/v1/api-keys/abc", env["ELEVENLABS_API_KEY"], "")
	if status != 403 || body["operation"] != "keys.manage" || calls != 0 {
		t.Fatalf("status %d body %v calls %d", status, body, calls)
	}
}

func TestSpeechPastTheDailyLimitWaitsForThePerson(t *testing.T) {
	f, channels := channelsFixture(t, elevenProxied())
	ctx := context.Background()
	env, _ := channels.SessionEnv(ctx, "task-1", "ex-1")
	calls := 0
	proxy := proxyAgainst(t, channels, elevenUpstream(t, &calls))

	f.broker.RecordMeasured(ctx, "eleven", map[string]int64{"characters": 19990})
	status, body := post(t, proxy.URL+"/c/eleven/v1/text-to-speech/abc", env["ELEVENLABS_API_KEY"], `{"text":"more than ten characters"}`)
	if status != 403 || body["status"] != "pending" || !strings.HasPrefix(body["approval"], "ap-") || calls != 0 {
		t.Fatalf("status %d body %v calls %d", status, body, calls)
	}
	approval, _ := f.approvals.Get(ctx, body["approval"])
	if approval.Status != store.ApprovalPending || approval.Options[0].ID != "window" {
		t.Fatalf("approval = %+v", approval)
	}

	// The person allows it for the hour; the agent is told, and speaks.
	if _, err := f.approvals.Decide(ctx, approval.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "window", By: "web"}); err != nil {
		t.Fatal(err)
	}
	if msgs := f.messenger.all(); len(msgs) != 1 || !strings.Contains(msgs[0], "[legato approvals] "+approval.ID+" approved") {
		t.Fatalf("messages = %v", msgs)
	}
	status, _ = post(t, proxy.URL+"/c/eleven/v1/text-to-speech/abc", env["ELEVENLABS_API_KEY"], `{"text":"more than ten characters"}`)
	if status != 200 || calls != 1 {
		t.Fatalf("after approval: status %d calls %d", status, calls)
	}
}

func TestAnEndedSessionsPlaceholderStopsWorking(t *testing.T) {
	f, channels := channelsFixture(t, elevenProxied())
	ctx := context.Background()
	env, _ := channels.SessionEnv(ctx, "task-1", "ex-1")
	calls := 0
	proxy := proxyAgainst(t, channels, elevenUpstream(t, &calls))
	_ = f.store.TerminalizeExecution(ctx, "ex-1", store.ExecutionSucceeded)
	status, _ := post(t, proxy.URL+"/c/eleven/v1/text-to-speech/abc", env["ELEVENLABS_API_KEY"], `{"text":"hi"}`)
	if status != 401 || calls != 0 {
		t.Fatalf("status %d calls %d", status, calls)
	}
}

func sshKeyPEM(t *testing.T) (string, ssh.PublicKey) {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	signer, _ := ssh.NewSignerFromKey(priv)
	return string(pem.EncodeToMemory(block)), signer.PublicKey()
}

func sshCredential() config.CredentialConfig {
	return config.CredentialConfig{Name: "homelab", Secret: "env://BROKER_SSH_KEY", Profile: "ssh", Type: credential.TypeKeyPair}
}

func TestAnSSHCredentialGivesTheSessionItsOwnSocket(t *testing.T) {
	keyPEM, public := sshKeyPEM(t)
	t.Setenv("BROKER_SSH_KEY", keyPEM)
	_, channels := channelsFixture(t, sshCredential())
	ctx := context.Background()
	env, err := channels.SessionEnv(ctx, "task-1", "ex-1")
	if err != nil {
		t.Fatal(err)
	}
	if env["SSH_AUTH_SOCK"] != channels.SSHSocketPath("ex-1") || !strings.HasSuffix(env["SSH_AUTH_SOCK"], "/ex-1.sock") {
		t.Fatalf("SSH_AUTH_SOCK = %q", env["SSH_AUTH_SOCK"])
	}
	keys, err := channels.Keys(ctx)
	if err != nil || len(keys) != 1 || keys[0].Credential != "homelab" || string(keys[0].Public.Marshal()) != string(public.Marshal()) {
		t.Fatalf("keys = %+v, %v", keys, err)
	}
}

func TestAHeldSignRequestIsDecidedLaterAndTheNextLoginSigns(t *testing.T) {
	keyPEM, public := sshKeyPEM(t)
	t.Setenv("BROKER_SSH_KEY", keyPEM)
	f, channels := channelsFixture(t, sshCredential())
	ctx := context.Background()
	closer, err := channels.SSHServer().Listen(channels.SSHSocketPath("ex-1"), credential.Execution{TaskID: "task-1", ExecutionID: "ex-1"})
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()
	conn, err := net.Dial("unix", channels.SSHSocketPath("ex-1"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := agent.NewClient(conn)

	_, hostPriv, _ := ed25519.GenerateKey(rand.Reader)
	host, _ := ssh.NewSignerFromKey(hostPriv)
	sid := []byte("session")
	sig, _ := host.Sign(rand.Reader, sid)
	bind := ssh.Marshal(struct {
		HostKey, SessionID, Signature []byte
		Forwarding                    bool
	}{host.PublicKey().Marshal(), sid, ssh.Marshal(sig), false})
	if _, err := client.Extension(credential.SessionBindExtension, bind); err != nil {
		t.Fatal(err)
	}
	login := ssh.Marshal(struct {
		SessionID             []byte
		Type                  byte
		User, Service, Method string
		Sign                  bool
		Algorithm             string
		Key                   []byte
	}{sid, 50, "cameron", "ssh-connection", "publickey", true, public.Type(), public.Marshal()})

	// Nobody answers within the wait: the agent gets a failure.
	if _, err := client.Sign(public, login); err == nil {
		t.Fatal("signed with no decision")
	}
	pending, _ := f.approvals.List(ctx, store.ApprovalFilter{Status: store.ApprovalPending})
	if len(pending) != 1 || pending[0].Options[0].ID != "window" || !strings.Contains(pending[0].Summary, "ssh://cameron@key:SHA256:") {
		t.Fatalf("pending = %+v", pending)
	}
	if _, err := f.approvals.Decide(ctx, pending[0].ID, ApprovalDecision{Decision: ApprovalAllow, Option: "window", By: "web"}); err != nil {
		t.Fatal(err)
	}
	if msgs := f.messenger.all(); len(msgs) != 1 || !strings.Contains(msgs[0], "[legato approvals] "+pending[0].ID+" approved") {
		t.Fatalf("messages = %v", msgs)
	}
	signature, err := client.Sign(public, login)
	if err != nil {
		t.Fatalf("second login: %v", err)
	}
	if err := public.Verify(login, signature); err != nil {
		t.Fatal(err)
	}
}

type fixedEnv map[string]string

func (f fixedEnv) SessionEnv(context.Context, string, string) (map[string]string, error) {
	return f, nil
}

func TestASpawnedSessionGetsTheChannelsEnvironment(t *testing.T) {
	_, db, _ := newApprovalFixture(t)
	createTask(t, db, "task-1")
	tmux := newMockTmux()
	agents := NewAgentService(db, tmux, t.TempDir())
	agents.(interface{ SetSessionEnv(SessionEnvProvider) }).SetSessionEnv(fixedEnv{"SSH_AUTH_SOCK": "/run/x/legato/ssh/ex.sock", "ELEVENLABS_API_KEY": "lgk_x"})
	if err := agents.SpawnAgent(context.Background(), "task-1", 80, 24); err != nil {
		t.Fatal(err)
	}
	env := tmux.envVars["legato-task-1"]
	if env["SSH_AUTH_SOCK"] != "/run/x/legato/ssh/ex.sock" || env["ELEVENLABS_API_KEY"] != "lgk_x" {
		t.Fatalf("env = %v", env)
	}
}

func TestWithoutChannelsASessionKeepsItsInheritedAgent(t *testing.T) {
	_, db, _ := newApprovalFixture(t)
	createTask(t, db, "task-1")
	tmux := newMockTmux()
	agents := NewAgentService(db, tmux, t.TempDir())
	if err := agents.SpawnAgent(context.Background(), "task-1", 80, 24); err != nil {
		t.Fatal(err)
	}
	if _, ok := tmux.envVars["legato-task-1"]["SSH_AUTH_SOCK"]; ok {
		t.Fatal("SSH_AUTH_SOCK set without channels")
	}
}

func TestAgentSessionsDoNotInheritTheVariablesSecretsComeFrom(t *testing.T) {
	t.Setenv("PROXIED_KEY", "sk-proxied")
	t.Setenv("HELPER_KEY", "sk-helper")
	_, channels := channelsFixture(t,
		config.CredentialConfig{Name: "eleven", Secret: "env://PROXIED_KEY", Profile: "elevenlabs"},
		config.CredentialConfig{Name: "plain", Secret: "env://HELPER_KEY", Profile: "secret"},
	)
	env, err := channels.SessionEnv(context.Background(), "task-1", "ex-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"OP_SERVICE_ACCOUNT_TOKEN", "LEGATO_SECRETS_PASSPHRASE", "PROXIED_KEY"} {
		if value, ok := env[name]; !ok || value != "" {
			t.Errorf("%s = %q, %v; want it set empty", name, value, ok)
		}
	}
	// A helper credential's value is read in the agent's session, by
	// legato credential get; the agent holds it anyway.
	if _, ok := env["HELPER_KEY"]; ok {
		t.Errorf("HELPER_KEY was emptied: legato credential get could not read it")
	}
}

func TestAgentSessionsHideTheTokenWithNoCredentials(t *testing.T) {
	_, channels := channelsFixture(t)
	env, _ := channels.SessionEnv(context.Background(), "task-1", "ex-1")
	if value, ok := env["OP_SERVICE_ACCOUNT_TOKEN"]; !ok || value != "" {
		t.Fatalf("OP_SERVICE_ACCOUNT_TOKEN = %q, %v", value, ok)
	}
}
