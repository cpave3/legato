package discordbot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

const testToken = "secret-token-xyz"

// frame is one gateway payload as the fake reads it.
type frame struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
}

// fakeConn is one client connection to the fake gateway.
type fakeConn struct {
	t       *testing.T
	path    string
	query   string
	c       *websocket.Conn
	frames  chan frame
	autoAck atomic.Bool
	done    chan struct{}
	readErr error
}

type fakeGateway struct {
	t     *testing.T
	srv   *httptest.Server
	conns chan *fakeConn
	// gatewayBotStatus is the status /gateway/bot answers with (0 = 200).
	gatewayBotStatus int
	gatewayBotAuth   atomic.Value
	// rest, when set, answers the REST calls under /interactions and
	// /channels.
	rest func(http.ResponseWriter, *http.Request)
}

func newFakeGateway(t *testing.T) *fakeGateway {
	t.Helper()
	gw := &fakeGateway{t: t, conns: make(chan *fakeConn, 8)}
	gw.srv = httptest.NewServer(http.HandlerFunc(gw.serve))
	t.Cleanup(gw.srv.Close)
	return gw
}

func (gw *fakeGateway) wsURL() string { return "ws" + strings.TrimPrefix(gw.srv.URL, "http") }

func (gw *fakeGateway) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/gateway/bot" {
		gw.gatewayBotAuth.Store(r.Header.Get("Authorization"))
		if gw.gatewayBotStatus != 0 {
			w.WriteHeader(gw.gatewayBotStatus)
			_, _ = w.Write([]byte(`{"message":"401: Unauthorized","code":0}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"url": gw.wsURL(), "shards": 1})
		return
	}
	if gw.rest != nil && (strings.HasPrefix(r.URL.Path, "/interactions/") || strings.HasPrefix(r.URL.Path, "/channels/")) {
		gw.rest(w, r)
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	c.SetReadLimit(1 << 20)
	fc := &fakeConn{t: gw.t, path: r.URL.Path, query: r.URL.RawQuery, c: c, frames: make(chan frame, 256), done: make(chan struct{})}
	fc.autoAck.Store(true)
	go fc.read()
	gw.conns <- fc
	<-fc.done
}

func (fc *fakeConn) read() {
	defer close(fc.done)
	defer close(fc.frames)
	for {
		_, data, err := fc.c.Read(context.Background())
		if err != nil {
			fc.readErr = err
			return
		}
		var f frame
		if err := json.Unmarshal(data, &f); err != nil {
			fc.t.Errorf("client sent bad JSON: %s", data)
			continue
		}
		if f.Op == 1 && fc.autoAck.Load() {
			_ = fc.c.Write(context.Background(), websocket.MessageText, []byte(`{"op":11}`))
		}
		select {
		case fc.frames <- f:
		default:
		}
	}
}

func (gw *fakeGateway) accept() *fakeConn {
	gw.t.Helper()
	select {
	case fc := <-gw.conns:
		return fc
	case <-time.After(2 * time.Second):
		gw.t.Fatal("client never connected")
		return nil
	}
}

func (fc *fakeConn) send(op int, d any, s int64, typ string) {
	fc.t.Helper()
	p := map[string]any{"op": op, "d": d}
	if typ != "" {
		p["t"] = typ
		p["s"] = s
	}
	b, _ := json.Marshal(p)
	if err := fc.c.Write(context.Background(), websocket.MessageText, b); err != nil {
		fc.t.Fatalf("fake write: %v", err)
	}
}

func (fc *fakeConn) hello(intervalMS int) {
	fc.send(10, map[string]any{"heartbeat_interval": intervalMS}, 0, "")
}

func (fc *fakeConn) dispatch(s int64, typ string, d any) { fc.send(0, d, s, typ) }

// expect waits for a frame with op, skipping heartbeats unless op is 1.
func (fc *fakeConn) expect(op int) frame {
	fc.t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case f, ok := <-fc.frames:
			if !ok {
				fc.t.Fatalf("connection ended waiting for op %d: %v", op, fc.readErr)
			}
			if f.Op == op {
				return f
			}
			if f.Op == 1 {
				continue
			}
			fc.t.Fatalf("got op %d, want op %d (%s)", f.Op, op, f.D)
		case <-timeout:
			fc.t.Fatalf("timed out waiting for op %d", op)
		}
	}
}

// closed waits for the connection to end and returns the client's close status.
func (fc *fakeConn) closed() websocket.StatusCode {
	fc.t.Helper()
	select {
	case <-fc.done:
		return websocket.CloseStatus(fc.readErr)
	case <-time.After(3 * time.Second):
		fc.t.Fatal("connection did not end")
		return -1
	}
}

func newTestClient(gw *fakeGateway) *Client {
	return &Client{
		Token:              testToken,
		APIBase:            gw.srv.URL,
		GatewayURL:         gw.wsURL() + "/gateway",
		Jitter:             func() float64 { return 0 },
		InvalidSessionWait: func() time.Duration { return 0 },
	}
}

type recorder struct {
	msgc   chan Message
	readyc chan string
}

func newRecorder() *recorder {
	return &recorder{msgc: make(chan Message, 16), readyc: make(chan string, 16)}
}

func (r *recorder) onMessage(m Message) { r.msgc <- m }
func (r *recorder) onReady(id string)   { r.readyc <- id }

func (r *recorder) message(t *testing.T) Message {
	t.Helper()
	select {
	case m := <-r.msgc:
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("no message delivered")
		return Message{}
	}
}

func (r *recorder) ready(t *testing.T) string {
	t.Helper()
	select {
	case id := <-r.readyc:
		return id
	case <-time.After(2 * time.Second):
		t.Fatal("onReady not called")
		return ""
	}
}

func startRun(ctx context.Context, c *Client, rec *recorder) chan error {
	errc := make(chan error, 1)
	go func() { errc <- c.Run(ctx, rec.onMessage, rec.onReady) }()
	return errc
}

func waitRun(t *testing.T, errc chan error) error {
	t.Helper()
	select {
	case err := <-errc:
		if err != nil && strings.Contains(err.Error(), testToken) {
			t.Fatalf("error leaks the token: %v", err)
		}
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return")
		return nil
	}
}

func readyPayload(gw *fakeGateway) map[string]any {
	return map[string]any{
		"v":                  10,
		"session_id":         "sess-1",
		"resume_gateway_url": gw.wsURL() + "/resume",
		"user":               map[string]any{"id": "bot-1", "bot": true},
		"guilds":             []any{},
	}
}

func decode(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return m
}

// identifyAndReady runs a session up to READY and returns the connection.
func identifyAndReady(t *testing.T, gw *fakeGateway, rec *recorder, s int64) *fakeConn {
	t.Helper()
	fc := gw.accept()
	fc.hello(1000)
	fc.expect(2)
	fc.dispatch(s, "READY", readyPayload(gw))
	if id := rec.ready(t); id != "bot-1" {
		t.Fatalf("onReady(%q), want bot-1", id)
	}
	return fc
}

func TestIdentifyReadyAndMessageCreate(t *testing.T) {
	gw := newFakeGateway(t)
	c := newTestClient(gw)
	c.GatewayURL = "" // asked of /gateway/bot
	rec := newRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := startRun(ctx, c, rec)

	fc := gw.accept()
	if got := gw.gatewayBotAuth.Load(); got != "Bot "+testToken {
		t.Fatalf("/gateway/bot Authorization = %v", got)
	}
	if !strings.Contains(fc.query, "v=10") || !strings.Contains(fc.query, "encoding=json") {
		t.Fatalf("query = %q", fc.query)
	}
	fc.hello(1000)
	id := decode(t, fc.expect(2).D)
	if id["token"] != testToken {
		t.Fatalf("identify token = %v", id["token"])
	}
	if int(id["intents"].(float64)) != DefaultIntents {
		t.Fatalf("intents = %v, want %d", id["intents"], DefaultIntents)
	}
	props := id["properties"].(map[string]any)
	if props["browser"] != "legato" || props["device"] != "legato" || props["os"] == "" {
		t.Fatalf("properties = %v", props)
	}
	fc.dispatch(1, "READY", readyPayload(gw))
	if got := rec.ready(t); got != "bot-1" {
		t.Fatalf("ready %q", got)
	}
	if c.SelfID() != "bot-1" {
		t.Fatalf("SelfID = %q", c.SelfID())
	}
	fc.dispatch(2, "MESSAGE_CREATE", map[string]any{
		"id": "m1", "channel_id": "c1", "guild_id": "g1",
		"author":            map[string]any{"id": "u1", "bot": true},
		"content":           "hello <@bot-1>",
		"mentions":          []any{map[string]any{"id": "bot-1"}, map[string]any{"id": "u2"}},
		"message_reference": map[string]any{"message_id": "m0", "channel_id": "c1"},
		"timestamp":         "2026-11-24T10:00:00.123000+00:00",
	})
	m := rec.message(t)
	want := Message{ID: "m1", ChannelID: "c1", GuildID: "g1", AuthorID: "u1", AuthorIsBot: true, Content: "hello <@bot-1>", ReferenceID: "m0"}
	if m.ID != want.ID || m.ChannelID != want.ChannelID || m.GuildID != want.GuildID || m.AuthorID != want.AuthorID ||
		m.AuthorIsBot != want.AuthorIsBot || m.Content != want.Content || m.ReferenceID != want.ReferenceID || m.InThread {
		t.Fatalf("message = %+v", m)
	}
	if strings.Join(m.Mentions, ",") != "bot-1,u2" {
		t.Fatalf("mentions = %v", m.Mentions)
	}
	if !m.Timestamp.Equal(time.Date(2026, 11, 24, 10, 0, 0, 123000000, time.UTC)) {
		t.Fatalf("timestamp = %v", m.Timestamp)
	}
	// A DM: no guild.
	fc.dispatch(3, "MESSAGE_CREATE", map[string]any{"id": "m2", "channel_id": "dm", "author": map[string]any{"id": "u1"}, "content": "hi"})
	if m := rec.message(t); m.GuildID != "" || m.AuthorIsBot {
		t.Fatalf("dm = %+v", m)
	}
	cancel()
	if err := waitRun(t, errc); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
}

func TestGatewayBotUnauthorized(t *testing.T) {
	gw := newFakeGateway(t)
	gw.gatewayBotStatus = http.StatusUnauthorized
	c := newTestClient(gw)
	c.GatewayURL = ""
	err := waitRun(t, startRun(context.Background(), c, newRecorder()))
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("Run = %v, want ErrAuth", err)
	}
}

func TestMissedAckReconnectsAndResumes(t *testing.T) {
	gw := newFakeGateway(t)
	c := newTestClient(gw)
	rec := newRecorder()
	errc := startRun(context.Background(), c, rec)

	fc := gw.accept()
	fc.hello(20)
	fc.expect(2)
	fc.dispatch(5, "READY", readyPayload(gw))
	rec.ready(t)
	hb := fc.expect(1)
	// The beat may have been sent before READY's seq was seen; wait for one that carries it.
	for string(hb.D) != "5" {
		hb = fc.expect(1)
	}
	fc.dispatch(6, "TYPING_START", map[string]any{})
	for string(hb.D) != "6" {
		hb = fc.expect(1)
	}
	fc.autoAck.Store(false)
	if err := waitRun(t, errc); !errors.Is(err, ErrReconnect) {
		t.Fatalf("Run = %v, want ErrReconnect", err)
	}
	if code := fc.closed(); code == websocket.StatusNormalClosure || code == websocket.StatusGoingAway {
		t.Fatalf("closed with %v; the session would not be resumable", code)
	}

	errc = startRun(context.Background(), c, rec)
	fc2 := gw.accept()
	if fc2.path != "/resume" {
		t.Fatalf("resumed at %q, want /resume", fc2.path)
	}
	if !strings.Contains(fc2.query, "v=10") {
		t.Fatalf("resume query = %q", fc2.query)
	}
	fc2.hello(1000)
	r := decode(t, fc2.expect(6).D)
	if r["token"] != testToken || r["session_id"] != "sess-1" || r["seq"].(float64) != 6 {
		t.Fatalf("resume = %v", r)
	}
	fc2.dispatch(7, "RESUMED", nil)
	if id := rec.ready(t); id != "bot-1" {
		t.Fatalf("onReady after resume = %q", id)
	}
	_ = fc2.c.Close(websocket.StatusCode(4000), "bye")
	err := waitRun(t, errc)
	if err == nil || errors.Is(err, ErrReconnect) || errors.Is(err, ErrAuth) {
		t.Fatalf("Run after 4000 = %v, want a plain error", err)
	}
	// The session is kept: the next run resumes.
	errc = startRun(context.Background(), c, rec)
	fc3 := gw.accept()
	if fc3.path != "/resume" {
		t.Fatalf("after 4000 dialled %q, want /resume", fc3.path)
	}
	_ = fc3.c.Close(websocket.StatusCode(4004), "auth")
	waitRun(t, errc)
}

func TestReconnectOpResumes(t *testing.T) {
	gw := newFakeGateway(t)
	c := newTestClient(gw)
	rec := newRecorder()
	errc := startRun(context.Background(), c, rec)
	fc := identifyAndReady(t, gw, rec, 3)
	fc.send(7, nil, 0, "")
	if err := waitRun(t, errc); !errors.Is(err, ErrReconnect) {
		t.Fatalf("Run = %v, want ErrReconnect", err)
	}
	if code := fc.closed(); code == websocket.StatusNormalClosure || code == websocket.StatusGoingAway {
		t.Fatalf("closed with %v", code)
	}
	errc = startRun(context.Background(), c, rec)
	fc2 := gw.accept()
	fc2.hello(1000)
	r := decode(t, fc2.expect(6).D)
	if r["session_id"] != "sess-1" || r["seq"].(float64) != 3 {
		t.Fatalf("resume = %v", r)
	}
	_ = fc2.c.Close(websocket.StatusCode(4004), "auth")
	waitRun(t, errc)
}

func TestInvalidSession(t *testing.T) {
	for _, tc := range []struct {
		name      string
		resumable bool
		wantOp    int
	}{
		{"not resumable identifies", false, 2},
		{"resumable resumes", true, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gw := newFakeGateway(t)
			c := newTestClient(gw)
			waited := false
			c.InvalidSessionWait = func() time.Duration { waited = true; return 0 }
			rec := newRecorder()
			errc := startRun(context.Background(), c, rec)
			fc := identifyAndReady(t, gw, rec, 4)
			fc.send(9, tc.resumable, 0, "")
			if err := waitRun(t, errc); !errors.Is(err, ErrReconnect) {
				t.Fatalf("Run = %v, want ErrReconnect", err)
			}
			if waited == tc.resumable {
				t.Fatalf("waited = %v", waited)
			}
			errc = startRun(context.Background(), c, rec)
			fc2 := gw.accept()
			fc2.hello(1000)
			fc2.expect(tc.wantOp)
			wantPath := "/gateway"
			if tc.resumable {
				wantPath = "/resume"
			}
			if fc2.path != wantPath {
				t.Fatalf("dialled %q, want %q", fc2.path, wantPath)
			}
			_ = fc2.c.Close(websocket.StatusCode(4004), "auth")
			waitRun(t, errc)
		})
	}
}

func TestServerHeartbeatRequest(t *testing.T) {
	gw := newFakeGateway(t)
	c := newTestClient(gw)
	c.Jitter = func() float64 { return 0.99 } // no scheduled beat during the test
	rec := newRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := startRun(ctx, c, rec)
	fc := identifyAndReady(t, gw, rec, 9)
	fc.send(1, nil, 0, "")
	start := time.Now()
	if hb := fc.expect(1); string(hb.D) != "9" {
		t.Fatalf("heartbeat d = %s", hb.D)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("heartbeat was not sent at once")
	}
	cancel()
	waitRun(t, errc)
}

func TestCloseCodes(t *testing.T) {
	for _, tc := range []struct {
		code int
		want error
	}{
		{4004, ErrAuth},
		{4013, ErrIntents},
		{4014, ErrIntents},
		{4010, ErrConfig},
		{4012, ErrConfig},
	} {
		gw := newFakeGateway(t)
		c := newTestClient(gw)
		errc := startRun(context.Background(), c, newRecorder())
		fc := gw.accept()
		fc.hello(1000)
		fc.expect(2)
		_ = fc.c.Close(websocket.StatusCode(tc.code), "no")
		err := waitRun(t, errc)
		if !errors.Is(err, tc.want) {
			t.Fatalf("close %d: Run = %v, want %v", tc.code, err, tc.want)
		}
		if tc.want == ErrIntents && (!strings.Contains(err.Error(), "Message Content") || !strings.Contains(err.Error(), "Developer Portal")) {
			t.Fatalf("intents error does not tell the operator what to do: %v", err)
		}
	}
}

func TestSessionTimeoutIdentifiesAgain(t *testing.T) {
	gw := newFakeGateway(t)
	c := newTestClient(gw)
	rec := newRecorder()
	errc := startRun(context.Background(), c, rec)
	fc := identifyAndReady(t, gw, rec, 2)
	_ = fc.c.Close(websocket.StatusCode(4009), "timed out")
	if err := waitRun(t, errc); !errors.Is(err, ErrReconnect) {
		t.Fatalf("Run = %v, want ErrReconnect", err)
	}
	errc = startRun(context.Background(), c, rec)
	fc2 := gw.accept()
	if fc2.path != "/gateway" {
		t.Fatalf("dialled %q", fc2.path)
	}
	fc2.hello(1000)
	fc2.expect(2)
	_ = fc2.c.Close(websocket.StatusCode(4004), "auth")
	waitRun(t, errc)
}

func TestThreadTracking(t *testing.T) {
	gw := newFakeGateway(t)
	c := newTestClient(gw)
	rec := newRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := startRun(ctx, c, rec)
	fc := identifyAndReady(t, gw, rec, 1)

	msg := func(s int64, ch string) Message {
		fc.dispatch(s, "MESSAGE_CREATE", map[string]any{"id": "m", "channel_id": ch, "guild_id": "g", "author": map[string]any{"id": "u"}})
		return rec.message(t)
	}
	if msg(2, "X").InThread {
		t.Fatal("X is not a thread yet")
	}
	fc.dispatch(3, "THREAD_CREATE", map[string]any{"id": "X", "type": 11, "parent_id": "c"})
	if !msg(4, "X").InThread {
		t.Fatal("X should be a thread after THREAD_CREATE")
	}
	fc.dispatch(5, "GUILD_CREATE", map[string]any{"id": "g", "threads": []any{map[string]any{"id": "Y", "type": 12}}})
	if !msg(6, "Y").InThread {
		t.Fatal("Y should be a thread after GUILD_CREATE")
	}
	fc.dispatch(7, "THREAD_LIST_SYNC", map[string]any{"guild_id": "g", "threads": []any{map[string]any{"id": "Z", "type": 10}}})
	if !msg(8, "Z").InThread {
		t.Fatal("Z should be a thread after THREAD_LIST_SYNC")
	}
	fc.dispatch(9, "THREAD_DELETE", map[string]any{"id": "X", "type": 11})
	if msg(10, "X").InThread {
		t.Fatal("X was deleted")
	}
	// A thread message carries position.
	fc.dispatch(11, "MESSAGE_CREATE", map[string]any{"id": "m", "channel_id": "W", "guild_id": "g", "position": 3, "author": map[string]any{"id": "u"}})
	if !rec.message(t).InThread {
		t.Fatal("a message with position is in a thread")
	}
	cancel()
	waitRun(t, errc)
}

func TestCancelReturnsPromptlyAndClosesNormally(t *testing.T) {
	gw := newFakeGateway(t)
	c := newTestClient(gw)
	rec := newRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	errc := startRun(ctx, c, rec)
	fc := identifyAndReady(t, gw, rec, 1)
	start := time.Now()
	cancel()
	if err := waitRun(t, errc); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("Run took %v to return", time.Since(start))
	}
	if code := fc.closed(); code != websocket.StatusNormalClosure {
		t.Fatalf("closed with %v, want 1000", code)
	}
	// Closing with 1000 ends the session: the next run identifies.
	errc = startRun(context.Background(), c, rec)
	fc2 := gw.accept()
	if fc2.path != "/gateway" {
		t.Fatalf("dialled %q", fc2.path)
	}
	fc2.hello(1000)
	fc2.expect(2)
	_ = fc2.c.Close(websocket.StatusCode(4004), "auth")
	waitRun(t, errc)
}

func TestCancelBeforeHello(t *testing.T) {
	gw := newFakeGateway(t)
	c := newTestClient(gw)
	ctx, cancel := context.WithCancel(context.Background())
	errc := startRun(ctx, c, newRecorder())
	gw.accept()
	cancel()
	if err := waitRun(t, errc); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
}

func TestIntentsFromNames(t *testing.T) {
	got, err := IntentsFromNames([]string{"guilds", "Message_Content"})
	if err != nil || got != IntentGuilds|IntentMessageContent {
		t.Fatalf("got %d, %v", got, err)
	}
	if _, err := IntentsFromNames([]string{"presence"}); err == nil {
		t.Fatal("unknown intent accepted")
	}
}
