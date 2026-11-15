package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/hooks"
)

func TestPeerMessageWaitsForChimeraModBinding(t *testing.T) {
	svc, s, mt := newNativeLaunchService(t, t.TempDir())
	a := hooks.NewChimeraAdapter("legato")
	a.SetModDir(t.TempDir())
	svc.(*agentService).adapter = a
	svc.(*agentService).nativeBindTimeout = time.Second
	ctx := context.Background()
	createTask(t, s, "recipient")
	if err := svc.SpawnAgent(ctx, "recipient", 0, 0); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- svc.SendPeerMessage(ctx, "sender", "recipient", "coordinate with me") }()
	select {
	case err := <-done:
		t.Fatalf("peer message did not wait for binding: %v", err)
	case <-time.After(80 * time.Millisecond):
	}
	if lines := mt.sentLinesFor("legato-recipient"); len(lines) != 1 {
		t.Fatalf("peer notice was typed: %v", lines)
	}
	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/secret/v1/sessions/recipient/messages" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Text string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		received <- body.Text
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	session, err := s.GetAgentSessionByTaskID(ctx, "recipient")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateChimeraDiscoveryForSession(ctx, session.ID, "recipient", server.URL+"/secret"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case text := <-received:
		if !strings.Contains(text, "[legato peer sender]") || !strings.Contains(text, "coordinate with me") {
			t.Fatalf("peer message=%q", text)
		}
	case <-time.After(time.Second):
		t.Fatal("peer message did not reach HTTP")
	}
	if lines := mt.sentLinesFor("legato-recipient"); len(lines) != 1 {
		t.Fatalf("peer notice was typed: %v", lines)
	}
}

func TestBoundChimeraFollowupsAndNoticesUseTheActualNativeConversation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LEGATO_HOME", t.TempDir())
	svc, s, mt := newNativeLaunchService(t, t.TempDir())
	a := hooks.NewChimeraAdapter("legato")
	a.SetModDir(t.TempDir())
	svc.(*agentService).adapter = a
	ctx := context.Background()
	createTask(t, s, "recipient")
	if err := svc.SpawnAgent(ctx, "recipient", 0, 0); err != nil {
		t.Fatal(err)
	}
	var paths, texts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/secret/v1/sessions/real-conversation" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]string{"id": "real-conversation", "cwd": t.TempDir()})
			return
		}
		if r.URL.Path != "/secret/v1/sessions/real-conversation/messages" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		paths = append(paths, r.URL.RequestURI())
		texts = append(texts, body.Content)
		if strings.Contains(body.Content, "busy") && r.URL.Query().Get("append") != "true" {
			w.WriteHeader(http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	session, err := s.GetAgentSessionByTaskID(ctx, "recipient")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateChimeraDiscoveryForSession(ctx, session.ID, "real-conversation", server.URL+"/secret"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SendMessage(ctx, "recipient", "operator follow-up\nsecond line"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SendPeerMessage(ctx, "sender", "recipient", "busy peer follow-up"); err != nil {
		t.Fatal(err)
	}
	if err := svc.(AgentMessageQueuer).QueueMessage(ctx, "recipient", "[legato coordination] another agent joined"); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 4 || paths[2] != "/secret/v1/sessions/real-conversation/messages?append=true" {
		t.Fatalf("native requests=%v", paths)
	}
	if texts[0] != "operator follow-up\nsecond line" || !strings.Contains(texts[1], "[legato peer sender]") || texts[1] != texts[2] || texts[3] != "[legato coordination] another agent joined" {
		t.Fatalf("native texts=%v", texts)
	}
	if lines := mt.sentLinesFor("legato-recipient"); len(lines) != 1 {
		t.Fatalf("messages typed: %v", lines)
	}
	if texts := mt.sentMultilineFor("legato-recipient"); len(texts) != 0 {
		t.Fatalf("messages typed: %v", texts)
	}
}
