package server

import (
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
)

// Both invalidation broadcasters tell every client once per event of
// either kind, and say nothing more once stopped, however often stop runs.
func TestInvalidationBroadcastersStopCleanly(t *testing.T) {
	for _, tc := range []struct {
		name  string
		start func(*Server, *events.Bus) func()
		msg   string
		kinds []events.EventType
	}{
		{"usage", (*Server).StartSubscriptionUsageEvents, MsgUsageChanged,
			[]events.EventType{events.EventSubscriptionUsageChanged, events.EventSubscriptionUsageInvalidated}},
		{"theme", (*Server).StartServerThemeEvents, MsgServerThemeChanged,
			[]events.EventType{events.EventServerThemeChanged, events.EventServerThemeInvalidated}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := New(nil, nil, nil, ":0")
			bus := events.New()
			ch := make(chan WSMessage, 8)
			srv.hub.add(&wsClient{sendFn: func(msg WSMessage) { ch <- msg }})
			stop := tc.start(srv, bus)
			for _, kind := range tc.kinds {
				bus.Publish(events.Event{Type: kind, At: time.Now()})
				select {
				case msg := <-ch:
					if msg.Type != tc.msg {
						t.Fatalf("event %v broadcast %q, want %q", kind, msg.Type, tc.msg)
					}
				case <-time.After(2 * time.Second):
					t.Fatalf("event %v was not broadcast", kind)
				}
			}

			stopped := make(chan struct{})
			go func() { stop(); stop(); close(stopped) }()
			select {
			case <-stopped:
			case <-time.After(2 * time.Second):
				t.Fatal("stop did not return")
			}

			for _, kind := range tc.kinds {
				bus.Publish(events.Event{Type: kind, At: time.Now()})
			}
			select {
			case msg := <-ch:
				t.Fatalf("broadcast %q after stop", msg.Type)
			case <-time.After(100 * time.Millisecond):
			}
		})
	}
}
