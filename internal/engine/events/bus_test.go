package events

import (
	"testing"
	"time"
)

func TestSubscribeAndPublishDeliversEvent(t *testing.T) {
	bus := New()
	ch := bus.Subscribe(EventCardMoved)

	want := Event{
		Type:    EventCardMoved,
		Payload: "ticket-123",
		At:      time.Now(),
	}
	bus.Publish(want)

	select {
	case got := <-ch:
		if got.Type != want.Type {
			t.Errorf("Type = %v, want %v", got.Type, want.Type)
		}
		if got.Payload != want.Payload {
			t.Errorf("Payload = %v, want %v", got.Payload, want.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestMultipleSubscribersAllReceiveEvent(t *testing.T) {
	bus := New()
	ch1 := bus.Subscribe(EventSyncCompleted)
	ch2 := bus.Subscribe(EventSyncCompleted)
	ch3 := bus.Subscribe(EventSyncCompleted)

	bus.Publish(Event{Type: EventSyncCompleted, Payload: "done", At: time.Now()})

	for i, ch := range []<-chan Event{ch1, ch2, ch3} {
		select {
		case got := <-ch:
			if got.Payload != "done" {
				t.Errorf("subscriber %d: Payload = %v, want %q", i, got.Payload, "done")
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out", i)
		}
	}
}

func TestUnrelatedEventTypeNotReceived(t *testing.T) {
	bus := New()
	ch := bus.Subscribe(EventCardMoved)

	bus.Publish(Event{Type: EventSyncStarted, Payload: "sync", At: time.Now()})

	select {
	case got := <-ch:
		t.Fatalf("received unexpected event: %v", got)
	case <-time.After(50 * time.Millisecond):
		// expected — no event received
	}
}

func TestUnsubscribeStopsDeliveryAndClosesChannel(t *testing.T) {
	bus := New()
	ch := bus.Subscribe(EventCardUpdated)

	bus.Unsubscribe(ch)

	bus.Publish(Event{Type: EventCardUpdated, Payload: "update", At: time.Now()})

	_, open := <-ch
	if open {
		t.Fatal("channel should be closed after unsubscribe")
	}
}

func TestUnsubscribeUnknownChannelIsNoop(t *testing.T) {
	bus := New()
	unknown := make(chan Event)
	// should not panic
	bus.Unsubscribe(unknown)
}

func TestFullBufferDropsEventWithoutBlocking(t *testing.T) {
	bus := New()
	ch := bus.Subscribe(EventCardMoved)

	// Fill the buffer (64)
	for i := 0; i < 64; i++ {
		bus.Publish(Event{Type: EventCardMoved, Payload: i, At: time.Now()})
	}

	// This publish should drop, not block
	done := make(chan struct{})
	go func() {
		bus.Publish(Event{Type: EventCardMoved, Payload: "dropped", At: time.Now()})
		close(done)
	}()

	select {
	case <-done:
		// good — publish didn't block
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on full buffer")
	}

	// Verify the other subscriber still works
	ch2 := bus.Subscribe(EventCardMoved)
	bus.Publish(Event{Type: EventCardMoved, Payload: "after", At: time.Now()})
	select {
	case got := <-ch2:
		if got.Payload != "after" {
			t.Errorf("Payload = %v, want %q", got.Payload, "after")
		}
	case <-time.After(time.Second):
		t.Fatal("new subscriber didn't receive event")
	}

	_ = ch // used above to fill buffer
}

func TestErrorEventPayloads(t *testing.T) {
	bus := New()
	ch := bus.Subscribe(EventSyncError)

	payload := ErrorPayload{
		ErrorType: "offline",
		Message:   "network unreachable",
		TicketKey: "",
	}
	bus.Publish(Event{Type: EventSyncError, Payload: payload, At: time.Now()})

	select {
	case got := <-ch:
		p, ok := got.Payload.(ErrorPayload)
		if !ok {
			t.Fatalf("expected ErrorPayload, got %T", got.Payload)
		}
		if p.ErrorType != "offline" {
			t.Errorf("ErrorType = %q, want offline", p.ErrorType)
		}
		if p.Message != "network unreachable" {
			t.Errorf("Message = %q, want 'network unreachable'", p.Message)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestTransitionFailedEvent(t *testing.T) {
	bus := New()
	ch := bus.Subscribe(EventTransitionFailed)

	payload := ErrorPayload{
		ErrorType: "transition_failed",
		Message:   "transition not available",
		TicketKey: "REX-42",
	}
	bus.Publish(Event{Type: EventTransitionFailed, Payload: payload, At: time.Now()})

	select {
	case got := <-ch:
		p := got.Payload.(ErrorPayload)
		if p.TicketKey != "REX-42" {
			t.Errorf("TicketKey = %q, want REX-42", p.TicketKey)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestConcurrentPublishSubscribeUnsubscribe(t *testing.T) {
	bus := New()
	done := make(chan struct{})

	// Run concurrent operations under the race detector
	go func() {
		for i := 0; i < 100; i++ {
			ch := bus.Subscribe(EventType(i % 6))
			bus.Publish(Event{Type: EventType(i % 6), At: time.Now()})
			bus.Unsubscribe(ch)
		}
		close(done)
	}()

	go func() {
		for i := 0; i < 100; i++ {
			bus.Publish(Event{Type: EventType(i % 6), At: time.Now()})
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent operations timed out")
	}
}
