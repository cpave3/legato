package jira

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientBasicAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"startAt":0,"maxResults":50,"total":0,"issues":[]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "alice@example.com", "secret-token", 30*time.Second)
	_, err := c.Search(context.Background(), "project = TEST")
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	want := "Basic YWxpY2VAZXhhbXBsZS5jb206c2VjcmV0LXRva2Vu"
	if gotAuth != want {
		t.Errorf("auth header = %q, want %q", gotAuth, want)
	}
}

func TestClientTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "a@b.com", "tok", 50*time.Millisecond)
	_, err := c.Search(context.Background(), "project = TEST")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestClientAcceptHeader(t *testing.T) {
	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"startAt":0,"maxResults":50,"total":0,"issues":[]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "a@b.com", "tok", 30*time.Second)
	c.Search(context.Background(), "project = TEST")

	if gotAccept != "application/json" {
		t.Errorf("accept = %q, want application/json", gotAccept)
	}
}

// Task 1.3: Search with pagination
func TestSearchPagination(t *testing.T) {
	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		startAt := r.URL.Query().Get("startAt")
		w.Header().Set("Content-Type", "application/json")
		switch startAt {
		case "0":
			fmt.Fprint(w, `{"startAt":0,"maxResults":50,"total":3,"issues":[
				{"key":"P-1","fields":{"summary":"One","status":{"name":"Open"},"updated":"2025-01-01T00:00:00.000+0000","project":{"key":"P"}}},
				{"key":"P-2","fields":{"summary":"Two","status":{"name":"Open"},"updated":"2025-01-01T00:00:00.000+0000","project":{"key":"P"}}}
			]}`)
		case "2":
			fmt.Fprint(w, `{"startAt":2,"maxResults":50,"total":3,"issues":[
				{"key":"P-3","fields":{"summary":"Three","status":{"name":"Done"},"updated":"2025-01-01T00:00:00.000+0000","project":{"key":"P"}}}
			]}`)
		default:
			t.Errorf("unexpected startAt: %s", startAt)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "a@b.com", "tok", 30*time.Second)
	issues, err := c.Search(context.Background(), "project = P")
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(issues) != 3 {
		t.Errorf("got %d issues, want 3", len(issues))
	}
	if callCount != 2 {
		t.Errorf("made %d API calls, want 2", callCount)
	}
	if issues[2].Key != "P-3" {
		t.Errorf("issues[2].key = %q, want P-3", issues[2].Key)
	}
}

func TestSearchEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"startAt":0,"maxResults":50,"total":0,"issues":[]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "a@b.com", "tok", 30*time.Second)
	issues, err := c.Search(context.Background(), "project = NOPE")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("got %d issues, want 0", len(issues))
	}
}

func TestSearchInvalidJQL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"errorMessages":["Error in the JQL Query"]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "a@b.com", "tok", 30*time.Second)
	_, err := c.Search(context.Background(), "invalid!!")
	if err == nil {
		t.Fatal("expected error for invalid JQL, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should mention 400: %v", err)
	}
}

// Task 1.4: GetIssue
func TestGetIssue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issue/PROJ-123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"key": "PROJ-123",
			"fields": {
				"summary": "Fix the thing",
				"status": {"name": "In Progress", "id": "3"},
				"priority": {"name": "High"},
				"issuetype": {"name": "Bug"},
				"assignee": {"displayName": "Alice"},
				"labels": ["backend"],
				"description": {"version":1,"type":"doc","content":[]},
				"updated": "2025-01-15T10:30:00.000+0000",
				"project": {"key": "PROJ"},
				"customfield_10014": "PROJ-100"
			}
		}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "a@b.com", "tok", 30*time.Second)
	issue, err := c.GetIssue(context.Background(), "PROJ-123")
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}

	if issue.Key != "PROJ-123" {
		t.Errorf("key = %q, want PROJ-123", issue.Key)
	}
	if issue.Fields.Summary != "Fix the thing" {
		t.Errorf("summary = %q", issue.Fields.Summary)
	}
	if issue.Fields.EpicKey != "PROJ-100" {
		t.Errorf("epic key = %q, want PROJ-100", issue.Fields.EpicKey)
	}
	if issue.Fields.Assignee.DisplayName != "Alice" {
		t.Errorf("assignee = %q", issue.Fields.Assignee.DisplayName)
	}
}

func TestGetIssueNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"errorMessages":["Issue does not exist"]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "a@b.com", "tok", 30*time.Second)
	_, err := c.GetIssue(context.Background(), "NOPE-999")
	if err == nil {
		t.Fatal("expected error for missing issue")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention not found: %v", err)
	}
}

// Task 1.5: GetTransitions
func TestGetTransitions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/transitions") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"transitions":[
			{"id":"21","name":"Start Progress","to":{"name":"In Progress","id":"3"}},
			{"id":"31","name":"Done","to":{"name":"Done","id":"10001"}}
		]}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "a@b.com", "tok", 30*time.Second)
	trans, err := c.GetTransitions(context.Background(), "PROJ-1")
	if err != nil {
		t.Fatalf("get transitions: %v", err)
	}

	if len(trans) != 2 {
		t.Fatalf("got %d transitions, want 2", len(trans))
	}
	if trans[0].ID != "21" || trans[0].Name != "Start Progress" || trans[0].To.Name != "In Progress" {
		t.Errorf("transition[0] = %+v", trans[0])
	}
	if trans[1].ID != "31" || trans[1].To.Name != "Done" {
		t.Errorf("transition[1] = %+v", trans[1])
	}
}

func TestGetTransitionsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"transitions":[]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "a@b.com", "tok", 30*time.Second)
	trans, err := c.GetTransitions(context.Background(), "PROJ-1")
	if err != nil {
		t.Fatalf("get transitions: %v", err)
	}
	if len(trans) != 0 {
		t.Errorf("got %d transitions, want 0", len(trans))
	}
}

// Task 1.6: DoTransition
func TestDoTransition(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "a@b.com", "tok", 30*time.Second)
	err := c.DoTransition(context.Background(), "PROJ-1", "31")
	if err != nil {
		t.Fatalf("do transition: %v", err)
	}

	if !strings.Contains(gotBody, `"id":"31"`) {
		t.Errorf("body = %q, should contain transition id", gotBody)
	}
}

func TestDoTransitionBadRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"errorMessages":["Transition not available"]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "a@b.com", "tok", 30*time.Second)
	err := c.DoTransition(context.Background(), "PROJ-1", "999")
	if err == nil {
		t.Fatal("expected error for bad transition")
	}
}

func TestDoTransitionNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "a@b.com", "tok", 30*time.Second)
	err := c.DoTransition(context.Background(), "NOPE-1", "31")
	if err == nil {
		t.Fatal("expected error for missing issue")
	}
}

// Task 1.7: Rate limiting
func TestRateLimitWithRetryAfter(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"startAt":0,"maxResults":50,"total":0,"issues":[]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "a@b.com", "tok", 30*time.Second)
	start := time.Now()
	_, err := c.Search(context.Background(), "project = P")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("search after retry: %v", err)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("attempts = %d, want 2", atomic.LoadInt32(&attempts))
	}
	if elapsed < 900*time.Millisecond {
		t.Errorf("retry too fast: %v (expected >= ~1s)", elapsed)
	}
}

func TestRateLimitExponentialBackoff(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"startAt":0,"maxResults":50,"total":0,"issues":[]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "a@b.com", "tok", 30*time.Second)
	start := time.Now()
	_, err := c.Search(context.Background(), "project = P")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("search after retry: %v", err)
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("attempts = %d, want 3", atomic.LoadInt32(&attempts))
	}
	// 1s + 2s = 3s minimum
	if elapsed < 2900*time.Millisecond {
		t.Errorf("backoff too fast: %v (expected >= ~3s)", elapsed)
	}
}

func TestRateLimitContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	c := NewClient(srv.URL, "a@b.com", "tok", 30*time.Second)
	_, err := c.Search(ctx, "project = P")
	if err == nil {
		t.Fatal("expected error when context cancelled during backoff")
	}
}

func TestAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "a@b.com", "bad-token", 30*time.Second)
	_, err := c.Search(context.Background(), "project = P")
	if err == nil {
		t.Fatal("expected auth error")
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("error = %v, should mention authentication", err)
	}
}
