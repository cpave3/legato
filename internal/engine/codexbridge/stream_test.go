package codexbridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// These tests pin the native event stream (GET /v1/sessions/{id}/events) as
// Legato and the web panel see it: the SSE frames, the replay cursor, the
// bound on what is kept for replay, and what happens to a subscriber that
// cannot keep up. Events come from the stand-in app server's notifications;
// the tests read only the wire and the snapshot.

// streamFrame is one SSE frame as written: its id line and its data.
type streamFrame struct {
	id   string
	data map[string]any
}

// openStream subscribes and returns the response and its frames, read raw so
// the id line can be checked against the data.
func (h *harness) openStream(since string) (*http.Response, <-chan streamFrame) {
	h.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h.t.Cleanup(cancel)
	path := h.server.URL + "/v1/sessions/task-1/events"
	if since != "" {
		path += "?since=" + since
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		h.t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		h.t.Fatalf("subscribe = %d", resp.StatusCode)
	}
	frames := make(chan streamFrame, 256)
	go func() {
		defer resp.Body.Close()
		defer close(frames)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		var frame streamFrame
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "id: "):
				frame.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "data: "):
				_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &frame.data)
			case line == "" && frame.data != nil:
				frames <- frame
				frame = streamFrame{}
			}
		}
	}()
	return resp, frames
}

func nextFrame(t *testing.T, frames <-chan streamFrame) streamFrame {
	t.Helper()
	select {
	case frame, ok := <-frames:
		if !ok {
			t.Fatal("event stream closed")
		}
		return frame
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a frame")
		return streamFrame{}
	}
}

// cursorParts splits an event ID into the bridge's instance and the event's
// number.
func cursorParts(t *testing.T, id string) (string, int) {
	t.Helper()
	cut := strings.LastIndex(id, "-")
	if cut <= 0 {
		t.Fatalf("event ID %q is not <instance>-<n>", id)
	}
	n, err := strconv.Atoi(id[cut+1:])
	if err != nil {
		t.Fatalf("event ID %q is not <instance>-<n>: %v", id, err)
	}
	return id[:cut], n
}

// replying starts a turn at the terminal and an assistant reply in it, so
// each delta of that reply is one native event.
func (h *harness) replying(itemID string) {
	h.t.Helper()
	h.attach()
	h.begin("turn-1")
	h.item("started", agentMessage(itemID, ""))
}

func (h *harness) delta(itemID, text string) {
	h.t.Helper()
	h.codex.notify("item/agentMessage/delta", map[string]any{"threadId": threadID, "turnId": "turn-1", "itemId": itemID, "delta": text})
}

// settle completes a reply and waits, within limit, until the snapshot shows
// it: the bridge has then handled every notification sent before it. A
// bridge wedged by its own publishing never answers in time.
func (h *harness) settle(itemID, text string, limit time.Duration) {
	h.t.Helper()
	h.item("completed", agentMessage(itemID, text))
	client := &http.Client{Timeout: limit}
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		resp, err := client.Get(h.server.URL + "/v1/sessions/task-1")
		if err != nil {
			h.t.Fatalf("snapshot did not answer within %s: %v", limit, err)
		}
		var snap snapshot
		decodeErr := json.NewDecoder(resp.Body).Decode(&snap)
		resp.Body.Close()
		if decodeErr == nil {
			for _, message := range snap.Messages {
				// A settled reply is a list of parts; its text is one of them.
				parts, _ := json.Marshal(message.Content)
				if message.Role == "assistant" && strings.Contains(string(parts), strconv.Quote(text)) {
					return
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatalf("the reply %q never reached the snapshot", text)
}

func TestEventIDsAreTheSSEIDsAndCountUpPerBridge(t *testing.T) {
	h := newHarness(t)
	h.replying("item-a1")
	resp, frames := h.openStream("")
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q", got)
	}

	h.delta("item-a1", "one")
	h.delta("item-a1", "two")
	h.delta("item-a1", "three")

	var instance string
	var previous int
	for index, want := range []string{"one", "two", "three"} {
		frame := nextFrame(t, frames)
		if frame.data["type"] != "assistant_text_delta" || frame.data["delta"] != want {
			t.Fatalf("frame %d = %v, want the delta %q", index, frame.data, want)
		}
		if frame.id != frame.data["eventId"] {
			t.Fatalf("id line %q differs from eventId %v", frame.id, frame.data["eventId"])
		}
		if frame.data["sessionId"] != "task-1" {
			t.Fatalf("sessionId = %v", frame.data["sessionId"])
		}
		if frame.data["ts"] != float64(1_700_000_000_000) {
			t.Fatalf("ts = %v, want the bridge clock in milliseconds", frame.data["ts"])
		}
		prefix, n := cursorParts(t, frame.id)
		if index == 0 {
			instance = prefix
		} else if prefix != instance || n != previous+1 {
			t.Fatalf("event ID %q does not follow %s-%d", frame.id, instance, previous)
		}
		previous = n
	}
}

func TestAKnownCursorReplaysOnlyWhatFollows(t *testing.T) {
	h := newHarness(t)
	h.replying("item-a1")
	live := h.subscribe("")
	h.delta("item-a1", "first")
	h.delta("item-a1", "second")
	first := nextEvent(t, live)
	nextEvent(t, live)

	replay := h.subscribe(first["eventId"].(string))
	if event := nextEvent(t, replay); event["delta"] != "second" {
		t.Fatalf("replayed %v, want only the event after the cursor", event)
	}
	h.delta("item-a1", "third")
	if event := nextEvent(t, replay); event["delta"] != "third" {
		t.Fatalf("after the replay came %v, want the live event", event)
	}
}

func TestAnUnknownOrEmptyCursorReplaysNothing(t *testing.T) {
	h := newHarness(t)
	h.replying("item-a1")
	// Notifications reach the bridge asynchronously: watch the earlier deltas
	// published before subscribing, or one may arrive after and look live.
	watch := h.subscribe("")
	h.delta("item-a1", "earlier")
	h.delta("item-a1", "also earlier")
	for _, want := range []string{"earlier", "also earlier"} {
		if event := nextEvent(t, watch); event["delta"] != want {
			t.Fatalf("published %v, want the delta %q", event, want)
		}
	}

	for index, cursor := range []string{"", "00000000-1", "not-a-cursor"} {
		events := h.subscribe(cursor)
		live := fmt.Sprintf("live-%d", index)
		h.delta("item-a1", live)
		if event := nextEvent(t, events); event["delta"] != live {
			t.Fatalf("cursor %q: first event = %v, want only the live %q", cursor, event, live)
		}
	}
}

func TestReplayKeepsTheNewest2000(t *testing.T) {
	h := newHarness(t)
	h.replying("item-a1")
	for i := 0; i < 2100; i++ {
		h.delta("item-a1", "x")
	}
	h.settle("item-a1", "bulk done", 10*time.Second)
	h.item("started", agentMessage("item-a2", ""))

	live := h.subscribe("")
	h.delta("item-a2", "sentinel")
	sentinel := nextEvent(t, live)
	if sentinel["delta"] != "sentinel" {
		t.Fatalf("live event = %v", sentinel)
	}
	instance, newest := cursorParts(t, sentinel["eventId"].(string))
	if newest <= 2001 {
		t.Fatalf("only %d events published; the test needs more than 2001", newest)
	}

	// The oldest event kept is the 2000th newest: a cursor there replays the
	// 1999 after it, ending at the newest.
	replay := h.subscribe(fmt.Sprintf("%s-%d", instance, newest-1999))
	for n := newest - 1998; n <= newest; n++ {
		event := nextEvent(t, replay)
		if want := fmt.Sprintf("%s-%d", instance, n); event["eventId"] != want {
			t.Fatalf("replayed %v, want %s", event["eventId"], want)
		}
	}

	// One older has been let go: its cursor is unknown and replays nothing.
	evicted := h.subscribe(fmt.Sprintf("%s-%d", instance, newest-2000))
	h.delta("item-a2", "after")
	if event := nextEvent(t, evicted); event["delta"] != "after" {
		t.Fatalf("evicted cursor replayed %v, want only the live event", event)
	}
}

// stalledWriter is a streaming response whose first event write does not
// return until released, as a viewer whose connection has stopped reading.
type stalledWriter struct {
	header    http.Header
	flushed   chan struct{}
	stalled   chan struct{}
	release   chan struct{}
	flushOnce sync.Once
	stallOnce sync.Once

	mu     sync.Mutex
	status int
	frames int
}

func newStalledWriter() *stalledWriter {
	return &stalledWriter{header: http.Header{}, flushed: make(chan struct{}), stalled: make(chan struct{}), release: make(chan struct{})}
}

func (w *stalledWriter) Header() http.Header { return w.header }

func (w *stalledWriter) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status = status
}

func (w *stalledWriter) Write(p []byte) (int, error) {
	w.stallOnce.Do(func() {
		close(w.stalled)
		<-w.release
	})
	w.mu.Lock()
	defer w.mu.Unlock()
	if bytes.HasPrefix(p, []byte("id: ")) {
		w.frames++
	}
	return len(p), nil
}

// Flush is first called once the stream has subscribed and sent its headers.
func (w *stalledWriter) Flush() { w.flushOnce.Do(func() { close(w.flushed) }) }

func (w *stalledWriter) written() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.frames
}

func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestALaggingSubscriberIsDroppedWithoutBlockingThePublisher(t *testing.T) {
	h := newHarness(t)
	h.replying("item-a1")
	writer := newStalledWriter()
	ctx, cancel := context.WithCancel(context.Background())
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		req := httptest.NewRequest(http.MethodGet, "/v1/sessions/task-1/events", nil).WithContext(ctx)
		h.bridge.Handler().ServeHTTP(writer, req)
	}()
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(writer.release) })
		cancel()
		// A publisher that blocks on this viewer holds the bridge's lock, which
		// the stream needs to leave: report that instead of hanging the run.
		select {
		case <-ended:
		case <-time.After(5 * time.Second):
			t.Error("the stalled stream could not leave: the bridge is still held by its publisher")
		}
	})
	waitFor(t, writer.flushed, "the stream to subscribe")

	h.delta("item-a1", "first")
	waitFor(t, writer.stalled, "the viewer to stall on its first event")

	// The viewer's backlog fills and overflows; the bridge must go on
	// handling the app server and answering while that viewer is stuck.
	for i := 0; i < 2100; i++ {
		h.delta("item-a1", "x")
	}
	h.settle("item-a1", "flood done", 10*time.Second)
	h.item("started", agentMessage("item-a2", ""))
	events := h.subscribe("")
	h.delta("item-a2", "later")
	if event := nextEvent(t, events); event["delta"] != "later" {
		t.Fatalf("a new viewer got %v while another was stuck", event)
	}

	releaseOnce.Do(func() { close(writer.release) })
	waitFor(t, ended, "the dropped stream to end")

	// It wrote the event it stalled on and its full backlog of 2064, nothing
	// after: the number is the behaviour, so it is written out here.
	if got := writer.written(); got != 2065 {
		t.Fatalf("dropped viewer was written %d events, want 2065", got)
	}
}

// plainWriter is a response that cannot be flushed.
type plainWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *plainWriter) Header() http.Header         { return w.header }
func (w *plainWriter) WriteHeader(status int)      { w.status = status }
func (w *plainWriter) Write(p []byte) (int, error) { return w.body.Write(p) }

func TestAWriterThatCannotFlushIsRefused(t *testing.T) {
	h := newHarness(t)
	h.replying("item-a1")

	writer := &plainWriter{header: http.Header{}}
	h.bridge.Handler().ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/v1/sessions/task-1/events", nil))

	if writer.status != http.StatusInternalServerError || strings.TrimSpace(writer.body.String()) != "streaming unsupported" {
		t.Fatalf("response = %d %q", writer.status, writer.body.String())
	}
	events := h.subscribe("")
	h.delta("item-a1", "still streaming")
	if event := nextEvent(t, events); event["delta"] != "still streaming" {
		t.Fatalf("event = %v", event)
	}
}

func TestClosingTheBridgeEndsItsStreams(t *testing.T) {
	h := newHarness(t)
	h.attach()
	events := h.subscribe("")

	h.bridge.Close()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, open := <-events:
			if !open {
				return
			}
		case <-deadline:
			t.Fatal("the stream did not end when the bridge closed")
		}
	}
}
