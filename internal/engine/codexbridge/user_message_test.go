package codexbridge

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

// A user message the bridge sees reach the terminal's thread live is
// reported, so Legato can close the agent's operator items; the history it
// loads when it joins is not.
func TestALiveUserMessageIsReportedAndHistoryIsNot(t *testing.T) {
	said := make(chan string, 8)
	codex := newAppServer(t)
	bridge := New(Options{
		TaskID: "task-1", AppServerURL: codex.url(), AppServerToken: appServerToken,
		Now:           func() time.Time { return time.UnixMilli(1_700_000_000_000) },
		RejoinEvery:   5 * time.Millisecond,
		OnUserMessage: func(text string) { said <- text },
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
	h.begin("turn-2", map[string]any{"id": "turn-1", "status": "completed", "items": []any{userMessage("item-u0", "an old message")}})

	h.item("completed", userMessage("item-u1", "/review"))
	select {
	case text := <-said:
		if text != "/review" {
			t.Fatalf("reported %q, want the live message", text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the live user message was not reported")
	}
	select {
	case text := <-said:
		t.Fatalf("also reported %q", text)
	case <-time.After(50 * time.Millisecond):
	}
}
