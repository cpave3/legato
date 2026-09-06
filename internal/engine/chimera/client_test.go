package chimera

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestModelSettingsContractRoundTrip(t *testing.T) {
	data, err := os.ReadFile("../../../contracts/model-settings-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		State struct {
			Capabilities json.RawMessage `json:"capabilities"`
			Defaults     json.RawMessage `json:"defaults"`
		} `json:"state"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(ModelEntry{Ref: "manticore/sol", Capabilities: fixture.State.Capabilities, Defaults: fixture.State.Defaults})
	if err != nil {
		t.Fatal(err)
	}
	var decoded ModelEntry
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(decoded.Capabilities), `"ultra"`) || !strings.Contains(string(decoded.Capabilities), `"max"`) {
		t.Fatalf("lost model effort values: %s", decoded.Capabilities)
	}
	if !strings.Contains(string(decoded.Defaults), `"medium"`) {
		t.Fatalf("lost model defaults: %s", decoded.Defaults)
	}
}

func TestGetInstance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/instance" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"pid":12345,"cwd":"/tmp","version":"0.1.0","sandboxMode":"off"}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	info, err := c.GetInstance(context.Background())
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	if info.PID != 12345 {
		t.Fatalf("PID = %d, want 12345", info.PID)
	}
}

func TestGetSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sessions/sess-1" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"sess-1","cwd":"/tmp","mode":"build","status":"idle"}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	snap, err := c.GetSession(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if snap.ID != "sess-1" {
		t.Fatalf("ID = %q, want sess-1", snap.ID)
	}
	if snap.Mode != "build" {
		t.Fatalf("Mode = %q, want build", snap.Mode)
	}
}

func TestGetSessionDecodesPendingPermissionsAndFieldPresence(t *testing.T) {
	tests := []struct {
		name            string
		body            string
		wantPresent     bool
		wantPermissions int
	}{
		{
			name:        "supported snapshot",
			body:        `{"id":"sess-1","pendingPermissions":[{"requestId":"req-1","tool":"bash","target":"host","command":"rm -rf cache","reason":"Runs on host","cwd":"/tmp","policyAnalysis":{"version":1,"outcome":"approval_required","stages":[],"findings":[]}}]}`,
			wantPresent: true, wantPermissions: 1,
		},
		{name: "supported empty snapshot", body: `{"id":"sess-1","pendingPermissions":[]}`, wantPresent: true},
		{name: "older snapshot", body: `{"id":"sess-1"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, tt.body)
			}))
			defer srv.Close()

			snapshot, err := NewClient(srv.URL).GetSession(context.Background(), "sess-1")
			if err != nil {
				t.Fatalf("GetSession: %v", err)
			}
			if (snapshot.PendingPermissions != nil) != tt.wantPresent {
				t.Fatalf("PendingPermissions presence = %t, want %t", snapshot.PendingPermissions != nil, tt.wantPresent)
			}
			if snapshot.PendingPermissions == nil {
				return
			}
			if len(*snapshot.PendingPermissions) != tt.wantPermissions {
				t.Fatalf("PendingPermissions = %+v, want %d entries", *snapshot.PendingPermissions, tt.wantPermissions)
			}
			if tt.wantPermissions > 0 {
				permission := (*snapshot.PendingPermissions)[0]
				if permission.RequestID != "req-1" || permission.Command != "rm -rf cache" || permission.Reason != "Runs on host" {
					t.Fatalf("permission = %+v", permission)
				}
				if !strings.Contains(string(permission.PolicyAnalysis), `"outcome":"approval_required"`) {
					t.Fatalf("policyAnalysis = %s", permission.PolicyAnalysis)
				}
			}
		})
	}
}

func TestListSubagents(t *testing.T) {
	var requestURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestURI = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"subagentId":"sub-1","sessionId":"child-1","purpose":"Review tests","url":"http://example.test","status":"running"},{"subagentId":"sub-2","sessionId":"child-2","purpose":"Check docs","status":"idle"}]`)
	}))
	defer srv.Close()

	subagents, err := NewClient(srv.URL).ListSubagents(context.Background(), "parent/one")
	if err != nil {
		t.Fatalf("ListSubagents: %v", err)
	}
	if requestURI != "/v1/sessions/parent%2Fone/subagents" {
		t.Fatalf("request URI = %q", requestURI)
	}
	if len(subagents) != 2 || subagents[0].SessionID != "child-1" || subagents[0].Purpose != "Review tests" {
		t.Fatalf("subagents = %+v", subagents)
	}
}

func TestListSubagentsReportsHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "subagents unavailable", http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL).ListSubagents(context.Background(), "parent")
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "subagents unavailable") {
		t.Fatalf("error = %v", err)
	}
}

func TestGetSessionContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sessions/sess-1/context" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"contextWindow":256000,"lastPromptTokens":128000}`)
	}))
	defer srv.Close()

	contextUsage, err := NewClient(srv.URL).GetSessionContext(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("GetSessionContext: %v", err)
	}
	if contextUsage.ContextWindow != 256000 || contextUsage.LastPromptTokens == nil || *contextUsage.LastPromptTokens != 128000 {
		t.Fatalf("context usage = %+v", contextUsage)
	}
}

func TestPostMessage(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sessions/sess-1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(202)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	if err := c.PostMessage(context.Background(), "sess-1", "hello"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if gotBody["content"] != "hello" {
		t.Fatalf("content = %q, want hello", gotBody["content"])
	}
}

func TestPostMessageAppendUsesChimeraQueue(t *testing.T) {
	var requestURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestURI = r.URL.RequestURI()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := NewClient(srv.URL).PostMessageAppend(context.Background(), "sess-1", "next"); err != nil {
		t.Fatalf("PostMessageAppend: %v", err)
	}
	if requestURI != "/v1/sessions/sess-1/messages?append=true" {
		t.Fatalf("request URI = %q", requestURI)
	}
}

func TestPostMessageBusy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.PostMessage(context.Background(), "sess-1", "hello")
	if err != ErrBusy {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
}

func TestInterrupt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sessions/sess-1/interrupt" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	if err := c.Interrupt(context.Background(), "sess-1"); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
}

func TestResolvePermission(t *testing.T) {
	tests := []struct {
		name   string
		action PermissionAction
		body   string
	}{
		{name: "allow", action: PermissionAction{Decision: "allow"}, body: `{"decision":"allow"}`},
		{name: "deny with feedback", action: PermissionAction{Decision: "deny", Feedback: "Use a safer command"}, body: `{"decision":"deny","feedback":"Use a safer command"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotBody string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/sessions/sess-1/permissions/req-1" {
					t.Fatalf("unexpected path: %s", r.URL.Path)
				}
				body, _ := io.ReadAll(r.Body)
				gotBody = string(body)
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()

			c := NewClient(srv.URL)
			if err := c.ResolvePermission(context.Background(), "sess-1", "req-1", tt.action); err != nil {
				t.Fatalf("ResolvePermission: %v", err)
			}
			if gotBody != tt.body {
				t.Fatalf("body = %s, want %s", gotBody, tt.body)
			}
		})
	}
}

func TestResolvePermissionPropagatesNonSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer srv.Close()

	err := NewClient(srv.URL).ResolvePermission(context.Background(), "sess-1", "req-1", PermissionAction{Decision: "allow"})
	if err == nil || !strings.Contains(err.Error(), "409 Conflict") {
		t.Fatalf("error = %v, want 409 Conflict", err)
	}
}

func TestParseSSE(t *testing.T) {
	// Build a raw SSE stream with multiple events, a heartbeat, and
	// multi-line data.
	sse := strings.Join([]string{
		`: heartbeat`,
		``,
		`event: agent_event`,
		`id: evt-1`,
		`data: {"type":"user_message","content":"hi","eventId":"evt-1","sessionId":"s1","ts":1000}`,
		``,
		`event: agent_event`,
		`id: evt-2`,
		`data: {"type":"assistant_text_done","text":"hello","eventId":"evt-2","sessionId":"s1","ts":2000}`,
		``,
		`: heartbeat`,
		``,
	}, "\n")

	var events []EventEnvelope
	err := parseSSE(strings.NewReader(sse), func(env EventEnvelope) {
		events = append(events, env)
	})
	if err != nil {
		t.Fatalf("parseSSE: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].EventID != "evt-1" {
		t.Fatalf("events[0] EventID = %q, want evt-1", events[0].EventID)
	}
	if events[0].Type != "user_message" {
		t.Fatalf("events[0] Type = %q, want user_message", events[0].Type)
	}
	if events[1].EventID != "evt-2" {
		t.Fatalf("events[1] EventID = %q, want evt-2", events[1].EventID)
	}
}

func TestParseLockfile(t *testing.T) {
	data := []byte(`{"pid":12345,"port":38123,"cwd":"/tmp","sessionId":"sess-abc","startedAt":1700000000,"version":"0.1.0","url":"http://127.0.0.1:38123"}`)
	lf, err := ParseLockfile(data)
	if err != nil {
		t.Fatalf("ParseLockfile: %v", err)
	}
	if lf.PID != 12345 {
		t.Fatalf("PID = %d, want 12345", lf.PID)
	}
	if lf.SessionID != "sess-abc" {
		t.Fatalf("SessionID = %q, want sess-abc", lf.SessionID)
	}
	if lf.URL != "http://127.0.0.1:38123" {
		t.Fatalf("URL = %q, want http://127.0.0.1:38123", lf.URL)
	}
}

func TestSubscribe(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprintf(w, "event: agent_event\nid: evt-1\ndata: {\"type\":\"user_message\",\"content\":\"hi\",\"eventId\":\"evt-1\",\"sessionId\":\"s1\",\"ts\":1000}\n\n")
		w.(http.Flusher).Flush()
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := c.Subscribe(ctx, "s1", "", func(env EventEnvelope) {
		count.Add(1)
		if env.EventID != "evt-1" {
			t.Errorf("EventID = %q, want evt-1", env.EventID)
		}
	})
	// ctx timeout is expected
	if err != nil && err != context.DeadlineExceeded {
		// The server keeps the stream open, so we expect a timeout
		t.Logf("Subscribe returned: %v (timeout expected)", err)
	}
	if count.Load() == 0 {
		t.Fatal("expected at least 1 event, got 0")
	}
}

func TestSubscribeWithSince(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("since") != "evt-0" {
			t.Errorf("since = %q, want evt-0", r.URL.Query().Get("since"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprintf(w, "event: agent_event\nid: evt-1\ndata: {\"type\":\"user_message\",\"content\":\"hi\",\"eventId\":\"evt-1\",\"sessionId\":\"s1\",\"ts\":1000}\n\n")
		w.(http.Flusher).Flush()
		// Keep stream open
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	var got EventEnvelope
	go func() {
		time.Sleep(500 * time.Millisecond)
		cancel()
	}()
	_ = c.Subscribe(ctx, "s1", "evt-0", func(env EventEnvelope) {
		got = env
	})
	if got.EventID != "evt-1" {
		t.Fatalf("EventID = %q, want evt-1", got.EventID)
	}
}
