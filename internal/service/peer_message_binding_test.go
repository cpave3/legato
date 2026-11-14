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
