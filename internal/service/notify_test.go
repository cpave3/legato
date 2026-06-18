package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewNtfyNotifier_EmptyTopic(t *testing.T) {
	n := NewNtfyNotifier("https://ntfy.sh", "", "")
	if n.Configured() {
		t.Error("expected Configured() == false for empty topic")
	}
}

func TestNtfyNotifier_NotifyAsync(t *testing.T) {
	var receivedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		receivedBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewNtfyNotifier(srv.URL, "test-topic", "secret").(*ntfyNotifier)
	if !n.Configured() {
		t.Fatal("expected configured")
	}
	if n.client.Timeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", n.client.Timeout)
	}

	err := n.Notify("Agent ready", "Agent TASK-1 is idle")
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}

	// Wait for the async goroutine to finish
	time.Sleep(100 * time.Millisecond)
	if receivedBody != "Agent TASK-1 is idle" {
		t.Errorf("body = %q, want %q", receivedBody, "Agent TASK-1 is idle")
	}
}

func TestNtfyNotifier_CanNotify_RateLimit(t *testing.T) {
	n := &ntfyNotifier{
		url:         "https://ntfy.sh",
		topic:       "test",
		client:      &http.Client{Timeout: 5 * time.Second},
		lastSent:    make(map[string]time.Time),
		minInterval: 100 * time.Millisecond,
	}

	if !n.CanNotify("task1") {
		t.Error("expected CanNotify first call")
	}
	if n.CanNotify("task1") {
		t.Error("expected rate-limited second call")
	}

	time.Sleep(150 * time.Millisecond)
	if !n.CanNotify("task1") {
		t.Error("expected CanNotify after interval")
	}
}

func TestNtfyNotifier_DifferentTasksIndependent(t *testing.T) {
	n := &ntfyNotifier{
		url:         "https://ntfy.sh",
		topic:       "test",
		client:      &http.Client{Timeout: 5 * time.Second},
		lastSent:    make(map[string]time.Time),
		minInterval: 5 * time.Minute,
	}

	if !n.CanNotify("task1") {
		t.Error("expected CanNotify for task1")
	}
	if !n.CanNotify("task2") {
		t.Error("expected CanNotify for task2 (independent)")
	}
}

func TestNoopNotifier(t *testing.T) {
	n := &noopNotifier{}
	if n.Configured() {
		t.Error("expected Configured() == false")
	}
	if err := n.Notify("x", "y"); err != nil {
		t.Errorf("Notify: %v", err)
	}
}
