package codexbridge

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

// Codex's hooks may not report a turn that was interrupted or failed; the
// bridge sees how every turn of the terminal's thread ended and says so for
// those that did not complete.
func TestATurnThatDidNotCompleteIsReported(t *testing.T) {
	ended := make(chan string, 8)
	codex := newAppServer(t)
	bridge := New(Options{
		TaskID: "task-1", AppServerURL: codex.url(), AppServerToken: appServerToken,
		Now:            func() time.Time { return time.UnixMilli(1_700_000_000_000) },
		RejoinEvery:    5 * time.Millisecond,
		OnTurnEnded:    func(status string) { ended <- status },
		TurnEndedDelay: time.Millisecond,
	})
	if err := bridge.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	go func() { _ = bridge.Run(ctx) }()
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(func() {
		stop()
		bridge.Close()
		server.Close()
	})
	h := &harness{t: t, codex: codex, bridge: bridge, server: server}
	h.attach()

	h.begin("turn-1")
	h.finish("turn-1", "completed")
	for _, turn := range []struct{ id, status string }{{"turn-2", "interrupted"}, {"turn-3", "failed"}} {
		h.codex.notify("turn/started", map[string]any{"threadId": threadID, "turn": map[string]any{"id": turn.id, "status": "inProgress", "items": []any{}}})
		h.finish(turn.id, turn.status)
	}

	// Each report runs on its own, so they may arrive in either order.
	got := map[string]bool{}
	for range 2 {
		select {
		case status := <-ended:
			got[status] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("reported only %v, want interrupted and failed", got)
		}
	}
	if !got["interrupted"] || !got["failed"] {
		t.Fatalf("reported %v, want interrupted and failed", got)
	}
	select {
	case got := <-ended:
		t.Fatalf("also reported %q", got)
	case <-time.After(50 * time.Millisecond):
	}
}

// An interrupt followed at once by the next turn: that turn's own hooks say
// it works, and the end of the one before must not undo them.
func TestATurnThatEndsAsTheNextBeginsIsNotReported(t *testing.T) {
	ended := make(chan string, 8)
	codex := newAppServer(t)
	bridge := New(Options{
		TaskID: "task-1", AppServerURL: codex.url(), AppServerToken: appServerToken,
		Now:            func() time.Time { return time.UnixMilli(1_700_000_000_000) },
		RejoinEvery:    5 * time.Millisecond,
		OnTurnEnded:    func(status string) { ended <- status },
		TurnEndedDelay: 200 * time.Millisecond,
	})
	if err := bridge.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	go func() { _ = bridge.Run(ctx) }()
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(func() {
		stop()
		bridge.Close()
		server.Close()
	})
	h := &harness{t: t, codex: codex, bridge: bridge, server: server}
	h.attach()
	h.begin("turn-1")

	h.finish("turn-1", "interrupted")
	h.codex.notify("turn/started", map[string]any{"threadId": threadID, "turn": map[string]any{"id": "turn-2", "status": "inProgress", "items": []any{}}})

	select {
	case status := <-ended:
		t.Fatalf("reported %q although the next turn had started", status)
	case <-time.After(400 * time.Millisecond):
	}
}
