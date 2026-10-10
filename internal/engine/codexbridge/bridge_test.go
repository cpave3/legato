package codexbridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

const appServerToken = "app-server-secret"

// appServer stands in for `codex app-server`: the bridge is one of its
// clients, as the Codex terminal UI is another. A test speaks for the server:
// it announces what the terminal UI's thread does and answers the bridge's
// calls.
type appServer struct {
	t      *testing.T
	server *httptest.Server

	mu       sync.Mutex
	conn     *websocket.Conn
	replies  map[string]func(params map[string]any) (any, *rpcError)
	calls    []call
	arrived  chan struct{}
	answers  chan frame
	rejected int
}

type call struct {
	Method string
	Params map[string]any
}

func newAppServer(t *testing.T) *appServer {
	t.Helper()
	s := &appServer{
		t:       t,
		replies: map[string]func(map[string]any) (any, *rpcError){},
		arrived: make(chan struct{}, 256),
		answers: make(chan frame, 16),
	}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+appServerToken {
			s.mu.Lock()
			s.rejected++
			s.mu.Unlock()
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		conn.SetReadLimit(64 << 20)
		s.mu.Lock()
		s.conn = conn
		s.mu.Unlock()
		for {
			_, data, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			var incoming frame
			if json.Unmarshal(data, &incoming) != nil {
				continue
			}
			if incoming.Method == "" {
				// The bridge answering a request the server made of it.
				s.answers <- incoming
				continue
			}
			var params map[string]any
			_ = json.Unmarshal(incoming.Params, &params)
			s.mu.Lock()
			s.calls = append(s.calls, call{Method: incoming.Method, Params: params})
			reply := s.replies[incoming.Method]
			s.mu.Unlock()
			s.arrived <- struct{}{}
			if len(incoming.ID) == 0 {
				continue
			}
			var result any = map[string]any{}
			var failure *rpcError
			if reply != nil {
				result, failure = reply(params)
			}
			s.write(map[string]any{"id": incoming.ID, "result": result, "error": failure})
		}
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *appServer) url() string {
	return "ws" + strings.TrimPrefix(s.server.URL, "http")
}

func (s *appServer) write(value map[string]any) {
	s.t.Helper()
	if value["error"] == (*rpcError)(nil) {
		delete(value, "error")
	} else if _, failed := value["error"]; failed {
		delete(value, "result")
	}
	data, err := json.Marshal(value)
	if err != nil {
		s.t.Fatal(err)
	}
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		s.t.Fatal("the bridge is not connected")
	}
	if err := conn.Write(context.Background(), websocket.MessageText, data); err != nil {
		s.t.Fatalf("writing to the bridge: %v", err)
	}
}

// hangUp is the app server going away, as when its session ends.
func (s *appServer) hangUp() {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn != nil {
		_ = conn.Close(websocket.StatusGoingAway, "")
	}
}

// on sets how the server answers one of the bridge's calls from here on.
func (s *appServer) on(method string, reply func(params map[string]any) (any, *rpcError)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replies[method] = reply
}

// notify is the server telling its clients something happened.
func (s *appServer) notify(method string, params map[string]any) {
	s.t.Helper()
	s.write(map[string]any{"method": method, "params": params})
}

// ask is the server asking its clients to decide something. It returns once
// the request is written; the bridge's answer, if it gives one, is read with
// answer.
func (s *appServer) ask(id int, method string, params map[string]any) {
	s.t.Helper()
	s.write(map[string]any{"id": id, "method": method, "params": params})
}

func (s *appServer) answer() frame {
	s.t.Helper()
	select {
	case answered := <-s.answers:
		return answered
	case <-time.After(2 * time.Second):
		s.t.Fatal("the bridge never answered the server's request")
		return frame{}
	}
}

// called waits for the bridge to have made a call and returns its latest one.
func (s *appServer) called(method string) map[string]any {
	s.t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		s.mu.Lock()
		for index := len(s.calls) - 1; index >= 0; index-- {
			if s.calls[index].Method == method {
				params := s.calls[index].Params
				s.mu.Unlock()
				return params
			}
		}
		s.mu.Unlock()
		select {
		case <-s.arrived:
		case <-deadline:
			s.t.Fatalf("the bridge never called %s", method)
			return nil
		}
	}
}

func (s *appServer) count(method string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, made := range s.calls {
		if made.Method == method {
			total++
		}
	}
	return total
}

// harness drives a Bridge from both sides: a stand-in Codex app server
// beneath it, and Legato speaking the native agent protocol above it.
type harness struct {
	t      *testing.T
	codex  *appServer
	bridge *Bridge
	server *httptest.Server
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	codex := newAppServer(t)
	bridge := New(Options{
		TaskID:         "task-1",
		AppServerURL:   codex.url(),
		AppServerToken: appServerToken,
		Now:            func() time.Time { return time.UnixMilli(1_700_000_000_000) },
		RejoinEvery:    5 * time.Millisecond,
	})
	if err := bridge.Connect(context.Background()); err != nil {
		t.Fatalf("connecting to the app server: %v", err)
	}
	ctx, stop := context.WithCancel(context.Background())
	go func() { _ = bridge.Run(ctx) }()
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(func() {
		stop()
		bridge.Close()
		server.Close()
	})
	return &harness{t: t, codex: codex, bridge: bridge, server: server}
}

const threadID = "thr-1"

// terminalThread is the thread the Codex terminal UI opens when it attaches.
func terminalThread(extra map[string]any) map[string]any {
	thread := map[string]any{
		"id": threadID, "ephemeral": false, "threadSource": "user", "parentThreadId": nil,
		"cwd": "/work/repo", "model": "gpt-6-astra", "reasoningEffort": "medium",
		"status": map[string]any{"type": "idle"}, "turns": []any{},
	}
	for key, value := range extra {
		thread[key] = value
	}
	return thread
}

// attach is the terminal UI opening its thread. A thread has no saved history
// until its first turn, so the bridge cannot join it yet.
func (h *harness) attach() {
	h.t.Helper()
	h.codex.on("thread/resume", func(map[string]any) (any, *rpcError) {
		return nil, &rpcError{Code: -32600, Message: "no rollout found for thread id " + threadID}
	})
	h.codex.notify("thread/started", map[string]any{"thread": terminalThread(nil)})
	select {
	case <-h.bridge.Attached():
	case <-time.After(2 * time.Second):
		h.t.Fatal("the bridge never followed the terminal's thread")
	}
}

// begin is a turn starting on the terminal's thread: from here the bridge can
// join it and receives everything the turn does.
func (h *harness) begin(turnID string, turns ...any) {
	h.t.Helper()
	joins := h.codex.count("thread/resume")
	turns = append(turns, map[string]any{"id": turnID, "status": "inProgress", "items": []any{}})
	h.codex.on("thread/resume", func(map[string]any) (any, *rpcError) {
		return map[string]any{
			"thread": terminalThread(map[string]any{"status": map[string]any{"type": "active", "activeFlags": []any{}}, "turns": turns}),
			"model":  "gpt-6-astra", "reasoningEffort": "medium",
		}, nil
	})
	h.codex.notify("thread/status/changed", map[string]any{"threadId": threadID, "status": map[string]any{"type": "active", "activeFlags": []any{}}})
	deadline := time.After(2 * time.Second)
	for h.codex.count("thread/resume") == joins || !h.bridge.joined() {
		select {
		case <-deadline:
			h.t.Fatal("the bridge never joined the thread")
		case <-time.After(2 * time.Millisecond):
		}
	}
}

func (h *harness) item(phase string, item map[string]any) {
	h.t.Helper()
	h.codex.notify("item/"+phase, map[string]any{"threadId": threadID, "turnId": "turn-1", "item": item})
}

func (h *harness) finish(turnID, status string) {
	h.t.Helper()
	h.codex.notify("thread/status/changed", map[string]any{"threadId": threadID, "status": map[string]any{"type": "idle"}})
	h.codex.notify("turn/completed", map[string]any{"threadId": threadID, "turn": map[string]any{"id": turnID, "status": status, "items": []any{}}})
}

func userMessage(id, text string) map[string]any {
	return map[string]any{"type": "userMessage", "id": id, "content": []any{map[string]any{"type": "text", "text": text}}}
}

func agentMessage(id, text string) map[string]any {
	return map[string]any{"type": "agentMessage", "id": id, "text": text, "phase": "final_answer"}
}

func (h *harness) getJSON(path string, out any) int {
	h.t.Helper()
	resp, err := http.Get(h.server.URL + path)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatal(err)
	}
	if out != nil && resp.StatusCode < 300 {
		if err := json.Unmarshal(payload, out); err != nil {
			h.t.Fatalf("GET %s: decoding %q: %v", path, payload, err)
		}
	}
	return resp.StatusCode
}

func (h *harness) post(path string, body any) (int, string) {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			h.t.Fatal(err)
		}
		reader = bytes.NewReader(payload)
	}
	resp, err := http.Post(h.server.URL+path, "application/json", reader)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(payload)
}

type snapshot struct {
	ID     string `json:"id"`
	CWD    string `json:"cwd"`
	Status string `json:"status"`
	Model  struct {
		ProviderID string         `json:"providerId"`
		ModelID    string         `json:"modelId"`
		Settings   map[string]any `json:"settings"`
	} `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	} `json:"messages"`
	ToolCalls []struct {
		CallID    string         `json:"callId"`
		Name      string         `json:"name"`
		Args      map[string]any `json:"args"`
		Result    map[string]any `json:"result"`
		StartedAt int64          `json:"startedAt"`
		EndedAt   int64          `json:"endedAt"`
		Display   struct {
			Summary string `json:"summary"`
		} `json:"display"`
	} `json:"toolCalls"`
	PendingPermissions []struct {
		RequestID string `json:"requestId"`
		Tool      string `json:"tool"`
		Target    string `json:"target"`
		Command   string `json:"command"`
		Reason    string `json:"reason"`
		CWD       string `json:"cwd"`
		Options   []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"options"`
	} `json:"pendingPermissions"`
	PendingQuestions []struct {
		RequestID string `json:"requestId"`
		Questions []struct {
			ID          string `json:"id"`
			Header      string `json:"header"`
			Question    string `json:"question"`
			MultiSelect bool   `json:"multiSelect"`
			AllowOther  bool   `json:"allowOther"`
			Options     []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
		} `json:"questions"`
	} `json:"pendingQuestions"`
	ContextWindow     int `json:"contextWindow"`
	UsedContextTokens int `json:"usedContextTokens"`
}

func (h *harness) snapshot() snapshot {
	h.t.Helper()
	var snap snapshot
	if code := h.getJSON("/v1/sessions/task-1", &snap); code != http.StatusOK {
		h.t.Fatalf("GET snapshot = %d", code)
	}
	return snap
}

// subscribe opens the native event stream and returns decoded envelopes.
func (h *harness) subscribe(since string) <-chan map[string]any {
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
	events := make(chan map[string]any, 256)
	go func() {
		defer resp.Body.Close()
		defer close(events)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			var event map[string]any
			if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event) == nil {
				events <- event
			}
		}
	}()
	return events
}

func nextEvent(t *testing.T, events <-chan map[string]any) map[string]any {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("event stream closed")
		}
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for an event")
		return nil
	}
}

// eventOf reads events until one of the wanted type arrives.
func eventOf(t *testing.T, events <-chan map[string]any, kind string) map[string]any {
	t.Helper()
	for {
		if event := nextEvent(t, events); event["type"] == kind {
			return event
		}
	}
}

func TestATurnTypedAtTheTerminalReachesSubscribersAndTheSnapshot(t *testing.T) {
	h := newHarness(t)
	h.attach()
	events := h.subscribe("")

	h.begin("turn-1")
	if started := eventOf(t, events, "session_started"); started["sessionId"] != "task-1" {
		t.Fatalf("started = %v", started)
	}
	h.item("started", userMessage("item-u1", "list the Go files"))
	h.item("completed", userMessage("item-u1", "list the Go files"))
	if said := nextEvent(t, events); said["type"] != "user_message" || said["content"] != "list the Go files" {
		t.Fatalf("event = %v", said)
	}
	h.item("started", agentMessage("item-a1", ""))
	h.codex.notify("item/agentMessage/delta", map[string]any{"threadId": threadID, "turnId": "turn-1", "itemId": "item-a1", "delta": "There are "})
	h.codex.notify("item/agentMessage/delta", map[string]any{"threadId": threadID, "turnId": "turn-1", "itemId": "item-a1", "delta": "three."})
	first, second := nextEvent(t, events), nextEvent(t, events)
	if first["type"] != "assistant_text_delta" || first["delta"] != "There are " || first["id"] != second["id"] || second["delta"] != "three." {
		t.Fatalf("deltas = %v, %v", first, second)
	}
	h.item("completed", agentMessage("item-a1", "There are three."))
	if done := nextEvent(t, events); done["type"] != "assistant_text_done" || done["text"] != "There are three." || done["id"] != first["id"] {
		t.Fatalf("event = %v", done)
	}
	if snap := h.snapshot(); snap.Status != "running" {
		t.Fatalf("status mid-turn = %q", snap.Status)
	}

	h.finish("turn-1", "completed")
	if finished := nextEvent(t, events); finished["type"] != "run_finished" || finished["reason"] != "stop" {
		t.Fatalf("event = %v", finished)
	}

	snap := h.snapshot()
	if snap.ID != "task-1" || snap.CWD != "/work/repo" || snap.Status != "idle" {
		t.Fatalf("snapshot = %+v", snap)
	}
	if snap.Model.ProviderID != "openai" || snap.Model.ModelID != "gpt-6-astra" || snap.Model.Settings["reasoningEffort"] != "medium" {
		t.Fatalf("model = %+v", snap.Model)
	}
	// The panel drops a leading user message (Chimera's launch prompt), so an
	// empty entry holds that slot.
	if len(snap.Messages) != 3 || snap.Messages[1].Role != "user" || snap.Messages[1].Content != "list the Go files" || snap.Messages[2].Role != "assistant" {
		t.Fatalf("messages = %+v", snap.Messages)
	}
}
