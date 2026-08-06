package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/cpave3/legato/internal/engine/chimera"
	"github.com/cpave3/legato/internal/engine/store"
)

func TestChimeraSendMessageExpandsAttachments(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoDir, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "src", "app.go"), []byte("package src\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var paths []struct {
		Kind string `json:"kind"`
		Path string `json:"path"`
	}
	var appends []string
	var sentContent string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/sessions/sess-1" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"cwd": repoDir})
		case r.URL.Path == "/v1/sessions/sess-1/paths" && r.Method == http.MethodPost:
			var body struct {
				Kind string `json:"kind"`
				Path string `json:"path"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			paths = append(paths, body)
			w.WriteHeader(204)
		case r.URL.Path == "/v1/sessions/sess-1/messages" && r.URL.Query().Get("append") == "true":
			var body struct {
				Content string `json:"content"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			appends = append(appends, body.Content)
			w.WriteHeader(204)
		case r.URL.Path == "/v1/sessions/sess-1/messages" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			var bodyJSON struct {
				Content string `json:"content"`
			}
			json.Unmarshal(body, &bodyJSON)
			sentContent = bodyJSON.Content
			w.WriteHeader(204)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	adapter := &ChimeraInteractionAdapter{
		clientFactory: func(endpoint string) *chimera.Client { return chimera.NewClient(srv.URL) },
		store:         nil,
	}

	// Mock store to return a session with the test server's endpoint
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	adapter.store = s

	// Create a task and agent session with Chimera endpoint
	endpoint := srv.URL
	sessionID := "sess-1"
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{ID: "task-1", Title: "Test", Status: "Todo"}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertAgentSession(ctx, store.AgentSession{
		TaskID:    "task-1",
		Status:    "running",
		AgentKind: "chimera",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateChimeraDiscovery(ctx, "task-1", sessionID, endpoint); err != nil {
		t.Fatal(err)
	}

	err = adapter.SendMessage(ctx, "task-1", "fix @main.go and #src")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if len(paths) != 2 {
		t.Fatalf("path grants = %+v", paths)
	}
	if paths[0].Kind != "read" || paths[0].Path != filepath.Join(repoDir, "main.go") {
		t.Fatalf("read path = %+v", paths[0])
	}
	if paths[1].Kind != "write" || paths[1].Path != filepath.Join(repoDir, "src") {
		t.Fatalf("write path = %+v", paths[1])
	}

	if len(appends) != 2 {
		t.Fatalf("appends = %+v", appends)
	}
	if !containsSubstr(appends[0], "[auto-attached @main.go]") || !containsSubstr(appends[0], "1\tpackage main") {
		t.Fatalf("file append = %q", appends[0])
	}
	if !containsSubstr(appends[1], "[auto-attached #src]") || !containsSubstr(appends[1], "app.go") {
		t.Fatalf("dir append = %q", appends[1])
	}

	if sentContent != "fix @main.go and #src" {
		t.Fatalf("sent content = %q", sentContent)
	}
}

func TestChimeraQueueMessageStartsIdleSessionWithoutExpandingAttachments(t *testing.T) {
	var requestURIs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestURIs = append(requestURIs, r.URL.RequestURI())
		if r.URL.Path == "/v1/sessions/sess-1/messages" && r.URL.RawQuery == "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	endpoint := srv.URL
	sessionID := "sess-1"
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{ID: "task-1", Title: "Test", Status: "Todo"}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertAgentSession(ctx, store.AgentSession{
		TaskID:    "task-1",
		Status:    "running",
		AgentKind: "chimera",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateChimeraDiscovery(ctx, "task-1", sessionID, endpoint); err != nil {
		t.Fatal(err)
	}

	adapter := &ChimeraInteractionAdapter{
		clientFactory: func(e string) *chimera.Client { return chimera.NewClient(srv.URL) },
		store:         s,
	}

	err = adapter.QueueMessage(ctx, "task-1", "fix @main.go")
	if err != nil {
		t.Fatalf("QueueMessage: %v", err)
	}
	if len(requestURIs) != 1 || requestURIs[0] != "/v1/sessions/sess-1/messages" {
		t.Fatalf("request URIs = %#v, want one idle-start request", requestURIs)
	}
}

func TestChimeraQueueMessageAppendsWhenSessionIsBusy(t *testing.T) {
	var requestURIs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestURIs = append(requestURIs, r.URL.RequestURI())
		if r.URL.Query().Get("append") == "true" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusConflict)
	}))
	defer srv.Close()

	dbPath := filepath.Join(t.TempDir(), "busy.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{ID: "task-busy", Title: "Busy", Status: "Todo"}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertAgentSession(ctx, store.AgentSession{TaskID: "task-busy", Status: "running", AgentKind: "chimera"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateChimeraDiscovery(ctx, "task-busy", "sess-busy", srv.URL); err != nil {
		t.Fatal(err)
	}
	adapter := &ChimeraInteractionAdapter{clientFactory: func(string) *chimera.Client { return chimera.NewClient(srv.URL) }, store: s}

	if err := adapter.QueueMessage(ctx, "task-busy", "coordinate"); err != nil {
		t.Fatal(err)
	}
	want := []string{"/v1/sessions/sess-busy/messages", "/v1/sessions/sess-busy/messages?append=true"}
	if !slices.Equal(requestURIs, want) {
		t.Fatalf("request URIs = %#v, want %#v", requestURIs, want)
	}
}

func containsSubstr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || containsSubstrHelper(s, sub))
}

func containsSubstrHelper(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestChimeraSendMessageContinuesWhenAttachmentFails(t *testing.T) {
	repoDir := t.TempDir()
	var sentContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/sessions/sess-1" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"cwd": repoDir})
		case r.URL.Path == "/v1/sessions/sess-1/paths":
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "no such file or directory"})
		case r.URL.Path == "/v1/sessions/sess-1/messages" && r.Method == http.MethodPost:
			var body struct {
				Content string `json:"content"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			sentContent = body.Content
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{ID: "task-1", Title: "Test", Status: "Todo"}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertAgentSession(ctx, store.AgentSession{TaskID: "task-1", Status: "running", AgentKind: "chimera"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateChimeraDiscovery(ctx, "task-1", "sess-1", srv.URL); err != nil {
		t.Fatal(err)
	}
	adapter := NewChimeraInteractionAdapter(s, func(string) *chimera.Client { return chimera.NewClient(srv.URL) })

	if err := adapter.SendMessage(ctx, "task-1", "review @missing.md anyway"); err != nil {
		t.Fatal(err)
	}
	if sentContent != "review @missing.md anyway" {
		t.Fatalf("sent content = %q", sentContent)
	}
}
