package claudebridge

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
)

// harness drives a Bridge the way its two peers do: the Claude Code mod posts
// engine events and reads commands, and Legato speaks the native agent
// protocol against the same handler.
type harness struct {
	t        *testing.T
	bridge   *Bridge
	server   *httptest.Server
	commands *commandLog
}

type commandLog struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	lines chan map[string]any
}

func (c *commandLog) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf.Write(p)
	for {
		line, err := c.buf.ReadBytes('\n')
		if err != nil {
			c.buf.Write(line)
			break
		}
		var command map[string]any
		if json.Unmarshal(line, &command) == nil {
			c.lines <- command
		}
	}
	return len(p), nil
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	commands := &commandLog{lines: make(chan map[string]any, 64)}
	bridge := New(Options{
		TaskID:   "task-1",
		Commands: commands,
		Now:      func() time.Time { return time.UnixMilli(1_700_000_000_000) },
	})
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(func() {
		bridge.Close()
		server.Close()
	})
	return &harness{t: t, bridge: bridge, server: server, commands: commands}
}

// mod posts engine events as the Claude Code mod does.
func (h *harness) mod(events ...map[string]any) {
	h.t.Helper()
	body, err := json.Marshal(map[string]any{"events": events})
	if err != nil {
		h.t.Fatal(err)
	}
	resp, err := http.Post(h.server.URL+"/mod/events", "application/json", bytes.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		payload, _ := io.ReadAll(resp.Body)
		h.t.Fatalf("POST /mod/events = %d %s", resp.StatusCode, payload)
	}
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
	Name   string `json:"name"`
	CWD    string `json:"cwd"`
	Status string `json:"status"`
	Mode   string `json:"mode"`
	Model  struct {
		ProviderID string `json:"providerId"`
		ModelID    string `json:"modelId"`
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
	} `json:"pendingPermissions"`
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

func (h *harness) nextCommand() map[string]any {
	h.t.Helper()
	select {
	case command := <-h.commands.lines:
		return command
	case <-time.After(2 * time.Second):
		h.t.Fatal("timed out waiting for a command")
		return nil
	}
}

func hello(extra map[string]any) map[string]any {
	event := map[string]any{
		"type":      "hello",
		"sessionId": "claude-session-1",
		"cwd":       "/work/repo",
		"model":     "claude-opus-5-5",
	}
	for key, value := range extra {
		event[key] = value
	}
	return event
}

func TestSnapshotDescribesTheClaudeSessionUnderItsLegatoTask(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))

	snap := h.snapshot()

	if snap.ID != "task-1" || snap.Name != "task-1" {
		t.Fatalf("session identity = %q/%q, want the Legato task", snap.ID, snap.Name)
	}
	if snap.CWD != "/work/repo" {
		t.Fatalf("cwd = %q", snap.CWD)
	}
	if snap.Model.ProviderID != "anthropic" || snap.Model.ModelID != "claude-opus-5-5" {
		t.Fatalf("model = %+v", snap.Model)
	}
	if snap.Status != "idle" {
		t.Fatalf("status = %q, want idle", snap.Status)
	}
}

func userRow(uuid, text string) map[string]any {
	return map[string]any{
		"type": "row", "uuid": uuid, "door": "prompt",
		"origin": map[string]any{"kind": "composer"},
		"message": map[string]any{
			"type": "user", "role": "user",
			"content": []map[string]any{{"type": "text", "text": text}},
		},
	}
}

func messageText(content any) string {
	if text, ok := content.(string); ok {
		return text
	}
	var out strings.Builder
	parts, _ := content.([]any)
	for _, part := range parts {
		block, _ := part.(map[string]any)
		if block["type"] == "text" {
			text, _ := block["text"].(string)
			out.WriteString(text)
		}
	}
	return out.String()
}

func TestTypedPromptReachesSubscribersAndTheSnapshot(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
	events := h.subscribe("")

	h.mod(userRow("row-1", "fix the flaky test"))

	event := nextEvent(t, events)
	if event["type"] != "user_message" || event["content"] != "fix the flaky test" {
		t.Fatalf("event = %v", event)
	}
	if event["eventId"] == "" || event["sessionId"] != "task-1" {
		t.Fatalf("envelope = %v", event)
	}

	snap := h.snapshot()
	var texts []string
	for index, message := range snap.Messages {
		// The native panel never renders a leading user message (Chimera's is
		// its launch prompt), so a real first prompt must not sit there.
		if index == 0 && message.Role == "user" {
			t.Fatalf("first snapshot message is a user message: %+v", message)
		}
		if message.Role == "user" {
			texts = append(texts, messageText(message.Content))
		}
	}
	if len(texts) != 1 || texts[0] != "fix the flaky test" {
		t.Fatalf("user messages = %v", texts)
	}
}

func TestRowsAreAppliedOnceWhenTheModRepeatsThem(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
	h.mod(userRow("row-1", "hello"), userRow("row-1", "hello"))

	count := 0
	for _, message := range h.snapshot().Messages {
		if message.Role == "user" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("user messages = %d, want 1", count)
	}
}

func TestEngineInjectedUserRowsStayOutOfTheConversation(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
	h.mod(
		map[string]any{
			"type": "row", "uuid": "meta-1", "door": "attachment", "origin": map[string]any{"kind": "engine"},
			"message": map[string]any{"type": "user", "role": "user", "isMeta": true,
				"content": []map[string]any{{"type": "text", "text": "<system-reminder>be careful</system-reminder>"}}},
		},
		map[string]any{
			"type": "row", "uuid": "note-1", "door": "prompt", "origin": map[string]any{"kind": "task-notification"},
			"message": map[string]any{"type": "user", "role": "user",
				"content": []map[string]any{{"type": "text", "text": "<task-notification>done</task-notification>"}}},
		},
	)

	for _, message := range h.snapshot().Messages {
		if message.Role == "user" {
			t.Fatalf("unexpected user message %q", messageText(message.Content))
		}
	}
}

func assistantRow(uuid string, blocks ...map[string]any) map[string]any {
	return map[string]any{
		"type": "row", "uuid": uuid, "door": "response",
		"origin":  map[string]any{"kind": "model", "model": "claude-opus-5-5"},
		"message": map[string]any{"type": "assistant", "role": "assistant", "content": blocks},
	}
}

func textBlock(text string) map[string]any {
	return map[string]any{"type": "text", "text": text}
}

func TestATurnStreamsItsReplyThenSettlesItFromTheStoredRow(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
	events := h.subscribe("")

	h.mod(map[string]any{"type": "turn_start", "turnId": "turn-1"})
	if event := nextEvent(t, events); event["type"] != "session_started" {
		t.Fatalf("turn start event = %v", event)
	}
	if status := h.snapshot().Status; status != "running" {
		t.Fatalf("status during a turn = %q", status)
	}

	h.mod(
		map[string]any{"type": "text_delta", "turnId": "turn-1", "step": 0, "index": 0, "text": "Hel"},
		map[string]any{"type": "text_delta", "turnId": "turn-1", "step": 0, "index": 0, "text": "lo"},
	)
	first, second := nextEvent(t, events), nextEvent(t, events)
	if first["type"] != "assistant_text_delta" || first["delta"] != "Hel" || second["delta"] != "lo" {
		t.Fatalf("deltas = %v, %v", first, second)
	}
	streamID, _ := first["id"].(string)
	if streamID == "" || second["id"] != streamID {
		t.Fatalf("deltas of one block must share a stream id: %v, %v", first["id"], second["id"])
	}

	h.mod(assistantRow("row-a", textBlock("Hello")))
	done := nextEvent(t, events)
	if done["type"] != "assistant_text_done" || done["text"] != "Hello" || done["id"] != streamID {
		t.Fatalf("done = %v, want it to settle stream %q", done, streamID)
	}

	h.mod(map[string]any{"type": "turn_complete", "turnId": "turn-1", "reason": "answer"})
	finished := nextEvent(t, events)
	if finished["type"] != "run_finished" || finished["reason"] != "stop" {
		t.Fatalf("turn end event = %v", finished)
	}

	snap := h.snapshot()
	if snap.Status != "idle" {
		t.Fatalf("status after the turn = %q", snap.Status)
	}
	last := snap.Messages[len(snap.Messages)-1]
	if last.Role != "assistant" || messageText(last.Content) != "Hello" {
		t.Fatalf("last snapshot message = %+v", last)
	}
}

func TestReplyWithoutStreamedPiecesStillAppears(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
	events := h.subscribe("")

	h.mod(assistantRow("row-a", textBlock("Done.")))

	done := nextEvent(t, events)
	if done["type"] != "assistant_text_done" || done["text"] != "Done." || done["id"] == "" {
		t.Fatalf("done = %v", done)
	}
}

func TestInterruptedTurnReportsItsReason(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil), map[string]any{"type": "turn_start", "turnId": "turn-1"})
	events := h.subscribe("")

	h.mod(map[string]any{"type": "turn_complete", "turnId": "turn-1", "reason": "aborted"})

	if event := nextEvent(t, events); event["type"] != "run_finished" || event["reason"] != "aborted" {
		t.Fatalf("event = %v", event)
	}
}

func TestSubscriberReplaysOnlyWhatFollowsItsCursor(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
	live := h.subscribe("")
	h.mod(userRow("row-1", "first"), userRow("row-2", "second"))
	first := nextEvent(t, live)
	nextEvent(t, live)

	cursor, _ := first["eventId"].(string)
	replayed := nextEvent(t, h.subscribe(cursor))

	if replayed["content"] != "second" {
		t.Fatalf("replayed = %v, want only the event after the cursor", replayed)
	}
}

func toolUse(id, name string, input map[string]any) map[string]any {
	return map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}
}

func toolResultRow(uuid, toolUseID, text string, isError bool) map[string]any {
	block := map[string]any{"type": "tool_result", "tool_use_id": toolUseID, "content": text}
	if isError {
		block["is_error"] = true
	}
	return map[string]any{
		"type": "row", "uuid": uuid, "door": "tool-result",
		"origin":  map[string]any{"kind": "tool", "tool": "Bash"},
		"message": map[string]any{"type": "user", "role": "user", "content": []map[string]any{block}},
	}
}

func TestShellCallIsShownAsANativeBashCallWithItsOutput(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
	events := h.subscribe("")

	h.mod(assistantRow("row-a", textBlock("Running the tests."), toolUse("toolu_1", "Bash", map[string]any{"command": "go test ./...", "description": "Run tests"})))
	nextEvent(t, events) // the text
	start := nextEvent(t, events)
	if start["type"] != "tool_call_start" || start["callId"] != "toolu_1" || start["name"] != "bash" {
		t.Fatalf("start = %v", start)
	}
	if args, _ := start["args"].(map[string]any); args["command"] != "go test ./..." {
		t.Fatalf("start args = %v", start["args"])
	}

	h.mod(toolResultRow("row-r", "toolu_1", "ok  \tpkg\t0.1s", false))
	result := nextEvent(t, events)
	if result["type"] != "tool_call_result" || result["callId"] != "toolu_1" {
		t.Fatalf("result = %v", result)
	}
	if payload, _ := result["result"].(map[string]any); payload["stdout"] != "ok  \tpkg\t0.1s" || payload["exit_code"] != float64(0) {
		t.Fatalf("result payload = %v", result["result"])
	}

	snap := h.snapshot()
	if len(snap.ToolCalls) != 1 || snap.ToolCalls[0].CallID != "toolu_1" || snap.ToolCalls[0].Name != "bash" {
		t.Fatalf("tool calls = %+v", snap.ToolCalls)
	}
	if snap.ToolCalls[0].Result["stdout"] != "ok  \tpkg\t0.1s" {
		t.Fatalf("snapshot result = %v", snap.ToolCalls[0].Result)
	}
	// The panel pairs tool calls with the assistant message that made them by
	// counting its tool-call parts, in order.
	last := snap.Messages[len(snap.Messages)-1]
	parts, _ := last.Content.([]any)
	toolParts := 0
	for _, part := range parts {
		if block, _ := part.(map[string]any); block["type"] == "tool-call" {
			toolParts++
		}
	}
	if last.Role != "assistant" || messageText(last.Content) != "Running the tests." || toolParts != 1 {
		t.Fatalf("last message = %+v", last)
	}
}

func TestFailedShellCallCarriesItsExitCode(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil), assistantRow("row-a", toolUse("toolu_1", "Bash", map[string]any{"command": "false"})))

	h.mod(toolResultRow("row-r", "toolu_1", "Exit code 2\nboom", true))

	result := h.snapshot().ToolCalls[0].Result
	if result["exit_code"] != float64(2) || result["stderr"] != "Exit code 2\nboom" {
		t.Fatalf("result = %v", result)
	}
}

func TestFileToolsUseThePanelsArgumentNames(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil), assistantRow("row-a",
		toolUse("toolu_e", "Edit", map[string]any{"file_path": "/work/repo/a.go", "old_string": "x", "new_string": "y"}),
		toolUse("toolu_w", "Write", map[string]any{"file_path": "/work/repo/b.go", "content": "package b"}),
		toolUse("toolu_r", "Read", map[string]any{"file_path": "/work/repo/c.go"}),
	))

	calls := h.snapshot().ToolCalls
	if len(calls) != 3 {
		t.Fatalf("tool calls = %+v", calls)
	}
	if calls[0].Name != "edit" || calls[0].Args["path"] != "/work/repo/a.go" || calls[0].Args["old_string"] != "x" || calls[0].Args["new_string"] != "y" {
		t.Fatalf("edit = %+v", calls[0])
	}
	if calls[1].Name != "write" || calls[1].Args["path"] != "/work/repo/b.go" || calls[1].Args["content"] != "package b" {
		t.Fatalf("write = %+v", calls[1])
	}
	if calls[2].Name != "read" || calls[2].Args["path"] != "/work/repo/c.go" {
		t.Fatalf("read = %+v", calls[2])
	}
}

func TestOtherToolsKeepTheirNameWithAReadableSummary(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil), assistantRow("row-a", toolUse("toolu_g", "Grep", map[string]any{"pattern": "TODO", "path": "internal"})))
	h.mod(toolResultRow("row-r", "toolu_g", "internal/a.go:3: // TODO", false))

	call := h.snapshot().ToolCalls[0]
	if call.Name != "Grep" || call.Display.Summary != "TODO" {
		t.Fatalf("call = %+v", call)
	}
	if call.Result["output"] != "internal/a.go:3: // TODO" {
		t.Fatalf("result = %v", call.Result)
	}
}

func TestToolResultBlocksMayCarryTextParts(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil), assistantRow("row-a", toolUse("toolu_g", "Glob", map[string]any{"pattern": "*.go"})))
	h.mod(map[string]any{
		"type": "row", "uuid": "row-r", "door": "tool-result", "origin": map[string]any{"kind": "tool", "tool": "Glob"},
		"message": map[string]any{"type": "user", "role": "user", "content": []map[string]any{{
			"type": "tool_result", "tool_use_id": "toolu_g",
			"content": []map[string]any{{"type": "text", "text": "a.go\n"}, {"type": "text", "text": "b.go"}},
		}}},
	})

	if output := h.snapshot().ToolCalls[0].Result["output"]; output != "a.go\nb.go" {
		t.Fatalf("output = %q", output)
	}
}

func TestMessageToAnIdleSessionIsSubmittedAsAPrompt(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))

	code, _ := h.post("/v1/sessions/task-1/messages", map[string]any{"content": "run the linter"})

	if code != http.StatusAccepted {
		t.Fatalf("status = %d", code)
	}
	if command := h.nextCommand(); command["type"] != "submit" || command["text"] != "run the linter" {
		t.Fatalf("command = %v", command)
	}
}

func TestMessageDuringATurnJoinsThatTurnInsteadOfBeingRefused(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil), map[string]any{"type": "turn_start", "turnId": "turn-1"})

	// Claude Code can fold a message into a running turn, so a busy session
	// accepts it rather than answering busy as a Chimera session does.
	code, _ := h.post("/v1/sessions/task-1/messages", map[string]any{"content": "also check docs"})

	if code != http.StatusAccepted {
		t.Fatalf("status = %d", code)
	}
	if command := h.nextCommand(); command["type"] != "queue" || command["text"] != "also check docs" {
		t.Fatalf("command = %v", command)
	}
}

func TestAppendedContextNeverStartsATurn(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))

	// Attachments are appended ahead of the message that refers to them.
	code, _ := h.post("/v1/sessions/task-1/messages?append=true", map[string]any{"content": "[auto-attached @a.go]\npackage a"})

	if code != http.StatusAccepted {
		t.Fatalf("status = %d", code)
	}
	if command := h.nextCommand(); command["type"] != "append" || command["text"] != "[auto-attached @a.go]\npackage a" {
		t.Fatalf("command = %v", command)
	}
}

func TestEmptyMessageIsRejected(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))

	if code, _ := h.post("/v1/sessions/task-1/messages", map[string]any{"content": ""}); code != http.StatusBadRequest {
		t.Fatalf("status = %d", code)
	}
}

func TestInterruptAsksTheModToAbortTheTurn(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil), map[string]any{"type": "turn_start", "turnId": "turn-1"})

	code, _ := h.post("/v1/sessions/task-1/interrupt", nil)

	if code != http.StatusNoContent {
		t.Fatalf("status = %d", code)
	}
	if command := h.nextCommand(); command["type"] != "interrupt" {
		t.Fatalf("command = %v", command)
	}
}

func TestModLearnsWhenANativeViewerIsWatching(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))

	h.subscribe("")

	if command := h.nextCommand(); command["type"] != "viewers" || command["count"] != float64(1) {
		t.Fatalf("command = %v", command)
	}
}

func TestRequestsWithoutTheSecretPathAreNotServed(t *testing.T) {
	bridge := New(Options{TaskID: "task-1", Token: "s3cret"})
	server := httptest.NewServer(bridge.Handler())
	defer server.Close()
	defer bridge.Close()

	for path, want := range map[string]int{
		"/v1/sessions/task-1":        http.StatusNotFound,
		"/wrong/v1/sessions/task-1":  http.StatusNotFound,
		"/s3cret/v1/sessions/task-1": http.StatusOK,
	} {
		resp, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("GET %s = %d, want %d", path, resp.StatusCode, want)
		}
	}
}

// askPermission is the mod holding a tool call open while Legato decides. It
// returns the bridge's answer once the request completes.
func (h *harness) askPermission(id, tool string, input map[string]any) <-chan map[string]any {
	h.t.Helper()
	answer := make(chan map[string]any, 1)
	go func() {
		defer close(answer)
		body, _ := json.Marshal(map[string]any{"requestId": id, "tool": tool, "input": input, "reason": "not in the allowlist"})
		resp, err := http.Post(h.server.URL+"/mod/permissions", "application/json", bytes.NewReader(body))
		if err != nil {
			return
		}
		defer resp.Body.Close()
		var decoded map[string]any
		if json.NewDecoder(resp.Body).Decode(&decoded) == nil {
			answer <- decoded
		}
	}()
	return answer
}

func nextAnswer(t *testing.T, answer <-chan map[string]any) map[string]any {
	t.Helper()
	select {
	case decoded, ok := <-answer:
		if !ok {
			t.Fatal("permission request failed")
		}
		return decoded
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the permission answer")
		return nil
	}
}

func TestPermissionRequestWaitsForLegatosDecision(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil), map[string]any{"type": "turn_start", "turnId": "turn-1"})
	events := h.subscribe("")

	answer := h.askPermission("toolu_1", "Bash", map[string]any{"command": "git push --force"})

	request := nextEvent(t, events)
	if request["type"] != "permission_request" || request["requestId"] != "toolu_1" || request["tool"] != "Bash" {
		t.Fatalf("request = %v", request)
	}
	if request["target"] != "host" || request["command"] != "git push --force" || request["reason"] != "not in the allowlist" {
		t.Fatalf("request = %v", request)
	}
	snap := h.snapshot()
	if snap.Status != "waiting_for_permission" {
		t.Fatalf("status = %q", snap.Status)
	}
	if len(snap.PendingPermissions) != 1 || snap.PendingPermissions[0].RequestID != "toolu_1" || snap.PendingPermissions[0].Command != "git push --force" || snap.PendingPermissions[0].CWD != "/work/repo" {
		t.Fatalf("pending = %+v", snap.PendingPermissions)
	}

	code, _ := h.post("/v1/sessions/task-1/permissions/toolu_1", map[string]any{"decision": "deny", "feedback": "never force push"})
	if code != http.StatusNoContent {
		t.Fatalf("resolve status = %d", code)
	}

	decided := nextAnswer(t, answer)
	if decided["decision"] != "deny" || decided["feedback"] != "never force push" {
		t.Fatalf("answer to the mod = %v", decided)
	}
	resolved := nextEvent(t, events)
	if resolved["type"] != "permission_resolved" || resolved["requestId"] != "toolu_1" || resolved["decision"] != "deny" {
		t.Fatalf("resolved = %v", resolved)
	}
	snap = h.snapshot()
	if len(snap.PendingPermissions) != 0 || snap.Status != "running" {
		t.Fatalf("after resolve: status %q pending %+v", snap.Status, snap.PendingPermissions)
	}
}

func TestPermissionGoesToTheTerminalWhenNobodyIsWatching(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))

	decided := nextAnswer(t, h.askPermission("toolu_1", "Bash", map[string]any{"command": "ls"}))

	// "ask" hands the prompt back to Claude Code's own dialog.
	if decided["decision"] != "ask" {
		t.Fatalf("answer = %v, want ask", decided)
	}
	if pending := h.snapshot().PendingPermissions; len(pending) != 0 {
		t.Fatalf("pending = %+v", pending)
	}
}

func TestDecisionForAnUnknownPermissionIsNotFound(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))

	if code, _ := h.post("/v1/sessions/task-1/permissions/nope", map[string]any{"decision": "allow"}); code != http.StatusNotFound {
		t.Fatalf("status = %d", code)
	}
}

func TestPermissionFallsBackToTheTerminalWhenTheLastViewerLeaves(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
	open := func() context.CancelFunc {
		ctx, leave := context.WithCancel(context.Background())
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, h.server.URL+"/v1/sessions/task-1/events", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { leave(); resp.Body.Close() })
		return leave
	}
	firstLeaves, secondLeaves := open(), open()
	h.nextCommand() // viewers: 1
	h.nextCommand() // viewers: 2

	answer := h.askPermission("toolu_1", "Bash", map[string]any{"command": "ls"})
	for len(h.snapshot().PendingPermissions) == 0 {
		time.Sleep(5 * time.Millisecond)
	}

	// One of two viewers leaving changes nothing: someone can still answer.
	firstLeaves()
	h.nextCommand() // viewers: 1
	select {
	case decided := <-answer:
		t.Fatalf("answered %v while a viewer was still watching", decided)
	case <-time.After(50 * time.Millisecond):
	}

	secondLeaves()
	if decided := nextAnswer(t, answer); decided["decision"] != "ask" {
		t.Fatalf("answer = %v, want the prompt handed back to the terminal", decided)
	}
	if pending := h.snapshot().PendingPermissions; len(pending) != 0 {
		t.Fatalf("pending = %+v", pending)
	}
}

func TestPermissionsLeftOpenWhenTheTurnEndsGoBackToTheTerminal(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil), map[string]any{"type": "turn_start", "turnId": "turn-1"})
	events := h.subscribe("")
	answer := h.askPermission("toolu_1", "Bash", map[string]any{"command": "ls"})
	nextEvent(t, events) // permission_request

	h.mod(map[string]any{"type": "turn_complete", "turnId": "turn-1", "reason": "aborted"})

	if decided := nextAnswer(t, answer); decided["decision"] != "ask" {
		t.Fatalf("answer = %v", decided)
	}
	if event := nextEvent(t, events); event["type"] != "permission_timeout" || event["requestId"] != "toolu_1" {
		t.Fatalf("event = %v", event)
	}
	if event := nextEvent(t, events); event["type"] != "run_finished" {
		t.Fatalf("event = %v", event)
	}
}

func TestPermissionTheModGivesUpOnExpiresInThePanel(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
	events := h.subscribe("")
	ctx, abandon := context.WithCancel(context.Background())
	body, _ := json.Marshal(map[string]any{"requestId": "toolu_1", "tool": "Bash", "input": map[string]any{"command": "ls"}})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, h.server.URL+"/mod/permissions", bytes.NewReader(body))
	go func() {
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
		}
	}()
	if event := nextEvent(t, events); event["type"] != "permission_request" {
		t.Fatalf("event = %v", event)
	}

	abandon() // the turn was interrupted and the tool call dropped

	if event := nextEvent(t, events); event["type"] != "permission_timeout" || event["requestId"] != "toolu_1" {
		t.Fatalf("event = %v", event)
	}
	if pending := h.snapshot().PendingPermissions; len(pending) != 0 {
		t.Fatalf("pending = %+v", pending)
	}
}

func TestEditPermissionShowsTheChangeBeingApproved(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
	events := h.subscribe("")

	h.askPermission("toolu_1", "Edit", map[string]any{"file_path": "/work/repo/a.go", "old_string": "x := 1", "new_string": "x := 2"})

	command, _ := nextEvent(t, events)["command"].(string)
	for _, want := range []string{"/work/repo/a.go", "- x := 1", "+ x := 2"} {
		if !strings.Contains(command, want) {
			t.Fatalf("command %q does not mention %q", command, want)
		}
	}
}

func TestResumedSessionHydratesFromItsTranscript(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(map[string]any{
		"busy": true,
		"messages": []map[string]any{
			{"role": "user", "text": "<system-reminder>context</system-reminder>", "toolUses": []any{}},
			{"role": "user", "text": "add a health check", "toolUses": []any{}},
			{"role": "assistant", "text": "On it.", "toolUses": []map[string]any{
				{"tool_use_id": "toolu_1", "tool": "Bash", "input": map[string]any{"command": "ls"}, "text": "main.go"},
				{"tool_use_id": "toolu_2", "tool": "Read", "input": map[string]any{"file_path": "main.go"}},
			}},
			{"role": "user", "text": "", "toolUses": []any{}, "toolResults": []map[string]any{{"tool_use_id": "toolu_1", "text": "main.go"}}},
			{"role": "assistant", "text": "Added.", "toolUses": []any{}},
		},
	}))

	snap := h.snapshot()
	if snap.Status != "running" {
		t.Fatalf("status = %q, want the turn already running", snap.Status)
	}
	var transcript []string
	for _, message := range snap.Messages {
		if text := messageText(message.Content); text != "" {
			transcript = append(transcript, message.Role+": "+text)
		}
	}
	want := []string{"user: add a health check", "assistant: On it.", "assistant: Added."}
	if strings.Join(transcript, "|") != strings.Join(want, "|") {
		t.Fatalf("transcript = %v, want %v", transcript, want)
	}
	if len(snap.ToolCalls) != 2 || snap.ToolCalls[0].Result["stdout"] != "main.go" || snap.ToolCalls[1].Result != nil {
		t.Fatalf("tool calls = %+v", snap.ToolCalls)
	}
}

func TestContextUsageFollowsTheSession(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(map[string]any{"context": map[string]any{"tokens": 1200, "window": 200000}}))

	var usage struct {
		ContextWindow    int  `json:"contextWindow"`
		LastPromptTokens *int `json:"lastPromptTokens"`
	}
	h.getJSON("/v1/sessions/task-1/context", &usage)
	if usage.ContextWindow != 200000 || usage.LastPromptTokens == nil || *usage.LastPromptTokens != 1200 {
		t.Fatalf("usage = %+v", usage)
	}

	events := h.subscribe("")
	h.mod(map[string]any{"type": "turn_complete", "turnId": "turn-1", "reason": "answer", "context": map[string]any{"tokens": 5000, "window": 200000}})

	updated := nextEvent(t, events)
	if updated["type"] != "usage_updated" || updated["usedContextTokens"] != float64(5000) || updated["contextWindow"] != float64(200000) {
		t.Fatalf("event = %v", updated)
	}
}

func TestClearingTheConversationEmptiesThePanel(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(map[string]any{"context": map[string]any{"tokens": 40000, "window": 200000}}), userRow("row-1", "hello"), assistantRow("row-2", textBlock("hi")))
	events := h.subscribe("")

	h.mod(map[string]any{"type": "reset", "sessionId": "claude-session-2"})

	// The panel re-reads the snapshot when a run finishes; that is how it
	// learns the conversation is gone.
	if event := nextEvent(t, events); event["type"] != "run_finished" {
		t.Fatalf("event = %v", event)
	}
	for _, message := range h.snapshot().Messages {
		if messageText(message.Content) != "" {
			t.Fatalf("conversation not cleared: %+v", message)
		}
	}
	var usage struct {
		LastPromptTokens *int `json:"lastPromptTokens"`
	}
	h.getJSON("/v1/sessions/task-1/context", &usage)
	if usage.LastPromptTokens != nil {
		t.Fatalf("context fill = %d, want it unknown until the new conversation's first reply", *usage.LastPromptTokens)
	}
	// A row repeated under the old id must not be dropped as a duplicate of
	// the cleared conversation's.
	h.mod(userRow("row-1", "fresh start"))
	if event := nextEvent(t, events); event["content"] != "fresh start" {
		t.Fatalf("event = %v", event)
	}
}

func TestSubagentActivityIsGroupedUnderTheCallThatSpawnedIt(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil), assistantRow("row-a", toolUse("toolu_agent", "Agent", map[string]any{
		"description": "Find the config loader", "prompt": "Locate where config is parsed.", "subagent_type": "Explore",
	})))
	events := h.subscribe("")

	call := h.snapshot().ToolCalls[0]
	if call.Name != "spawn_agent" || call.Args["purpose"] != "Find the config loader" || call.Args["agent"] != "Explore" || call.Args["prompt"] != "Locate where config is parsed." {
		t.Fatalf("call = %+v", call)
	}

	h.mod(map[string]any{"type": "agent_spawn", "toolUseId": "toolu_agent", "agentId": "agent-7", "description": "Find the config loader"})
	spawned := nextEvent(t, events)
	if spawned["type"] != "subagent_spawned" || spawned["parentCallId"] != "toolu_agent" || spawned["subagentId"] != "agent-7" || spawned["purpose"] != "Find the config loader" {
		t.Fatalf("spawned = %v", spawned)
	}

	child := assistantRow("row-c", textBlock("Looking in config/."), toolUse("toolu_c1", "Grep", map[string]any{"pattern": "yaml.Unmarshal"}))
	child["agentId"] = "agent-7"
	h.mod(child)
	said, started := nextEvent(t, events), nextEvent(t, events)
	if said["type"] != "subagent_event" || said["subagentId"] != "agent-7" {
		t.Fatalf("said = %v", said)
	}
	if inner, _ := said["event"].(map[string]any); inner["type"] != "assistant_text_done" || inner["text"] != "Looking in config/." {
		t.Fatalf("said inner = %v", said["event"])
	}
	if inner, _ := started["event"].(map[string]any); inner["type"] != "tool_call_start" || inner["name"] != "Grep" || inner["callId"] != "toolu_c1" {
		t.Fatalf("started inner = %v", started["event"])
	}

	h.mod(map[string]any{"type": "turn_complete", "turnId": "turn-9", "reason": "answer", "agentId": "agent-7"})
	finished := nextEvent(t, events)
	if finished["type"] != "subagent_finished" || finished["subagentId"] != "agent-7" || finished["reason"] != "stop" {
		t.Fatalf("finished = %v", finished)
	}

	// The subagent's own rows never join the main conversation.
	if calls := h.snapshot().ToolCalls; len(calls) != 1 {
		t.Fatalf("tool calls = %+v", calls)
	}
}

func TestModelChangeIsAnnounced(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
	events := h.subscribe("")

	h.mod(map[string]any{"type": "model", "model": "claude-sonnet-5-5"})

	if event := nextEvent(t, events); event["type"] != "model_changed" || event["to"] != "anthropic/claude-sonnet-5-5" {
		t.Fatalf("event = %v", event)
	}
	if model := h.snapshot().Model.ModelID; model != "claude-sonnet-5-5" {
		t.Fatalf("snapshot model = %q", model)
	}
}

func TestProtocolRoutesTheNativePanelReads(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(map[string]any{"commands": []map[string]any{
		{"name": "compact", "description": "Compact the conversation", "source": "builtin"},
	}}))

	var instance struct {
		CWD     string `json:"cwd"`
		PID     int    `json:"pid"`
		Version string `json:"version"`
	}
	if code := h.getJSON("/v1/instance", &instance); code != http.StatusOK || instance.CWD != "/work/repo" || instance.PID == 0 {
		t.Fatalf("instance = %d %+v", code, instance)
	}

	var models []struct {
		Ref       string `json:"ref"`
		IsDefault bool   `json:"isDefault"`
	}
	h.getJSON("/v1/sessions/task-1/models", &models)
	if len(models) != 1 || models[0].Ref != "anthropic/claude-opus-5-5" || !models[0].IsDefault {
		t.Fatalf("models = %+v", models)
	}

	var skills []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	h.getJSON("/v1/sessions/task-1/skills", &skills)
	if len(skills) != 1 || skills[0].Name != "compact" || skills[0].Description != "Compact the conversation" {
		t.Fatalf("skills = %+v", skills)
	}

	for _, path := range []string{"/v1/sessions/task-1/modes", "/v1/sessions/task-1/commands", "/v1/sessions/task-1/subagents"} {
		var list []any
		if code := h.getJSON(path, &list); code != http.StatusOK || list == nil || len(list) != 0 {
			t.Fatalf("GET %s = %d %v, want an empty list", path, code, list)
		}
	}

	if code, _ := h.post("/v1/sessions/task-1/paths", map[string]any{"kind": "read", "path": "/work/repo/a.go"}); code != http.StatusNoContent {
		t.Fatalf("paths = %d", code)
	}
}

func TestBridgeAnnouncesItsEndpointToTheMod(t *testing.T) {
	h := newHarness(t)

	if err := h.bridge.Ready("http://127.0.0.1:38123/tok"); err != nil {
		t.Fatal(err)
	}

	if command := h.nextCommand(); command["type"] != "ready" || command["url"] != "http://127.0.0.1:38123/tok" {
		t.Fatalf("command = %v", command)
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestBridgeReportsWhenTheModStopsReading(t *testing.T) {
	disconnected := make(chan struct{})
	bridge := New(Options{TaskID: "task-1", Commands: brokenWriter{}, OnDisconnect: func() { close(disconnected) }})
	defer bridge.Close()

	if err := bridge.Ready("http://127.0.0.1:1/x"); err == nil {
		t.Fatal("expected the write failure")
	}
	_ = bridge.Ready("http://127.0.0.1:1/x") // reported once, however often it fails

	select {
	case <-disconnected:
	case <-time.After(time.Second):
		t.Fatal("OnDisconnect was not called")
	}
}

func TestMessageFoldedIntoARunningTurnShowsAsThePersonsMessage(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil), map[string]any{"type": "turn_start", "turnId": "turn-1"})
	events := h.subscribe("")

	// The mod appends a queued message to the running turn in a framed row;
	// it reports the words as sent so the panel shows those.
	h.mod(map[string]any{"type": "user_note", "text": "also update the docs"})

	if event := nextEvent(t, events); event["type"] != "user_message" || event["content"] != "also update the docs" {
		t.Fatalf("event = %v", event)
	}
	last := h.snapshot().Messages
	if tail := last[len(last)-1]; tail.Role != "user" || messageText(tail.Content) != "also update the docs" {
		t.Fatalf("last message = %+v", tail)
	}
}

func promptRow(uuid, originKind, text string, extra map[string]any) map[string]any {
	message := map[string]any{"type": "user", "role": "user", "content": []map[string]any{{"type": "text", "text": text}}}
	for key, value := range extra {
		message[key] = value
	}
	return map[string]any{"type": "row", "uuid": uuid, "door": "prompt", "origin": map[string]any{"kind": originKind}, "message": message}
}

func TestPromptsAreShownWhateverChannelTheyArrivedBy(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))

	// A headless host's prompt carries no provenance the engine can name; a
	// message Legato submits is stamped with the mod's name.
	h.mod(
		promptRow("row-1", "unclassified", "from a headless host", nil),
		promptRow("row-2", "plugin", "from Legato", nil),
		promptRow("row-3", "peer", "from another session", nil),
	)

	var shown []string
	for _, message := range h.snapshot().Messages {
		if message.Role == "user" {
			shown = append(shown, messageText(message.Content))
		}
	}
	if strings.Join(shown, "|") != "from a headless host|from Legato|from another session" {
		t.Fatalf("shown = %v", shown)
	}
}

func TestRowsThePersonNeverSeesAsTypedStayOut(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))

	h.mod(
		promptRow("row-1", "unclassified", "an idle notice the engine queued", map[string]any{"isMeta": true}),
		promptRow("row-2", "scheduled-trigger", "a routine firing its stored prompt", nil),
		promptRow("row-3", "engine", "<system-reminder>context</system-reminder>", map[string]any{"type": "attachment", "name": "environment"}),
	)

	for _, message := range h.snapshot().Messages {
		if message.Role == "user" {
			t.Fatalf("unexpected user message %q", messageText(message.Content))
		}
	}
}

func TestPermissionAnsweredAtTheTerminalLeavesThePanel(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil), map[string]any{"type": "turn_start", "turnId": "turn-1"})
	h.mod(assistantRow("row-a",
		toolUse("toolu_other", "Bash", map[string]any{"command": "ls"}),
		toolUse("toolu_1", "Bash", map[string]any{"command": "git push"}),
	))
	events := h.subscribe("")
	answer := h.askPermission("permission-1", "Bash", map[string]any{"command": "git push"})
	nextEvent(t, events) // permission_request

	// Another call finishing says nothing about the one being decided.
	h.mod(toolResultRow("row-r0", "toolu_other", "a.go", false))
	if event := nextEvent(t, events); event["type"] != "tool_call_result" {
		t.Fatalf("event = %v", event)
	}
	if pending := h.snapshot().PendingPermissions; len(pending) != 1 {
		t.Fatalf("pending = %+v, want the request still open", pending)
	}

	// Claude Code shows its own dialog while Legato is asked. The call's
	// result arriving means the person answered there.
	h.mod(toolResultRow("row-r1", "toolu_1", "Everything up-to-date", false))

	if event := nextEvent(t, events); event["type"] != "permission_timeout" || event["requestId"] != "permission-1" {
		t.Fatalf("event = %v", event)
	}
	if decided := nextAnswer(t, answer); decided["decision"] != "ask" {
		t.Fatalf("answer = %v", decided)
	}
	snap := h.snapshot()
	if len(snap.PendingPermissions) != 0 || snap.Status != "running" {
		t.Fatalf("status %q pending %+v", snap.Status, snap.PendingPermissions)
	}
}

func TestSwitchingSessionsReplacesTheConversationAndReloadsThePanel(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil), userRow("row-1", "work in the first session"))
	events := h.subscribe("")

	// The person resumed another session inside the same Claude Code process:
	// the mod describes the session again, with that session's transcript.
	h.mod(hello(map[string]any{
		"sessionId": "claude-session-2",
		"context":   map[string]any{"tokens": 88000, "window": 200000},
		"messages": []map[string]any{
			{"role": "user", "text": "earlier question", "toolUses": []any{}},
			{"role": "assistant", "text": "earlier answer", "toolUses": []any{}},
		},
	}))

	// A finished run is what makes the panel read the snapshot again.
	if event := nextEvent(t, events); event["type"] != "run_finished" {
		t.Fatalf("event = %v", event)
	}
	var transcript []string
	for _, message := range h.snapshot().Messages {
		if text := messageText(message.Content); text != "" {
			transcript = append(transcript, message.Role+": "+text)
		}
	}
	if strings.Join(transcript, "|") != "user: earlier question|assistant: earlier answer" {
		t.Fatalf("transcript = %v, want only the resumed session's", transcript)
	}
	var usage struct {
		LastPromptTokens *int `json:"lastPromptTokens"`
	}
	h.getJSON("/v1/sessions/task-1/context", &usage)
	if usage.LastPromptTokens == nil || *usage.LastPromptTokens != 88000 {
		t.Fatalf("context = %+v, want the resumed session's", usage)
	}
}

func TestTheFirstDescriptionOfASessionReloadsNothing(t *testing.T) {
	h := newHarness(t)
	events := h.subscribe("")

	h.mod(hello(nil), userRow("row-1", "hello"))

	if event := nextEvent(t, events); event["type"] != "user_message" {
		t.Fatalf("first event = %v, want no reload before the conversation", event)
	}
}

func TestContextUsageIsReportedWhileATurnRuns(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil), map[string]any{"type": "turn_start", "turnId": "turn-1"})
	events := h.subscribe("")

	// Each model request of a long turn moves the figure; waiting for the
	// turn to end would leave the bar stale for its whole length.
	h.mod(map[string]any{"type": "usage", "context": map[string]any{"tokens": 61000, "window": 200000}})

	event := nextEvent(t, events)
	if event["type"] != "usage_updated" || event["usedContextTokens"] != float64(61000) || event["contextWindow"] != float64(200000) {
		t.Fatalf("event = %v", event)
	}
	if status := h.snapshot().Status; status != "running" {
		t.Fatalf("status = %q, want the turn still running", status)
	}
}
