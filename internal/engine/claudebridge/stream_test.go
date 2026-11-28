package claudebridge

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
// cannot keep up. They speak only HTTP to the bridge and read only the wire
// and the commands the mod receives.

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

// rows makes count prompt rows with distinct IDs, each one user message.
func rows(prefix string, count int) []map[string]any {
	out := make([]map[string]any, count)
	for i := range out {
		id := fmt.Sprintf("%s-%d", prefix, i)
		out[i] = userRow(id, id)
	}
	return out
}

// modWithin posts engine events as the mod does, failing instead of hanging
// when the bridge does not answer: a publish that blocks holds the call.
func (h *harness) modWithin(limit time.Duration, events ...map[string]any) {
	h.t.Helper()
	body, err := json.Marshal(map[string]any{"events": events})
	if err != nil {
		h.t.Fatal(err)
	}
	client := &http.Client{Timeout: limit}
	resp, err := client.Post(h.server.URL+"/mod/events", "application/json", bytes.NewReader(body))
	if err != nil {
		h.t.Fatalf("POST /mod/events did not return within %s: %v", limit, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		h.t.Fatalf("POST /mod/events = %d", resp.StatusCode)
	}
}

// noCommandFor fails if the mod receives any command within d.
func (h *harness) noCommandFor(d time.Duration) {
	h.t.Helper()
	select {
	case command := <-h.commands.lines:
		h.t.Fatalf("unexpected command %v", command)
	case <-time.After(d):
	}
}

func TestEventIDsAreTheSSEIDsAndCountUpPerBridge(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
	resp, frames := h.openStream("")
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q", got)
	}

	h.mod(userRow("row-1", "one"), userRow("row-2", "two"), userRow("row-3", "three"))

	var instance string
	var previous int
	for index, want := range []string{"one", "two", "three"} {
		frame := nextFrame(t, frames)
		if frame.data["type"] != "user_message" || frame.data["content"] != want {
			t.Fatalf("frame %d = %v, want the user message %q", index, frame.data, want)
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

func TestAnUnknownOrEmptyCursorReplaysNothing(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil), userRow("row-1", "earlier"), userRow("row-2", "also earlier"))

	for index, cursor := range []string{"", "00000000-1", "not-a-cursor"} {
		events := h.subscribe(cursor)
		live := fmt.Sprintf("live-%d", index)
		h.mod(userRow(live, live))
		if event := nextEvent(t, events); event["content"] != live {
			t.Fatalf("cursor %q: first event = %v, want only the live %q", cursor, event, live)
		}
	}
}

func TestReplayKeepsTheNewest2000(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
	h.mod(rows("bulk", 2100)...)

	live := h.subscribe("")
	h.mod(userRow("sentinel", "sentinel"))
	sentinel := nextEvent(t, live)
	if sentinel["content"] != "sentinel" {
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
	h.mod(userRow("after", "after"))
	if event := nextEvent(t, evicted); event["content"] != "after" {
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
	h.mod(hello(nil))
	writer := newStalledWriter()
	ctx, cancel := context.WithCancel(context.Background())
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		req := httptest.NewRequest(http.MethodGet, "/v1/sessions/task-1/events", nil).WithContext(ctx)
		req.SetBasicAuth("legato", "test-host-decision-key")
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
	if command := h.nextCommand(); command["type"] != "viewers" || command["count"] != float64(1) {
		t.Fatalf("command = %v, want viewers 1", command)
	}

	h.mod(userRow("first", "first"))
	waitFor(t, writer.stalled, "the viewer to stall on its first event")

	// The viewer's backlog fills and overflows; publishing must not wait for it.
	h.modWithin(5*time.Second, rows("flood", 2100)...)

	// A dropped viewer is gone at once: nobody is left to answer a permission,
	// so it goes to the terminal, though the stalled stream has not ended yet.
	if decided := nextAnswer(t, h.askPermission("toolu_1", "Bash", map[string]any{"command": "ls"})); decided["decision"] != "ask" {
		t.Fatalf("answer = %v, want the prompt handed to the terminal", decided)
	}
	h.noCommandFor(50 * time.Millisecond)

	releaseOnce.Do(func() { close(writer.release) })
	waitFor(t, ended, "the dropped stream to end")

	// It wrote the event it stalled on and its full backlog of 2064, nothing
	// after: the number is the behaviour, so it is written out here.
	if got := writer.written(); got != 2065 {
		t.Fatalf("dropped viewer was written %d events, want 2065", got)
	}
	if command := h.nextCommand(); command["type"] != "viewers" || command["count"] != float64(0) {
		t.Fatalf("command = %v, want viewers 0 once the stream ended", command)
	}

	events := h.subscribe("")
	h.mod(userRow("later", "later"))
	if event := nextEvent(t, events); event["content"] != "later" {
		t.Fatalf("a new viewer got %v", event)
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

func TestAWriterThatCannotFlushIsRefusedBeforeSubscribing(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))

	writer := &plainWriter{header: http.Header{}}
	req := httptest.NewRequest(http.MethodGet, "/v1/sessions/task-1/events", nil)
	req.SetBasicAuth("legato", "test-host-decision-key")
	h.bridge.Handler().ServeHTTP(writer, req)

	if writer.status != http.StatusInternalServerError || strings.TrimSpace(writer.body.String()) != "streaming unsupported" {
		t.Fatalf("response = %d %q", writer.status, writer.body.String())
	}
	// It never counted as a viewer: the mod hears nothing and a permission
	// goes to the terminal.
	h.noCommandFor(50 * time.Millisecond)
	if decided := nextAnswer(t, h.askPermission("toolu_1", "Bash", map[string]any{"command": "ls"})); decided["decision"] != "ask" {
		t.Fatalf("answer = %v", decided)
	}
}

func TestClosingTheBridgeEndsItsStreams(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
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
