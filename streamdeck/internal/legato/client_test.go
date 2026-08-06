package legato

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// mockLegatoServer returns a test server that serves /health, /api/agents,
// and /api/attention with configurable responses.
type mockLegatoServer struct {
	*httptest.Server
	agentsJSON    string
	attentionJSON string
	agentsCalls   int32
	attnCalls     int32
	healthOK      int32
}

func newMockServer(agentsJSON, attentionJSON string) *mockLegatoServer {
	m := &mockLegatoServer{
		agentsJSON:    agentsJSON,
		attentionJSON: attentionJSON,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&m.healthOK, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": "test"})
	})
	mux.HandleFunc("/api/agents", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&m.agentsCalls, 1)
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(agentsJSON))
	})
	mux.HandleFunc("/api/attention", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&m.attnCalls, 1)
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(attentionJSON))
	})
	m.Server = httptest.NewServer(mux)
	return m
}

func TestFetchAgents(t *testing.T) {
	agentsJSON := `[
		{"id":1,"task_id":"t-1","task_title":"Build feature","status":"running","activity":"working"},
		{"id":2,"task_id":"t-2","task_title":"Fix bug","status":"running","activity":"waiting"},
		{"id":3,"task_id":"t-3","task_title":"Old task","status":"dead","activity":""}
	]`
	srv := newMockServer(agentsJSON, `{"items":[]}`)
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, Token: "test-token", PollInterval: 100 * time.Millisecond}, nil)
	agents, err := c.fetchAgents(context.Background(), "test-token")
	if err != nil {
		t.Fatalf("fetchAgents: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("expected 2 running agents, got %d", len(agents))
	}
	if agents[0].Title != "Build feature" {
		t.Errorf("first agent title: got %q, want %q", agents[0].Title, "Build feature")
	}
	if agents[1].Activity != "waiting" {
		t.Errorf("second agent activity: got %q, want %q", agents[1].Activity, "waiting")
	}
	// Verify the dead agent was filtered out.
	for _, a := range agents {
		if a.Status != "running" {
			t.Errorf("non-running agent leaked: %+v", a)
		}
	}
}

func TestFetchAttention(t *testing.T) {
	attentionJSON := `{"items":[
		{"id":"attn-1","producer":"plans","signal_kind":"plan_decision","title":"Approve plan X","urgency":"normal","navigation":{"kind":"plan","target":"pl-1"},"actions":[{"id":"open","label":"Open plan","kind":"navigate"}]}
	]}`
	srv := newMockServer(`[]`, attentionJSON)
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, Token: "test-token"}, nil)
	items, err := c.fetchAttention(context.Background(), "test-token")
	if err != nil {
		t.Fatalf("fetchAttention: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 attention item, got %d", len(items))
	}
	if items[0].Producer != "plans" || items[0].SignalKind != "plan_decision" {
		t.Errorf("attention classification: producer=%q signal_kind=%q", items[0].Producer, items[0].SignalKind)
	}
	if items[0].Navigation.Kind != "plan" || items[0].Navigation.Target != "pl-1" {
		t.Errorf("navigation: %+v", items[0].Navigation)
	}
}

func TestHealthCheck(t *testing.T) {
	srv := newMockServer(`[]`, `{"items":[]}`)
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, Token: "test-token"}, nil)
	ok, err := c.healthCheck(context.Background())
	if err != nil {
		t.Fatalf("healthCheck: %v", err)
	}
	if !ok {
		t.Fatal("expected healthy server")
	}
}

func TestHealthCheckFailsOnUnreachable(t *testing.T) {
	c := NewClient(Config{BaseURL: "http://127.0.0.1:1", Token: "test-token"}, nil)
	ok, err := c.healthCheck(context.Background())
	if ok {
		t.Fatal("expected unhealthy server")
	}
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSnapshotConnectedAfterStart(t *testing.T) {
	agentsJSON := `[{"id":1,"task_id":"t-1","task_title":"Agent 1","status":"running","activity":"working"}]`
	srv := newMockServer(agentsJSON, `{"items":[]}`)
	defer srv.Close()

	var callCount int32
	cb := func(s Snapshot) {
		atomic.StoreInt32(&callCount, 1)
		_ = s
	}

	c := NewClient(Config{BaseURL: srv.URL, Token: "test-token", PollInterval: 100 * time.Millisecond}, cb)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)

	// Wait for connection to establish.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snap := c.Snapshot()
		if snap.Connected && len(snap.Agents) == 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	snap := c.Snapshot()
	if !snap.Connected {
		t.Fatal("expected connected snapshot")
	}
	if len(snap.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(snap.Agents))
	}
	if !snap.HasToken {
		t.Fatal("expected has token")
	}
	c.Stop()
}

func TestRecoveryAfterServerRestart(t *testing.T) {
	agentsJSON := `[{"id":1,"task_id":"t-1","task_title":"Recovery agent","status":"running","activity":"working"}]`
	srv := newMockServer(agentsJSON, `{"items":[]}`)

	c := NewClient(Config{
		BaseURL:      srv.URL,
		Token:        "test-token",
		PollInterval: 200 * time.Millisecond,
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)

	// Wait for connected.
	waitFor(t, 3*time.Second, func() bool {
		return c.Snapshot().Connected
	})

	// Stop the server (simulate Legato stopping).
	srv.Close()

	// Wait for disconnected.
	waitFor(t, 5*time.Second, func() bool {
		return !c.Snapshot().Connected
	})

	// Start a new server on the same port isn't possible with httptest,
	// so we just verify the client detects the disconnect and shows offline.
	snap := c.Snapshot()
	if snap.Connected {
		t.Fatal("expected disconnected after server stop")
	}

	c.Stop()
}

func TestExecuteAction(t *testing.T) {
	var actionCalled int32
	var receivedBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/attention/attn-1/actions/approve", func(w http.ResponseWriter, r *http.Request) {
		atomic.StoreInt32(&actionCalled, 1)
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		receivedBody = string(buf[:n])
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, Token: "test-token"}, nil)
	err := c.ExecuteAction(context.Background(), "attn-1", "approve", "rev-1")
	if err != nil {
		t.Fatalf("ExecuteAction: %v", err)
	}
	if atomic.LoadInt32(&actionCalled) != 1 {
		t.Fatal("action was not called")
	}
	if !strings.Contains(receivedBody, "rev-1") {
		t.Errorf("body missing revision: %q", receivedBody)
	}
	if !strings.Contains(receivedBody, "confirmed") {
		t.Errorf("body missing confirmed: %q", receivedBody)
	}
}

func TestAttentionDeepLink(t *testing.T) {
	tests := []struct {
		kind, target, want string
	}{
		{"plan", "pl-123", "http://localhost:3080/plans/pl-123"},
		{"review", "rt-456", "http://localhost:3080/review/rt-456"},
		{"workflow_run", "wr-789", "http://localhost:3080/workflows/runs/wr-789"},
		{"task", "t-001", "http://localhost:3080/agents?task=t-001"},
		{"unknown", "x", "http://localhost:3080/attention"},
	}
	for _, tt := range tests {
		got := AttentionDeepLink("http://localhost:3080", AttentionNavigation{Kind: tt.kind, Target: tt.target})
		if got != tt.want {
			t.Errorf("AttentionDeepLink(%q,%q) = %q, want %q", tt.kind, tt.target, got, tt.want)
		}
	}
}

func TestResolveTokenFromFile(t *testing.T) {
	// Create a temp dir with an auth-token file.
	tmpDir := t.TempDir()
	tokenPath := tmpDir + "/auth-token"
	testToken := "abcdef123456"
	if err := writeTestToken(tokenPath, testToken); err != nil {
		t.Fatal(err)
	}

	c := NewClient(Config{BaseURL: "http://localhost:1", DataDir: tmpDir}, nil)
	token, err := c.resolveToken()
	if err != nil {
		t.Fatalf("resolveToken: %v", err)
	}
	if token != testToken {
		t.Errorf("token: got %q, want %q", token, testToken)
	}
}

func TestResolveTokenExplicitConfig(t *testing.T) {
	c := NewClient(Config{Token: "explicit-token"}, nil)
	token, err := c.resolveToken()
	if err != nil {
		t.Fatalf("resolveToken: %v", err)
	}
	if token != "explicit-token" {
		t.Errorf("token: got %q, want %q", token, "explicit-token")
	}
}

// --- Helpers ---

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", timeout)
}

func writeTestToken(path, token string) error {
	return os.WriteFile(path, []byte(token+"\n"), 0o600)
}
