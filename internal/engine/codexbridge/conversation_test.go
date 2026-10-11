package codexbridge

import (
	"net/http"
	"reflect"
	"testing"
	"time"
)

func commandItem(id, command, status string, output any, exitCode any) map[string]any {
	return map[string]any{
		"type": "commandExecution", "id": id, "command": command, "cwd": "/work/repo", "status": status,
		"aggregatedOutput": output, "exitCode": exitCode, "durationMs": 1200,
	}
}

func TestShellCommandIsShownAsANativeBashCallWithItsOutput(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")
	events := h.subscribe("")

	// Codex runs every command in a login shell; the panel shows what was asked.
	h.item("started", commandItem("exec-1", "/usr/bin/zsh -lc 'go test ./... && echo '\\''ok'\\'''", "inProgress", nil, nil))
	started := nextEvent(t, events)
	if started["type"] != "tool_call_start" || started["callId"] != "exec-1" || started["name"] != "bash" {
		t.Fatalf("event = %v", started)
	}
	if args, _ := started["args"].(map[string]any); args["command"] != "go test ./... && echo 'ok'" {
		t.Fatalf("args = %v", started["args"])
	}

	h.item("completed", commandItem("exec-1", "/usr/bin/zsh -lc 'go test ./... && echo '\\''ok'\\'''", "completed", "ok\n", 0))
	finished := nextEvent(t, events)
	result, _ := finished["result"].(map[string]any)
	if finished["type"] != "tool_call_result" || finished["callId"] != "exec-1" || result["stdout"] != "ok\n" || result["exit_code"] != float64(0) {
		t.Fatalf("event = %v", finished)
	}

	h.item("started", commandItem("exec-2", "false", "inProgress", nil, nil))
	h.item("completed", commandItem("exec-2", "false", "failed", "boom\n", 2))
	failed := eventOf(t, events, "tool_call_result")
	if result, _ := failed["result"].(map[string]any); result["stderr"] != "boom\n" || result["exit_code"] != float64(2) {
		t.Fatalf("failed result = %v", failed["result"])
	}

	calls := h.snapshot().ToolCalls
	if len(calls) != 2 || calls[0].Name != "bash" || calls[0].Display.Summary != "go test ./... && echo 'ok'" || calls[0].EndedAt-calls[0].StartedAt != 1200 {
		t.Fatalf("tool calls = %+v", calls)
	}
}

func TestFileChangeIsShownAsAPatchOfEachFile(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")
	events := h.subscribe("")
	change := func(status string) map[string]any {
		return map[string]any{
			"type": "fileChange", "id": "patch-1", "status": status,
			"changes": []any{
				map[string]any{"path": "/work/repo/a.go", "kind": map[string]any{"type": "update", "move_path": nil}, "diff": "@@ -1 +1 @@\n-x := 1\n+x := 2\n"},
				map[string]any{"path": "/work/repo/new.txt", "kind": map[string]any{"type": "add"}, "diff": "hi\n"},
			},
		}
	}

	h.item("started", change("inProgress"))
	started := nextEvent(t, events)
	if started["type"] != "tool_call_start" || started["name"] != "patch" {
		t.Fatalf("event = %v", started)
	}
	args, _ := started["args"].(map[string]any)
	changes, _ := args["changes"].([]any)
	if len(changes) != 2 {
		t.Fatalf("args = %v", started["args"])
	}
	first, _ := changes[0].(map[string]any)
	if first["path"] != "/work/repo/a.go" || first["kind"] != "update" || first["diff"] != "@@ -1 +1 @@\n-x := 1\n+x := 2\n" {
		t.Fatalf("first change = %v", first)
	}
	if display, _ := started["display"].(map[string]any); display["summary"] != "a.go, new.txt" {
		t.Fatalf("display = %v", started["display"])
	}

	h.item("completed", change("completed"))
	if result, _ := nextEvent(t, events)["result"].(map[string]any); result["ok"] != true {
		t.Fatalf("result = %v", result)
	}
}

func TestOtherToolsKeepTheirNameAndArguments(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")
	events := h.subscribe("")

	h.item("completed", map[string]any{
		"type": "mcpToolCall", "id": "mcp-1", "server": "github", "tool": "search_issues", "status": "completed",
		"arguments": map[string]any{"query": "flaky test"},
		"result":    map[string]any{"content": []any{map[string]any{"type": "text", "text": "3 issues"}}},
	})

	started := nextEvent(t, events)
	if started["name"] != "github.search_issues" {
		t.Fatalf("event = %v", started)
	}
	if display, _ := started["display"].(map[string]any); display["summary"] != "flaky test" {
		t.Fatalf("display = %v", started["display"])
	}
	if result, _ := nextEvent(t, events)["result"].(map[string]any); result["output"] != "3 issues" {
		t.Fatalf("result = %v", result)
	}

	// Bookkeeping items, such as a compaction, are not part of what the panel shows.
	h.item("completed", map[string]any{"type": "contextCompaction", "id": "c-1"})
	h.item("completed", agentMessage("item-a1", "done"))
	if next := nextEvent(t, events); next["type"] != "assistant_text_done" {
		t.Fatalf("event = %v", next)
	}
}

func TestMessageToAnIdleSessionStartsATurnAndJoinsIt(t *testing.T) {
	h := newHarness(t)
	h.attach()

	// Launch: the terminal has opened its thread but nothing has run yet, so
	// the bridge is not receiving the thread's events.
	code, body := h.post("/v1/sessions/task-1/messages", map[string]any{"content": "read your brief and begin"})
	if code != http.StatusAccepted {
		t.Fatalf("status = %d %s", code, body)
	}
	started := h.codex.called("turn/start")
	if started["threadId"] != threadID {
		t.Fatalf("turn/start = %v", started)
	}
	want := []any{map[string]any{"type": "text", "text": "read your brief and begin", "text_elements": []any{}}}
	if !reflect.DeepEqual(started["input"], want) {
		t.Fatalf("input = %v", started["input"])
	}

	// The turn it started gives the thread a history to join.
	h.codex.on("thread/resume", func(map[string]any) (any, *rpcError) {
		return map[string]any{"thread": terminalThread(map[string]any{
			"status": map[string]any{"type": "active"},
			"turns":  []any{map[string]any{"id": "turn-1", "status": "inProgress", "items": []any{userMessage("item-u1", "read your brief and begin")}}},
		})}, nil
	})
	deadline := time.After(2 * time.Second)
	for !h.bridge.joined() {
		select {
		case <-deadline:
			t.Fatal("the bridge never joined the thread it started a turn on")
		case <-time.After(2 * time.Millisecond):
		}
	}
	snap := h.snapshot()
	if snap.Status != "running" || len(snap.Messages) != 2 || snap.Messages[1].Content != "read your brief and begin" {
		t.Fatalf("snapshot = %+v", snap)
	}
}

func TestMessageDuringATurnJoinsThatTurn(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-7")

	if code, body := h.post("/v1/sessions/task-1/messages", map[string]any{"content": "also update the docs"}); code != http.StatusAccepted {
		t.Fatalf("status = %d %s", code, body)
	}

	steered := h.codex.called("turn/steer")
	if steered["threadId"] != threadID || steered["expectedTurnId"] != "turn-7" {
		t.Fatalf("turn/steer = %v", steered)
	}
	if h.codex.count("turn/start") != 0 {
		t.Fatal("a message during a turn started a second turn")
	}
}

func TestMessageSentAsATurnEndsStartsTheNextOne(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-7")
	h.codex.on("turn/steer", func(map[string]any) (any, *rpcError) {
		return nil, &rpcError{Code: -32600, Message: "no active turn to steer"}
	})

	if code, body := h.post("/v1/sessions/task-1/messages", map[string]any{"content": "one more thing"}); code != http.StatusAccepted {
		t.Fatalf("status = %d %s", code, body)
	}

	if started := h.codex.called("turn/start"); started["threadId"] != threadID {
		t.Fatalf("turn/start = %v", started)
	}
}

func TestAppendedContextRidesWithTheNextMessage(t *testing.T) {
	h := newHarness(t)
	h.attach()

	if code, _ := h.post("/v1/sessions/task-1/messages?append=true", map[string]any{"content": "<file path=\"main.go\">package main</file>"}); code != http.StatusAccepted {
		t.Fatalf("append status = %d", code)
	}
	if h.codex.count("turn/start") != 0 {
		t.Fatal("appended context started a turn")
	}
	h.post("/v1/sessions/task-1/messages", map[string]any{"content": "explain @main.go"})

	input, _ := h.codex.called("turn/start")["input"].([]any)
	if len(input) != 2 {
		t.Fatalf("input = %v", input)
	}
	attached, _ := input[0].(map[string]any)
	asked, _ := input[1].(map[string]any)
	if attached["text"] != "<file path=\"main.go\">package main</file>" || asked["text"] != "explain @main.go" {
		t.Fatalf("input = %v", input)
	}
}

func TestMessagesSentBeforeTheTerminalHasAThreadWaitForIt(t *testing.T) {
	h := newHarness(t)

	// Launch: the bridge is up, but the terminal is still starting (or is
	// showing a prompt of its own) and has no thread to put a message on.
	if code, body := h.post("/v1/sessions/task-1/messages", map[string]any{"content": "read your brief and begin"}); code != http.StatusAccepted {
		t.Fatalf("status = %d %s, want the message held", code, body)
	}
	if code, _ := h.post("/v1/sessions/task-1/messages", map[string]any{"content": "and tell me when you are done"}); code != http.StatusAccepted {
		t.Fatalf("second message: status = %d", code)
	}
	if h.codex.count("turn/start") != 0 {
		t.Fatal("a turn was started with no thread to start it on")
	}
	if code, _ := h.post("/v1/sessions/task-1/messages", map[string]any{"content": ""}); code != http.StatusBadRequest {
		t.Fatalf("empty message: status = %d, want 400", code)
	}

	h.attach()

	// Both go to the thread as one turn, in the order they were sent.
	started := h.codex.called("turn/start")
	input, _ := started["input"].([]any)
	if started["threadId"] != threadID || len(input) != 2 {
		t.Fatalf("turn/start = %v", started)
	}
	first, _ := input[0].(map[string]any)
	second, _ := input[1].(map[string]any)
	if first["text"] != "read your brief and begin" || second["text"] != "and tell me when you are done" {
		t.Fatalf("input = %v", input)
	}
	time.Sleep(20 * time.Millisecond)
	if h.codex.count("turn/start") != 1 {
		t.Fatalf("held messages started %d turns, want one", h.codex.count("turn/start"))
	}
}

func TestInterruptStopsTheTurnInProgress(t *testing.T) {
	h := newHarness(t)
	h.attach()

	// Nothing is running: there is nothing to stop.
	if code, _ := h.post("/v1/sessions/task-1/interrupt", nil); code != http.StatusNoContent {
		t.Fatalf("idle interrupt status = %d", code)
	}
	if h.codex.count("turn/interrupt") != 0 {
		t.Fatal("an idle session was interrupted")
	}

	h.begin("turn-3")
	events := h.subscribe("")
	if code, body := h.post("/v1/sessions/task-1/interrupt", nil); code != http.StatusNoContent {
		t.Fatalf("status = %d %s", code, body)
	}
	if stopped := h.codex.called("turn/interrupt"); stopped["threadId"] != threadID || stopped["turnId"] != "turn-3" {
		t.Fatalf("turn/interrupt = %v", stopped)
	}

	h.finish("turn-3", "interrupted")
	if finished := nextEvent(t, events); finished["type"] != "run_finished" || finished["reason"] != "interrupted" {
		t.Fatalf("event = %v", finished)
	}
}

func TestHelperAndSubagentThreadsAreNotTheConversation(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")
	events := h.subscribe("")
	parent := threadID

	// Codex names a session on a short-lived thread of its own, and runs
	// subagents on threads under the session's.
	h.codex.notify("thread/started", map[string]any{"thread": map[string]any{"id": "thr-title", "ephemeral": true, "threadSource": "thread_title", "cwd": "/elsewhere"}})
	h.codex.notify("thread/started", map[string]any{"thread": map[string]any{"id": "thr-sub", "ephemeral": false, "parentThreadId": parent, "cwd": "/elsewhere"}})
	h.codex.notify("item/completed", map[string]any{"threadId": "thr-title", "item": agentMessage("item-x", "A title")})
	h.codex.notify("turn/completed", map[string]any{"threadId": "thr-title", "turn": map[string]any{"id": "turn-x", "status": "completed"}})
	h.item("completed", agentMessage("item-a1", "still here"))

	if next := nextEvent(t, events); next["type"] != "assistant_text_done" || next["text"] != "still here" {
		t.Fatalf("event = %v", next)
	}
	if snap := h.snapshot(); snap.CWD != "/work/repo" || snap.Status != "running" {
		t.Fatalf("snapshot = %+v", snap)
	}
}

func TestStartingOverInTheTerminalReplacesTheConversation(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")
	h.item("completed", userMessage("item-u1", "first conversation"))
	h.finish("turn-1", "completed")
	events := h.subscribe("")

	// /new in the terminal: the same process goes on under another thread.
	h.codex.on("thread/resume", func(map[string]any) (any, *rpcError) {
		return nil, &rpcError{Code: -32600, Message: "no rollout found"}
	})
	h.codex.notify("thread/started", map[string]any{"thread": terminalThread(map[string]any{"id": "thr-2", "cwd": "/work/other"})})

	// The panel re-reads the snapshot on run_finished.
	if reset := nextEvent(t, events); reset["type"] != "run_finished" || reset["reason"] != "reset" {
		t.Fatalf("event = %v", reset)
	}
	if snap := h.snapshot(); len(snap.Messages) != 1 || snap.CWD != "/work/other" || snap.Status != "idle" {
		t.Fatalf("snapshot = %+v", snap)
	}

	// Messages now go to the new thread.
	h.post("/v1/sessions/task-1/messages", map[string]any{"content": "second conversation"})
	if started := h.codex.called("turn/start"); started["threadId"] != "thr-2" {
		t.Fatalf("turn/start = %v", started)
	}
}

func TestAThreadResumedFromHistoryShowsItsEarlierTurns(t *testing.T) {
	h := newHarness(t)
	events := h.subscribe("")
	h.codex.on("thread/resume", func(map[string]any) (any, *rpcError) {
		return map[string]any{"thread": terminalThread(map[string]any{"turns": []any{
			map[string]any{"id": "turn-0", "status": "completed", "items": []any{
				userMessage("item-u0", "what is in this repo?"),
				commandItem("exec-0", "ls", "completed", "main.go\n", 0),
				agentMessage("item-a0", "One Go file."),
			}},
		}})}, nil
	})

	// `codex resume`: the terminal opens a thread that already has turns.
	h.codex.notify("thread/started", map[string]any{"thread": terminalThread(nil)})

	kinds := []string{}
	for len(kinds) < 4 {
		kinds = append(kinds, nextEvent(t, events)["type"].(string))
	}
	if want := []string{"user_message", "tool_call_start", "tool_call_result", "assistant_text_done"}; !reflect.DeepEqual(kinds, want) {
		t.Fatalf("events = %v, want %v", kinds, want)
	}
	snap := h.snapshot()
	// The prompt, the call the reply opened with, and the reply's text.
	if snap.Status != "idle" || len(snap.Messages) != 4 || len(snap.ToolCalls) != 1 || snap.ToolCalls[0].Result["stdout"] != "main.go\n" {
		t.Fatalf("snapshot = %+v", snap)
	}
}

func TestATurnThatFailsReportsWhy(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")
	events := h.subscribe("")

	h.codex.notify("turn/completed", map[string]any{"threadId": threadID, "turn": map[string]any{
		"id": "turn-1", "status": "failed", "error": map[string]any{"message": "usage limit reached"},
	}})

	if failure := nextEvent(t, events); failure["type"] != "error" || failure["error"] != "usage limit reached" {
		t.Fatalf("event = %v", failure)
	}
	if finished := nextEvent(t, events); finished["type"] != "run_finished" || finished["reason"] != "error" {
		t.Fatalf("event = %v", finished)
	}
}

func TestContextUsageFollowsTheThread(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")
	events := h.subscribe("")

	h.codex.notify("thread/tokenUsage/updated", map[string]any{"threadId": threadID, "turnId": "turn-1", "tokenUsage": map[string]any{
		"total": map[string]any{"totalTokens": 59937}, "last": map[string]any{"totalTokens": 20040}, "modelContextWindow": 258400,
	}})

	// The last request's size is how full the context is; the total is spend.
	if usage := nextEvent(t, events); usage["type"] != "usage_updated" || usage["usedContextTokens"] != float64(20040) || usage["contextWindow"] != float64(258400) {
		t.Fatalf("event = %v", usage)
	}
	if snap := h.snapshot(); snap.UsedContextTokens != 20040 || snap.ContextWindow != 258400 {
		t.Fatalf("snapshot usage = %d / %d", snap.UsedContextTokens, snap.ContextWindow)
	}
}

func TestAMessageHeldForTheTerminalIsShownAtOnceAndOnlyOnce(t *testing.T) {
	h := newHarness(t)
	events := h.subscribe("")

	// The terminal is not ready, but the person should see their message land.
	h.post("/v1/sessions/task-1/messages", map[string]any{"content": "read your brief and begin"})

	if shown := nextEvent(t, events); shown["type"] != "user_message" || shown["content"] != "read your brief and begin" {
		t.Fatalf("event = %v", shown)
	}
	if snap := h.snapshot(); len(snap.Messages) != 2 || snap.Messages[1].Content != "read your brief and begin" || snap.Status != "idle" {
		t.Fatalf("snapshot while held = %+v", snap)
	}

	// The terminal opens its thread; the message goes to it, and Codex
	// reports it back as part of the turn it started.
	h.attach()
	h.codex.called("turn/start")
	h.begin("turn-1")
	h.item("completed", userMessage("item-u1", "read your brief and begin"))
	h.item("completed", agentMessage("item-a1", "On it."))

	for {
		event := nextEvent(t, events)
		if event["type"] == "user_message" {
			t.Fatalf("the held message was shown a second time: %v", event)
		}
		if event["type"] == "assistant_text_done" {
			break
		}
	}
	if snap := h.snapshot(); len(snap.Messages) != 3 || snap.Messages[1].Content != "read your brief and begin" {
		t.Fatalf("snapshot = %+v", snap.Messages)
	}
}

func TestAttachedContextIsNotShownAsWhatThePersonTyped(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-0")
	h.finish("turn-0", "completed")
	events := h.subscribe("")
	h.post("/v1/sessions/task-1/messages?append=true", map[string]any{"content": "<file path=\"main.go\">package main</file>"})
	h.post("/v1/sessions/task-1/messages", map[string]any{"content": "explain @main.go"})
	h.codex.called("turn/start")

	// Codex reports the turn's input as one message: the file and the words.
	h.item("completed", map[string]any{"type": "userMessage", "id": "item-u1", "content": []any{
		map[string]any{"type": "text", "text": "<file path=\"main.go\">package main</file>"},
		map[string]any{"type": "text", "text": "explain @main.go"},
	}})

	if shown := eventOf(t, events, "user_message"); shown["content"] != "explain @main.go" {
		t.Fatalf("shown = %q, want what the person typed", shown["content"])
	}
}
