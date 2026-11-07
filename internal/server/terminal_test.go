package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

type fakeTerm struct {
	mu       sync.Mutex
	out      chan []byte
	in       []byte
	cols     int
	rows     int
	resizes  [][2]int
	closed   chan struct{}
	closeOne sync.Once
}

func newFakeTerm(cols, rows int) *fakeTerm {
	return &fakeTerm{out: make(chan []byte, 8), cols: cols, rows: rows, closed: make(chan struct{})}
}
func (f *fakeTerm) Read(b []byte) (int, error) {
	select {
	case d := <-f.out:
		return copy(b, d), nil
	case <-f.closed:
		return 0, io.EOF
	}
}
func (f *fakeTerm) Write(b []byte) (int, error) {
	f.mu.Lock()
	f.in = append(f.in, b...)
	f.mu.Unlock()
	return len(b), nil
}
func (f *fakeTerm) Resize(c, r int) error {
	f.mu.Lock()
	f.resizes = append(f.resizes, [2]int{c, r})
	f.mu.Unlock()
	return nil
}
func (f *fakeTerm) Close() error { f.closeOne.Do(func() { close(f.closed) }); return nil }
func (f *fakeTerm) input() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return string(f.in)
}

func dialTerminal(t *testing.T, srv *Server, path string) (*websocket.Conn, context.Context) {
	t.Helper()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+path, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { c.CloseNow() })
	return c, ctx
}

func TestTerminalBridgesBinaryBothWays(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	ft := newFakeTerm(0, 0)
	var gotName string
	var gotCols, gotRows int
	srv.SetTerminalAttacher(func(name string, cols, rows int) (TerminalSession, error) {
		gotName, gotCols, gotRows = name, cols, rows
		return ft, nil
	})

	c, ctx := dialTerminal(t, srv, "/api/agents/abc/terminal?cols=120&rows=40")
	ft.out <- []byte("hello\x1b[31m")
	typ, data, err := c.Read(ctx)
	if err != nil || typ != websocket.MessageBinary || string(data) != "hello\x1b[31m" {
		t.Fatalf("got %v %q %v", typ, data, err)
	}

	if err := c.Write(ctx, websocket.MessageBinary, []byte("\x03ls\r")); err != nil {
		t.Fatal(err)
	}
	if err := c.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":90,"rows":30}`)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		ft.mu.Lock()
		n := len(ft.resizes)
		ft.mu.Unlock()
		if ft.input() == "\x03ls\r" && n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("input=%q resizes=%v", ft.input(), ft.resizes)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if gotName != "legato-abc" || gotCols != 120 || gotRows != 40 || ft.resizes[0] != [2]int{90, 30} {
		t.Fatalf("name=%q size=%dx%d resizes=%v", gotName, gotCols, gotRows, ft.resizes)
	}

	c.Close(websocket.StatusNormalClosure, "")
	select {
	case <-ft.closed:
	case <-time.After(3 * time.Second):
		t.Fatal("terminal not closed when the browser left")
	}
}

func TestTerminalClosesSocketWhenSessionEnds(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	ft := newFakeTerm(0, 0)
	srv.SetTerminalAttacher(func(string, int, int) (TerminalSession, error) { return ft, nil })
	c, ctx := dialTerminal(t, srv, "/api/agents/abc/terminal?cols=80&rows=24")
	ft.Close()
	if _, _, err := c.Read(ctx); err == nil {
		t.Fatal("want the socket closed")
	}
}

func TestTerminalRefusals(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	srv.SetAuthToken("secret")
	srv.SetTerminalAttacher(func(string, int, int) (TerminalSession, error) { return nil, errors.New("no such session") })

	for _, tc := range []struct {
		path string
		want int
	}{
		{"/api/agents/abc/terminal?cols=80&rows=24", 401},
		{"/api/agents/abc/terminal?cols=80&rows=24&token=wrong", 401},
		{"/api/agents/abc/terminal?cols=0&rows=24&token=secret", 400},
		{"/api/agents/abc/terminal?cols=80&rows=24&token=secret", 404},
		{"/api/agents/..%2Fx/terminal?cols=80&rows=24&token=secret", 400},
	} {
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.want {
			t.Errorf("%s: %d, want %d", tc.path, w.Code, tc.want)
		}
	}

	srv2 := New(nil, nil, nil, ":0")
	w := httptest.NewRecorder()
	srv2.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/agents/abc/terminal?cols=80&rows=24", nil))
	if w.Code != 503 {
		t.Errorf("no attacher: %d, want 503", w.Code)
	}
}

// menuTmux is a tmux whose panes all show a menu waiting for an answer.
type menuTmux struct{ mockTmuxManager }

func (menuTmux) Capture(string) (string, error) {
	return "Do you want to proceed?\n❯ 1. Yes\n  2. No", nil
}

// Terminal mode has no mirror stream, so prompt detection runs off the
// terminal's own output: once it settles, every browser hears the prompt.
func TestTerminalOutputAnnouncesPromptToEveryBrowser(t *testing.T) {
	srv := New(nil, nil, &menuTmux{}, ":0")
	ft := newFakeTerm(0, 0)
	srv.SetTerminalAttacher(func(string, int, int) (TerminalSession, error) { return ft, nil })

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	browser, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { browser.CloseNow() })
	if _, _, err := browser.Read(ctx); err != nil { // the agent list sent on connect
		t.Fatal(err)
	}

	term, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/api/agents/abc/terminal?cols=80&rows=24", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { term.CloseNow() })
	ft.out <- []byte("Do you want to proceed?\r\n❯ 1. Yes\r\n  2. No")
	if _, _, err := term.Read(ctx); err != nil {
		t.Fatal(err)
	}

	for {
		_, data, err := browser.Read(ctx)
		if err != nil {
			t.Fatalf("no prompt_state reached the browser: %v", err)
		}
		var msg WSMessage
		if json.Unmarshal(data, &msg) != nil || msg.Type != MsgPromptState {
			continue
		}
		if msg.AgentID != "abc" || msg.Prompt == nil || len(msg.Prompt.Actions) != 2 {
			t.Fatalf("got %+v", msg)
		}
		return
	}
}
