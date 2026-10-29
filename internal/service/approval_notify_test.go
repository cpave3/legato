package service

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestANewApprovalSendsANotificationThatOpensItOnThePhone(t *testing.T) {
	var mu sync.Mutex
	var got *http.Request
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got, body = r, string(b)
		mu.Unlock()
	}))
	defer srv.Close()
	notify := ApprovalNotifications(NewNtfyNotifier(srv.URL, "topic", ""), "http://legato.lan:3080")

	details, _ := json.Marshal(map[string]string{"task_title": "Fix attention sort", "agent_kind": "claude-code", "command": "curl -H 'Authorization: Bearer s3cret' x"})
	notify(Approval{Approval: store.Approval{ID: "ap-1234abcd", TaskID: "t1", Title: "Run a command", Summary: "curl -H 'Authorization: Bearer s3cret' x"}, Details: details})

	mu.Lock()
	defer mu.Unlock()
	if got == nil {
		t.Fatal("no notification sent")
	}
	if got.Header.Get("Title") != "Run a command" {
		t.Fatalf("title = %q", got.Header.Get("Title"))
	}
	if click := got.Header.Get("Click"); click != "legato://approval?url=http%3A%2F%2Flegato.lan%3A3080&id=ap-1234abcd" {
		t.Fatalf("click = %q", click)
	}
	if !strings.Contains(body, "Fix attention sort") || strings.Contains(body, "s3cret") {
		t.Fatalf("body = %q: it should say who asks and never what the command holds", body)
	}
}

func TestANotificationHasNoLinkWithoutTheServersAddress(t *testing.T) {
	var click = "unset"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { click = r.Header.Get("Click") }))
	defer srv.Close()

	ApprovalNotifications(NewNtfyNotifier(srv.URL, "topic", ""), "")(Approval{Approval: store.Approval{ID: "ap-1", Title: "Run a command"}, Details: json.RawMessage(`{}`)})

	if click != "" {
		t.Fatalf("click = %q, want none", click)
	}
}
