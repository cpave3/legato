package slackbot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

const (
	appToken = "xapp-1-APPSECRET"
	botToken = "xoxb-BOTSECRET"
)

// fakeSlack serves the Web API methods and a Socket Mode endpoint.
type fakeSlack struct {
	t   *testing.T
	srv *httptest.Server

	mu      sync.Mutex
	methods map[string]func(w http.ResponseWriter, r *http.Request)
	calls   map[string]int
	bodies  map[string][]string
	auths   map[string]string

	// socket drives one Socket Mode session; nil closes at once.
	socket func(ctx context.Context, c *websocket.Conn)
	// closeStatus receives the status the client closed the socket with.
	closeStatus chan websocket.StatusCode
}

func newFake(t *testing.T) *fakeSlack {
	t.Helper()
	f := &fakeSlack{
		t:           t,
		methods:     map[string]func(http.ResponseWriter, *http.Request){},
		calls:       map[string]int{},
		bodies:      map[string][]string{},
		auths:       map[string]string{},
		closeStatus: make(chan websocket.StatusCode, 1),
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	f.handle("apps.connections.open", func(w http.ResponseWriter, r *http.Request) {
		wsURL := "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/link?ticket=SECRET"
		writeJSON(w, map[string]any{"ok": true, "url": wsURL})
	})
	return f
}

func (f *fakeSlack) handle(method string, h func(http.ResponseWriter, *http.Request)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.methods[method] = h
}

func (f *fakeSlack) callCount(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[method]
}

func (f *fakeSlack) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/link" {
		if r.URL.Query().Get("ticket") != "SECRET" {
			http.Error(w, "bad ticket", http.StatusForbidden)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			f.t.Errorf("accept: %v", err)
			return
		}
		defer c.CloseNow()
		if f.socket != nil {
			f.socket(r.Context(), c)
		}
		return
	}
	if r.URL.Path == "/nope" {
		http.Error(w, "no socket here", http.StatusForbidden)
		return
	}
	method := strings.TrimPrefix(r.URL.Path, "/")
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.calls[method]++
	f.bodies[method] = append(f.bodies[method], string(body))
	f.auths[method] = r.Header.Get("Authorization")
	h := f.methods[method]
	f.mu.Unlock()
	if r.Method != http.MethodPost {
		f.t.Errorf("%s: method %s, want POST", method, r.Method)
	}
	if h == nil {
		writeJSON(w, map[string]any{"ok": false, "error": "unknown_method"})
		return
	}
	h(w, r)
}

func (f *fakeSlack) client() *Client {
	return &Client{AppToken: appToken, BotToken: botToken, APIBase: f.srv.URL}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func send(ctx context.Context, t *testing.T, c *websocket.Conn, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Errorf("fake write: %v", err)
	}
}

func readAck(ctx context.Context, t *testing.T, c *websocket.Conn) string {
	t.Helper()
	_, b, err := c.Read(ctx)
	if err != nil {
		t.Errorf("fake read ack: %v", err)
		return ""
	}
	var ack struct {
		EnvelopeID string `json:"envelope_id"`
	}
	if err := json.Unmarshal(b, &ack); err != nil {
		t.Errorf("ack %q: %v", b, err)
	}
	return ack.EnvelopeID
}

func eventsAPI(id string, event map[string]any) map[string]any {
	return map[string]any{
		"type":        "events_api",
		"envelope_id": id,
		"payload":     map[string]any{"type": "event_callback", "event": event},
	}
}

func assertNoSecrets(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	s := err.Error()
	for _, secret := range []string{appToken, botToken, "APPSECRET", "BOTSECRET", "ticket=SECRET", "SECRET"} {
		if strings.Contains(s, secret) {
			t.Errorf("error %q contains secret %q", s, secret)
		}
	}
}

func TestAuthTestReturnsIdentityUsingBotToken(t *testing.T) {
	f := newFake(t)
	f.handle("auth.test", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "user_id": "U1", "bot_id": "B1", "team_id": "T1"})
	})
	id, err := f.client().AuthTest(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if id != (Identity{UserID: "U1", BotID: "B1", TeamID: "T1"}) {
		t.Fatalf("identity = %+v", id)
	}
	if got := f.auths["auth.test"]; got != "Bearer "+botToken {
		t.Fatalf("auth header = %q", got)
	}
}

func TestAuthTestInvalidAuthIsErrAuth(t *testing.T) {
	f := newFake(t)
	f.handle("auth.test", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": false, "error": "invalid_auth"})
	})
	_, err := f.client().AuthTest(t.Context())
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("err = %v, want ErrAuth", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "invalid_auth" || apiErr.Method != "auth.test" {
		t.Fatalf("err = %#v", err)
	}
	assertNoSecrets(t, err)
}

func TestOpenUsesAppTokenAndInvalidAuthIsErrAuth(t *testing.T) {
	f := newFake(t)
	url, err := f.client().Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(url, "/link?ticket=SECRET") {
		t.Fatalf("url = %q", url)
	}
	if got := f.auths["apps.connections.open"]; got != "Bearer "+appToken {
		t.Fatalf("auth header = %q", got)
	}

	f.handle("apps.connections.open", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": false, "error": "invalid_auth"})
	})
	err = f.client().Run(t.Context(), func(Event) {}, nil)
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("Run err = %v, want ErrAuth", err)
	}
	assertNoSecrets(t, err)
}

func TestRunAcksEnvelopeBeforeHandlingEvent(t *testing.T) {
	f := newFake(t)
	var acked atomic.Bool
	f.socket = func(ctx context.Context, c *websocket.Conn) {
		send(ctx, t, c, map[string]any{"type": "hello"})
		send(ctx, t, c, eventsAPI("env-1", map[string]any{
			"type": "message", "channel": "C1", "channel_type": "im", "user": "U2",
			"text": "hi", "ts": "1.1", "thread_ts": "1.0", "event_ts": "1.1",
		}))
		if got := readAck(ctx, t, c); got != "env-1" {
			t.Errorf("ack = %q", got)
		}
		acked.Store(true)
		_ = c.Close(websocket.StatusNormalClosure, "")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var events []Event
	var connected atomic.Int32
	_ = f.client().Run(ctx, func(e Event) {
		// The fake marks the ack after reading it; give it a moment.
		deadline := time.Now().Add(2 * time.Second)
		for !acked.Load() && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if !acked.Load() {
			t.Error("event handled before ack was sent")
		}
		if connected.Load() != 1 {
			t.Error("onConnected not called before the first event")
		}
		events = append(events, e)
	}, func() { connected.Add(1) })
	if connected.Load() != 1 {
		t.Fatalf("onConnected called %d times", connected.Load())
	}
	want := Event{Type: "message", Channel: "C1", ChannelType: "im", User: "U2", Text: "hi", TS: "1.1", ThreadTS: "1.0", EventTS: "1.1"}
	if len(events) != 1 || events[0] != want {
		t.Fatalf("events = %+v", events)
	}
}

func TestRunFiltersEventsAndAcksEveryEnvelope(t *testing.T) {
	f := newFake(t)
	f.socket = func(ctx context.Context, c *websocket.Conn) {
		send(ctx, t, c, map[string]any{"type": "hello"})
		msgs := []map[string]any{
			eventsAPI("e1", map[string]any{"type": "message", "subtype": "message_changed", "channel": "C1", "ts": "1"}),
			eventsAPI("e2", map[string]any{"type": "message", "subtype": "bot_message", "bot_id": "B9", "channel": "C1", "ts": "2"}),
			eventsAPI("e3", map[string]any{"type": "message", "subtype": "thread_broadcast", "channel": "C1", "user": "U1", "text": "tb", "ts": "3", "thread_ts": "0.5"}),
			eventsAPI("e4", map[string]any{"type": "app_mention", "channel": "C2", "user": "U1", "text": "<@UBOT> hi", "ts": "4", "event_ts": "4"}),
			eventsAPI("e5", map[string]any{"type": "reaction_added", "user": "U1"}),
			{"type": "slash_commands", "envelope_id": "e6", "payload": map[string]any{"command": "/x"}},
			eventsAPI("e7", map[string]any{"type": "message", "subtype": "file_share", "channel": "C1", "user": "U1", "text": "f", "ts": "7"}),
		}
		for _, m := range msgs {
			send(ctx, t, c, m)
			want := m["envelope_id"].(string)
			if got := readAck(ctx, t, c); got != want {
				t.Errorf("ack = %q, want %q", got, want)
			}
		}
		_ = c.Close(websocket.StatusNormalClosure, "")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var events []Event
	_ = f.client().Run(ctx, func(e Event) { events = append(events, e) }, nil)
	want := []Event{
		{Type: "message", Subtype: "bot_message", Channel: "C1", BotID: "B9", TS: "2"},
		{Type: "message", Subtype: "thread_broadcast", Channel: "C1", User: "U1", Text: "tb", TS: "3", ThreadTS: "0.5"},
		{Type: "mention", Channel: "C2", User: "U1", Text: "<@UBOT> hi", TS: "4", EventTS: "4"},
		{Type: "message", Subtype: "file_share", Channel: "C1", User: "U1", Text: "f", TS: "7"},
	}
	if len(events) != len(want) {
		t.Fatalf("events = %+v", events)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, events[i], want[i])
		}
	}
}

func TestRunDisconnect(t *testing.T) {
	for _, tc := range []struct {
		reason string
		want   error
	}{
		{"refresh_requested", ErrReconnect},
		{"warning", ErrReconnect},
		{"too_many_websockets", ErrReconnect},
		{"link_disabled", ErrAuth},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			f := newFake(t)
			f.socket = func(ctx context.Context, c *websocket.Conn) {
				send(ctx, t, c, map[string]any{"type": "hello"})
				send(ctx, t, c, map[string]any{"type": "disconnect", "reason": tc.reason})
				_, _, _ = c.Read(ctx)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			err := f.client().Run(ctx, func(Event) {}, nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			assertNoSecrets(t, err)
		})
	}
}

func TestRunReturnsPromptlyOnCancel(t *testing.T) {
	f := newFake(t)
	connected := make(chan struct{})
	f.socket = func(ctx context.Context, c *websocket.Conn) {
		send(ctx, t, c, map[string]any{"type": "hello"})
		_, _, err := c.Read(ctx)
		f.closeStatus <- websocket.CloseStatus(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.client().Run(ctx, func(Event) {}, func() { close(connected) }) }()
	select {
	case <-connected:
	case <-time.After(5 * time.Second):
		t.Fatal("never connected")
	}
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if d := time.Since(start); d > time.Second {
			t.Fatalf("Run took %v to return", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	select {
	case st := <-f.closeStatus:
		if st != websocket.StatusNormalClosure {
			t.Fatalf("close status = %v, want normal closure", st)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fake never saw the close")
	}
}

func TestRunDialFailureDoesNotLeakTicket(t *testing.T) {
	f := newFake(t)
	f.handle("apps.connections.open", func(w http.ResponseWriter, r *http.Request) {
		// A port nothing listens on, then a path the fake refuses.
		writeJSON(w, map[string]any{"ok": true, "url": "ws://127.0.0.1:1/link?ticket=SECRET"})
	})
	err := f.client().Run(t.Context(), func(Event) {}, nil)
	if err == nil {
		t.Fatal("want dial error")
	}
	assertNoSecrets(t, err)

	f.handle("apps.connections.open", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "url": "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/nope?ticket=SECRET"})
	})
	err = f.client().Run(t.Context(), func(Event) {}, nil)
	if err == nil {
		t.Fatal("want handshake error")
	}
	assertNoSecrets(t, err)
}

func TestPostMessage(t *testing.T) {
	f := newFake(t)
	f.handle("chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("content type %q", ct)
		}
		writeJSON(w, map[string]any{"ok": true, "channel": "C1", "ts": "9.9"})
	})
	c := f.client()
	p, err := c.PostMessage(t.Context(), "C1", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	if p != (Posted{TS: "9.9", Channel: "C1"}) {
		t.Fatalf("posted = %+v", p)
	}
	if _, err := c.PostMessage(t.Context(), "C1", "reply", "1.5"); err != nil {
		t.Fatal(err)
	}
	if got := f.auths["chat.postMessage"]; got != "Bearer "+botToken {
		t.Fatalf("auth header = %q", got)
	}
	var bodies []map[string]any
	for _, b := range f.bodies["chat.postMessage"] {
		var m map[string]any
		if err := json.Unmarshal([]byte(b), &m); err != nil {
			t.Fatalf("body %q: %v", b, err)
		}
		bodies = append(bodies, m)
	}
	if len(bodies) != 2 {
		t.Fatalf("bodies = %v", bodies)
	}
	if bodies[0]["channel"] != "C1" || bodies[0]["text"] != "hello" {
		t.Errorf("body 0 = %v", bodies[0])
	}
	if _, ok := bodies[0]["thread_ts"]; ok {
		t.Errorf("body 0 has thread_ts: %v", bodies[0])
	}
	if bodies[1]["thread_ts"] != "1.5" || bodies[1]["text"] != "reply" {
		t.Errorf("body 1 = %v", bodies[1])
	}
}

func TestPostMessageAPIError(t *testing.T) {
	f := newFake(t)
	f.handle("chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": false, "error": "not_in_channel"})
	})
	_, err := f.client().PostMessage(t.Context(), "C1", "x", "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "not_in_channel" {
		t.Fatalf("err = %#v", err)
	}
	if errors.Is(err, ErrAuth) {
		t.Fatal("not_in_channel must not be ErrAuth")
	}
	if f.callCount("chat.postMessage") != 1 {
		t.Fatalf("calls = %d", f.callCount("chat.postMessage"))
	}
	assertNoSecrets(t, err)
}

func TestPostMessageRetriesOnceWhenRateLimited(t *testing.T) {
	for _, retryAfter := range []string{"0", "1"} {
		t.Run("retry-after "+retryAfter, func(t *testing.T) {
			f := newFake(t)
			var n atomic.Int32
			f.handle("chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
				if n.Add(1) == 1 {
					w.Header().Set("Retry-After", retryAfter)
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				writeJSON(w, map[string]any{"ok": true, "channel": "C1", "ts": "2.0"})
			})
			p, err := f.client().PostMessage(t.Context(), "C1", "x", "")
			if err != nil {
				t.Fatal(err)
			}
			if p.TS != "2.0" || n.Load() != 2 {
				t.Fatalf("posted %+v after %d calls", p, n.Load())
			}
		})
	}
}

func TestPostMessageGivesUpOnLongOrRepeatedRateLimit(t *testing.T) {
	f := newFake(t)
	f.handle("chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		writeJSON(w, map[string]any{"ok": false, "error": "ratelimited"})
	})
	_, err := f.client().PostMessage(t.Context(), "C1", "x", "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "ratelimited" || apiErr.RetryAfter != 30*time.Second {
		t.Fatalf("err = %#v", err)
	}
	if f.callCount("chat.postMessage") != 1 {
		t.Fatalf("calls = %d, want 1", f.callCount("chat.postMessage"))
	}

	f2 := newFake(t)
	f2.handle("chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err = f2.client().PostMessage(t.Context(), "C1", "x", "")
	if !errors.As(err, &apiErr) || apiErr.Code != "ratelimited" {
		t.Fatalf("err = %#v", err)
	}
	if f2.callCount("chat.postMessage") != 2 {
		t.Fatalf("calls = %d, want 2", f2.callCount("chat.postMessage"))
	}
}

func TestPostMessageTooLong(t *testing.T) {
	f := newFake(t)
	_, err := f.client().PostMessage(t.Context(), "C1", strings.Repeat("a", MaxTextLength+1), "")
	if !errors.Is(err, ErrTooLong) {
		t.Fatalf("err = %v, want ErrTooLong", err)
	}
	if f.callCount("chat.postMessage") != 0 {
		t.Fatal("made an HTTP call")
	}
}
