package codexbridge

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func commandApproval(decisions ...any) map[string]any {
	params := map[string]any{
		"kind": "command", "threadId": threadID, "turnId": "turn-1", "itemId": "exec-1",
		"command": "/usr/bin/zsh -lc 'npm run build'", "cwd": "/work/repo",
		"reason": "The sandbox blocked writing to dist.",
	}
	if decisions != nil {
		params["availableDecisions"] = decisions
	}
	return params
}

var alwaysNpmRun = map[string]any{"acceptWithExecpolicyAmendment": map[string]any{"execpolicy_amendment": []any{"npm", "run"}}}

// decision reads what the bridge answered the app server with.
func decision(t *testing.T, answered frame) any {
	t.Helper()
	var result struct {
		Decision any `json:"decision"`
	}
	if err := json.Unmarshal(answered.Result, &result); err != nil {
		t.Fatalf("answer %s: %v", answered.Result, err)
	}
	return result.Decision
}

func TestCommandApprovalWaitsForLegatosDecision(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")
	events := h.subscribe("")

	h.codex.ask(7, "item/commandExecution/requestApproval", commandApproval("accept", alwaysNpmRun, "cancel"))

	request := nextEvent(t, events)
	if request["type"] != "permission_request" || request["tool"] != "bash" || request["target"] != "host" {
		t.Fatalf("request = %v", request)
	}
	if request["command"] != "npm run build" || request["reason"] != "The sandbox blocked writing to dist." {
		t.Fatalf("request = %v", request)
	}
	requestID, _ := request["requestId"].(string)
	want := []any{
		map[string]any{"id": "accept", "label": "Allow once"},
		map[string]any{"id": "acceptWithExecpolicyAmendment", "label": "Allow, and don't ask again for commands that start with `npm run`"},
	}
	if !reflect.DeepEqual(request["options"], want) {
		t.Fatalf("options = %v", request["options"])
	}
	snap := h.snapshot()
	if snap.Status != "waiting_for_permission" || len(snap.PendingPermissions) != 1 || snap.PendingPermissions[0].RequestID != requestID {
		t.Fatalf("status %q pending %+v", snap.Status, snap.PendingPermissions)
	}
	if pending := snap.PendingPermissions[0]; pending.Command != "npm run build" || pending.CWD != "/work/repo" || len(pending.Options) != 2 {
		t.Fatalf("pending = %+v", pending)
	}

	// An option the request never offered changes nothing.
	if code, _ := h.post("/v1/sessions/task-1/permissions/"+requestID, map[string]any{"decision": "allow", "option": "forever"}); code != http.StatusBadRequest {
		t.Fatalf("unknown option: status = %d, want 400", code)
	}
	if code, body := h.post("/v1/sessions/task-1/permissions/"+requestID, map[string]any{"decision": "allow", "option": "acceptWithExecpolicyAmendment"}); code != http.StatusNoContent {
		t.Fatalf("resolve status = %d %s", code, body)
	}

	answered := h.codex.answer()
	if string(answered.ID) != "7" || !reflect.DeepEqual(decision(t, answered), alwaysNpmRun) {
		t.Fatalf("answer to the app server = %s %s", answered.ID, answered.Result)
	}
	if resolved := nextEvent(t, events); resolved["type"] != "permission_resolved" || resolved["requestId"] != requestID || resolved["decision"] != "allow" || resolved["option"] != "acceptWithExecpolicyAmendment" {
		t.Fatalf("resolved = %v", resolved)
	}
	if snap := h.snapshot(); len(snap.PendingPermissions) != 0 || snap.Status != "running" {
		t.Fatalf("after resolve: status %q pending %+v", snap.Status, snap.PendingPermissions)
	}
}

func TestAnAllowThatNamesNoOptionAllowsOnce(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")
	events := h.subscribe("")
	h.codex.ask(7, "item/commandExecution/requestApproval", commandApproval("accept", alwaysNpmRun, "cancel"))
	requestID, _ := nextEvent(t, events)["requestId"].(string)

	h.post("/v1/sessions/task-1/permissions/"+requestID, map[string]any{"decision": "allow"})

	if got := decision(t, h.codex.answer()); got != "accept" {
		t.Fatalf("decision = %v, want accept", got)
	}
}

func TestARequestWithOneWayToAllowTakesAPlainAllow(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")
	events := h.subscribe("")

	// An older server names no decisions at all.
	h.codex.ask(7, "item/commandExecution/requestApproval", commandApproval())

	request := nextEvent(t, events)
	if _, listed := request["options"]; listed {
		t.Fatalf("request = %v, want no options", request)
	}
}

func TestDenyingDeclinesTheCommandWhereTheTurnCanGoOn(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")
	events := h.subscribe("")
	h.codex.ask(7, "item/commandExecution/requestApproval", commandApproval("accept", "decline", "cancel"))
	requestID, _ := nextEvent(t, events)["requestId"].(string)

	// What the person wants done instead reaches the turn that is still running.
	if code, body := h.post("/v1/sessions/task-1/permissions/"+requestID, map[string]any{"decision": "deny", "feedback": "use pnpm instead"}); code != http.StatusNoContent {
		t.Fatalf("status = %d %s", code, body)
	}

	if got := decision(t, h.codex.answer()); got != "decline" {
		t.Fatalf("decision = %v, want decline", got)
	}
	if resolved := nextEvent(t, events); resolved["type"] != "permission_resolved" || resolved["decision"] != "deny" {
		t.Fatalf("resolved = %v", resolved)
	}
	steered := h.codex.called("turn/steer")
	input, _ := steered["input"].([]any)
	if first, _ := input[0].(map[string]any); len(input) != 1 || first["text"] != "use pnpm instead" {
		t.Fatalf("turn/steer input = %v", steered["input"])
	}
}

func TestDenyingARequestThatCanOnlyBeCancelledSendsTheFeedbackAfterTheTurn(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")
	events := h.subscribe("")
	h.codex.ask(7, "item/commandExecution/requestApproval", commandApproval("accept", alwaysNpmRun, "cancel"))
	requestID, _ := nextEvent(t, events)["requestId"].(string)

	h.post("/v1/sessions/task-1/permissions/"+requestID, map[string]any{"decision": "deny", "feedback": "use pnpm instead"})
	if got := decision(t, h.codex.answer()); got != "cancel" {
		t.Fatalf("decision = %v, want cancel", got)
	}

	// Cancelling ends the turn; the feedback opens the next one.
	time.Sleep(20 * time.Millisecond)
	if h.codex.count("turn/steer")+h.codex.count("turn/start") != 0 {
		t.Fatal("the feedback was sent into the turn being cancelled")
	}
	h.finish("turn-1", "interrupted")
	started := h.codex.called("turn/start")
	input, _ := started["input"].([]any)
	if first, _ := input[0].(map[string]any); first["text"] != "use pnpm instead" {
		t.Fatalf("turn/start input = %v", started["input"])
	}
}

func TestFileChangeApprovalShowsTheChange(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")
	events := h.subscribe("")
	h.item("started", map[string]any{
		"type": "fileChange", "id": "patch-1", "status": "inProgress",
		"changes": []any{map[string]any{"path": "/work/repo/a.go", "kind": map[string]any{"type": "update"}, "diff": "@@ -1 +1 @@\n-x := 1\n+x := 2\n"}},
	})
	nextEvent(t, events) // tool_call_start

	h.codex.ask(3, "item/fileChange/requestApproval", map[string]any{"threadId": threadID, "turnId": "turn-1", "itemId": "patch-1", "reason": nil, "grantRoot": nil})

	request := nextEvent(t, events)
	command, _ := request["command"].(string)
	if request["type"] != "permission_request" || request["tool"] != "patch" {
		t.Fatalf("request = %v", request)
	}
	for _, want := range []string{"Update /work/repo/a.go", "-x := 1", "+x := 2"} {
		if !strings.Contains(command, want) {
			t.Fatalf("command %q does not show %q", command, want)
		}
	}
	want := []any{
		map[string]any{"id": "accept", "label": "Allow once"},
		map[string]any{"id": "acceptForSession", "label": "Allow for this session"},
	}
	if !reflect.DeepEqual(request["options"], want) {
		t.Fatalf("options = %v", request["options"])
	}

	requestID, _ := request["requestId"].(string)
	h.post("/v1/sessions/task-1/permissions/"+requestID, map[string]any{"decision": "allow", "option": "acceptForSession"})
	if got := decision(t, h.codex.answer()); got != "acceptForSession" {
		t.Fatalf("decision = %v", got)
	}
}

func TestApprovalAnsweredAtTheTerminalLeavesThePanel(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")
	events := h.subscribe("")
	h.codex.ask(7, "item/commandExecution/requestApproval", commandApproval("accept", "cancel"))
	requestID, _ := nextEvent(t, events)["requestId"].(string)

	// The terminal shows the same request; the server says once it is settled.
	h.codex.notify("serverRequest/resolved", map[string]any{"threadId": threadID, "requestId": 7})

	if expired := nextEvent(t, events); expired["type"] != "permission_timeout" || expired["requestId"] != requestID {
		t.Fatalf("event = %v", expired)
	}
	if code, _ := h.post("/v1/sessions/task-1/permissions/"+requestID, map[string]any{"decision": "allow"}); code != http.StatusNotFound {
		t.Fatalf("late decision: status = %d, want 404", code)
	}
	if snap := h.snapshot(); len(snap.PendingPermissions) != 0 || snap.Status != "running" {
		t.Fatalf("status %q pending %+v", snap.Status, snap.PendingPermissions)
	}
}

func TestRequestsLeftOpenWhenTheTurnEndsAreClosed(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")
	events := h.subscribe("")
	h.codex.ask(7, "item/commandExecution/requestApproval", commandApproval("accept", "cancel"))
	requestID, _ := nextEvent(t, events)["requestId"].(string)

	h.finish("turn-1", "interrupted")

	if expired := nextEvent(t, events); expired["type"] != "permission_timeout" || expired["requestId"] != requestID {
		t.Fatalf("event = %v", expired)
	}
	if finished := nextEvent(t, events); finished["type"] != "run_finished" {
		t.Fatalf("event = %v", finished)
	}
}

func colorQuestion() map[string]any {
	return map[string]any{
		"threadId": threadID, "turnId": "turn-1", "itemId": "tool-1", "isBlocking": true,
		"questions": []any{
			map[string]any{
				"id": "color", "header": "Color", "question": "Which color?", "isOther": true, "isSecret": false,
				"options": []any{map[string]any{"label": "Red", "description": "Warm"}, map[string]any{"label": "Blue", "description": "Cool"}},
			},
			map[string]any{"id": "name", "header": "Name", "question": "What should it be called?", "isOther": false, "isSecret": false, "options": nil},
		},
	}
}

func TestAQuestionFromCodexWaitsForTheAnswersGivenInLegato(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")
	events := h.subscribe("")

	h.codex.ask(9, "item/tool/requestUserInput", colorQuestion())

	request := nextEvent(t, events)
	requestID, _ := request["requestId"].(string)
	if request["type"] != "question_request" || requestID == "" {
		t.Fatalf("request = %v", request)
	}
	snap := h.snapshot()
	if snap.Status != "waiting_for_input" || len(snap.PendingQuestions) != 1 || len(snap.PendingQuestions[0].Questions) != 2 {
		t.Fatalf("status %q pending %+v", snap.Status, snap.PendingQuestions)
	}
	color, name := snap.PendingQuestions[0].Questions[0], snap.PendingQuestions[0].Questions[1]
	if color.ID != "color" || color.Header != "Color" || color.Question != "Which color?" || !color.AllowOther || color.MultiSelect || len(color.Options) != 2 || color.Options[1].Description != "Cool" {
		t.Fatalf("first question = %+v", color)
	}
	// A question with nothing to pick from is answered in the person's words.
	if name.ID != "name" || !name.AllowOther || name.Options == nil || len(name.Options) != 0 {
		t.Fatalf("second question = %+v", name)
	}

	for label, answers := range map[string]map[string]any{
		"a question left unanswered":        {"color": []string{"Blue"}},
		"two answers to a single choice":    {"color": []string{"Red", "Blue"}, "name": []string{"Widget"}},
		"an answer to a question not asked": {"color": []string{"Blue"}, "name": []string{"Widget"}, "size": []string{"L"}},
	} {
		if code, _ := h.post("/v1/sessions/task-1/questions/"+requestID, map[string]any{"answers": answers}); code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", label, code)
		}
	}
	// A question is answered, never allowed or denied.
	if code, _ := h.post("/v1/sessions/task-1/permissions/"+requestID, map[string]any{"decision": "allow"}); code != http.StatusNotFound {
		t.Fatalf("allow on a question: status = %d, want 404", code)
	}

	if code, body := h.post("/v1/sessions/task-1/questions/"+requestID, map[string]any{
		"answers": map[string]any{"color": []string{"Blue"}, "name": []string{" Widget "}},
	}); code != http.StatusNoContent {
		t.Fatalf("answer status = %d %s", code, body)
	}

	var answered struct {
		Answers map[string]struct {
			Answers []string `json:"answers"`
		} `json:"answers"`
	}
	reply := h.codex.answer()
	if err := json.Unmarshal(reply.Result, &answered); err != nil || string(reply.ID) != "9" {
		t.Fatalf("answer to the app server = %s %s", reply.ID, reply.Result)
	}
	if !reflect.DeepEqual(answered.Answers["color"].Answers, []string{"Blue"}) || !reflect.DeepEqual(answered.Answers["name"].Answers, []string{"Widget"}) {
		t.Fatalf("answers = %+v", answered.Answers)
	}
	if resolved := nextEvent(t, events); resolved["type"] != "question_resolved" || resolved["requestId"] != requestID {
		t.Fatalf("resolved = %v", resolved)
	}
	if snap := h.snapshot(); len(snap.PendingQuestions) != 0 || snap.Status != "running" {
		t.Fatalf("after the answer: status %q pending %+v", snap.Status, snap.PendingQuestions)
	}
}

func TestADismissedQuestionGoesBackUnanswered(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")
	events := h.subscribe("")
	h.codex.ask(9, "item/tool/requestUserInput", colorQuestion())
	requestID, _ := nextEvent(t, events)["requestId"].(string)

	if code, body := h.post("/v1/sessions/task-1/questions/"+requestID, map[string]any{"dismissed": true}); code != http.StatusNoContent {
		t.Fatalf("dismiss status = %d %s", code, body)
	}

	if reply := h.codex.answer(); strings.TrimSpace(string(reply.Result)) != `{"answers":{}}` {
		t.Fatalf("answer to the app server = %s", reply.Result)
	}
	if resolved := nextEvent(t, events); resolved["type"] != "question_resolved" || resolved["dismissed"] != true {
		t.Fatalf("resolved = %v", resolved)
	}
}

func TestAQuestionAnsweredAtTheTerminalLeavesThePanel(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")
	events := h.subscribe("")
	h.codex.ask(9, "item/tool/requestUserInput", colorQuestion())
	requestID, _ := nextEvent(t, events)["requestId"].(string)

	h.codex.notify("serverRequest/resolved", map[string]any{"threadId": threadID, "requestId": 9})

	if closed := nextEvent(t, events); closed["type"] != "question_timeout" || closed["requestId"] != requestID {
		t.Fatalf("event = %v", closed)
	}
}

func TestRequestsTheBridgeCannotPutToThePersonAreLeftToTheTerminal(t *testing.T) {
	h := newHarness(t)
	h.attach()
	h.begin("turn-1")
	events := h.subscribe("")

	// An MCP server's form, and a request about another thread.
	h.codex.ask(4, "mcpServer/elicitation/request", map[string]any{"threadId": threadID, "serverName": "jira", "message": "Sign in"})
	h.codex.ask(5, "item/commandExecution/requestApproval", map[string]any{"threadId": "thr-sub", "itemId": "exec-9", "command": "ls"})
	h.item("completed", agentMessage("item-a1", "carrying on"))

	if next := nextEvent(t, events); next["type"] != "assistant_text_done" {
		t.Fatalf("event = %v, want nothing raised for either request", next)
	}
	select {
	case answered := <-h.codex.answers:
		t.Fatalf("the bridge answered a request it does not handle: %s", answered.Result)
	case <-time.After(30 * time.Millisecond):
	}
}
