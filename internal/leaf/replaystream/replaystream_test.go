package replaystream

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// publish takes the next ID and publishes data under it.
func publish(s *Stream, data string) string {
	id := s.NextID()
	s.Publish(id, []byte(data))
	return id
}

func ids(events []Event) []string {
	out := make([]string, len(events))
	for i, event := range events {
		out[i] = event.ID
	}
	return out
}

func receive(t *testing.T, sub *Subscriber) Event {
	t.Helper()
	select {
	case event, open := <-sub.Events():
		if !open {
			t.Fatal("subscriber closed")
		}
		return event
	case <-time.After(time.Second):
		t.Fatal("no event")
		return Event{}
	}
}

func TestIDsCountUpUnderThePrefixAndAreNeverReused(t *testing.T) {
	s := New("ab12", 10, 10)
	if id := s.NextID(); id != "ab12-1" {
		t.Fatalf("first ID = %q", id)
	}
	// An ID taken but never published (its event could not be encoded) is
	// still spent: the next event does not reuse it.
	if id := publish(s, "x"); id != "ab12-2" {
		t.Fatalf("second ID = %q", id)
	}
}

func TestASubscriberGetsWhatFollowsItsCursorThenLiveEvents(t *testing.T) {
	s := New("p", 10, 10)
	first := publish(s, "one")
	publish(s, "two")
	publish(s, "three")

	sub, replay := s.Subscribe(first)
	if got := strings.Join(ids(replay), ","); got != "p-2,p-3" {
		t.Fatalf("replay = %s", got)
	}
	if string(replay[0].Data) != "two" {
		t.Fatalf("replayed data = %q", replay[0].Data)
	}
	publish(s, "four")
	if event := receive(t, sub); event.ID != "p-4" || string(event.Data) != "four" {
		t.Fatalf("live event = %+v", event)
	}
}

func TestAnEmptyOrUnknownCursorReplaysNothing(t *testing.T) {
	s := New("p", 10, 10)
	publish(s, "one")
	for _, cursor := range []string{"", "q-1", "p-99", "nonsense"} {
		if _, replay := s.Subscribe(cursor); len(replay) != 0 {
			t.Fatalf("cursor %q replayed %v", cursor, ids(replay))
		}
	}
}

func TestOnlyTheNewestLimitAreKept(t *testing.T) {
	s := New("p", 3, 10)
	for _, data := range []string{"1", "2", "3", "4", "5"} {
		publish(s, data)
	}
	if _, replay := s.Subscribe("p-2"); len(replay) != 0 {
		t.Fatalf("an evicted cursor replayed %v", ids(replay))
	}
	if _, replay := s.Subscribe("p-3"); strings.Join(ids(replay), ",") != "p-4,p-5" {
		t.Fatalf("oldest kept cursor replayed %v", ids(replay))
	}
}

func TestAReplayIsTheSubscribersOwnCopy(t *testing.T) {
	s := New("p", 10, 10)
	publish(s, "1")
	publish(s, "2")
	_, replay := s.Subscribe("p-1")
	replay[0] = Event{ID: "changed"}
	if _, again := s.Subscribe("p-1"); again[0].ID != "p-2" {
		t.Fatalf("a replay shares the stream's events: %v", ids(again))
	}
}

func TestASubscriberThatFallsBehindIsDroppedAndClosedWhileOthersReceive(t *testing.T) {
	s := New("p", 10, 2)
	slow, _ := s.Subscribe("")
	fast, _ := s.Subscribe("")
	if s.Viewers() != 2 {
		t.Fatalf("viewers = %d", s.Viewers())
	}

	publish(s, "1")
	publish(s, "2")
	receive(t, fast)
	receive(t, fast)
	publish(s, "3") // slow's backlog of 2 is full: it is dropped, not waited for

	if event := receive(t, fast); event.ID != "p-3" {
		t.Fatalf("fast got %+v", event)
	}
	if s.Viewers() != 1 {
		t.Fatalf("viewers after the drop = %d, want 1", s.Viewers())
	}
	// The slow one still has its backlog, then its channel is closed.
	if a, b := receive(t, slow), receive(t, slow); a.ID != "p-1" || b.ID != "p-2" {
		t.Fatalf("slow backlog = %s, %s", a.ID, b.ID)
	}
	if _, open := <-slow.Events(); open {
		t.Fatal("a dropped subscriber's channel stays open")
	}
	// Leaving after being dropped is harmless and still says who is left.
	if viewers := s.Unsubscribe(slow); viewers != 1 {
		t.Fatalf("viewers = %d", viewers)
	}
}

func TestUnsubscribeClosesTheChannelAndSaysWhoIsLeft(t *testing.T) {
	s := New("p", 10, 10)
	a, _ := s.Subscribe("")
	b, _ := s.Subscribe("")
	if viewers := s.Unsubscribe(a); viewers != 1 {
		t.Fatalf("viewers = %d", viewers)
	}
	if _, open := <-a.Events(); open {
		t.Fatal("an unsubscribed channel stays open")
	}
	publish(s, "1")
	if event := receive(t, b); event.ID != "p-1" {
		t.Fatalf("remaining subscriber got %+v", event)
	}
	if viewers := s.Unsubscribe(b); viewers != 0 {
		t.Fatalf("viewers = %d", viewers)
	}
}

func TestServeWritesTheReplayThenLiveEventsAsServerSentEvents(t *testing.T) {
	s := New("p", 10, 10)
	publish(s, `{"n":1}`)
	publish(s, `{"n":2}`)
	sub, _ := s.Subscribe("p-1")
	publish(s, `{"n":3}`)
	s.Unsubscribe(sub) // the live event stays buffered; then the stream ends

	recorder := httptest.NewRecorder()
	sub.Serve(context.Background(), recorder, recorder, nil, time.Hour)

	if recorder.Code != http.StatusOK || !recorder.Flushed {
		t.Fatalf("status %d, flushed %v", recorder.Code, recorder.Flushed)
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q", got)
	}
	want := "id: p-2\ndata: {\"n\":2}\n\nid: p-3\ndata: {\"n\":3}\n\n"
	if recorder.Body.String() != want {
		t.Fatalf("body = %q, want %q", recorder.Body.String(), want)
	}
}

// lockedWriter is a streaming response that may be read while it is written.
type lockedWriter struct {
	mu     sync.Mutex
	header http.Header
	body   strings.Builder
	fail   bool
}

func (w *lockedWriter) Header() http.Header { return w.header }
func (w *lockedWriter) WriteHeader(int)     {}
func (w *lockedWriter) Flush()              {}
func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fail {
		return 0, errors.New("connection gone")
	}
	return w.body.Write(p)
}
func (w *lockedWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.String()
}

// serving runs Serve and returns a channel closed when it returns.
func serving(ctx context.Context, sub *Subscriber, w *lockedWriter, closed <-chan struct{}, heartbeat time.Duration) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		sub.Serve(ctx, w, w, closed, heartbeat)
	}()
	return done
}

func ends(t *testing.T, done <-chan struct{}, why string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("Serve did not return %s", why)
	}
}

func TestServeSendsAHeartbeatWhileQuiet(t *testing.T) {
	s := New("p", 10, 10)
	sub, _ := s.Subscribe("")
	w := &lockedWriter{header: http.Header{}}
	ctx, cancel := context.WithCancel(context.Background())
	done := serving(ctx, sub, w, nil, 5*time.Millisecond)

	deadline := time.Now().Add(time.Second)
	for !strings.Contains(w.String(), ": ping\n\n") {
		if time.Now().After(deadline) {
			t.Fatalf("no heartbeat; body = %q", w.String())
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	ends(t, done, "when its request was cancelled")
}

func TestServeEndsWhenItsOwnerCloses(t *testing.T) {
	s := New("p", 10, 10)
	sub, _ := s.Subscribe("")
	closed := make(chan struct{})
	done := serving(context.Background(), sub, &lockedWriter{header: http.Header{}}, closed, time.Hour)
	close(closed)
	ends(t, done, "when its owner closed")
}

func TestServeEndsWhenTheConnectionFailsDuringReplay(t *testing.T) {
	s := New("p", 10, 10)
	publish(s, "1")
	publish(s, "2")
	sub, replay := s.Subscribe("p-1")
	if len(replay) != 1 {
		t.Fatalf("replay = %v", ids(replay))
	}
	w := &lockedWriter{header: http.Header{}, fail: true}
	ends(t, serving(context.Background(), sub, w, nil, time.Hour), "when writing the replay failed")
}

func TestServeEndsWhenTheConnectionFailsOnALiveEvent(t *testing.T) {
	s := New("p", 10, 10)
	sub, _ := s.Subscribe("")
	w := &lockedWriter{header: http.Header{}, fail: true}
	done := serving(context.Background(), sub, w, nil, time.Hour)
	publish(s, "1")
	ends(t, done, "when writing a live event failed")
}
