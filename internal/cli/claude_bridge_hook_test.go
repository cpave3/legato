package cli_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/claudebridge"
)

// fakeBridge stands in for a session's bridge: it records what the hook posts
// and answers /mod/permissions with the decisions it is given, in order.
type fakeBridge struct {
	mu      sync.Mutex
	answers []map[string]any
	status  int
	asked   []map[string]any
	events  []map[string]any
	paths   []string
	server  *httptest.Server
}

func newFakeBridge(t *testing.T, answers ...map[string]any) *fakeBridge {
	t.Helper()
	fake := &fakeBridge{answers: answers, status: http.StatusOK}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		fake.mu.Lock()
		defer fake.mu.Unlock()
		fake.paths = append(fake.paths, r.URL.Path)
		switch r.URL.Path {
		case "/secret/mod/permissions":
			fake.asked = append(fake.asked, body)
			if fake.status != http.StatusOK {
				w.WriteHeader(fake.status)
				return
			}
			answer := map[string]any{"decision": "ask"}
			if len(fake.answers) > 0 {
				answer, fake.answers = fake.answers[0], fake.answers[1:]
			}
			_ = json.NewEncoder(w).Encode(answer)
		case "/secret/mod/events":
			events, _ := body["events"].([]any)
			for _, event := range events {
				if object, ok := event.(map[string]any); ok {
					fake.events = append(fake.events, object)
				}
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeBridge) url() string { return f.server.URL + "/secret" }

func runBridgeHook(t *testing.T, url string, payload any) string {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return cli.ClaudeBridgeHook(context.Background(), strings.NewReader(string(data)), cli.ClaudeBridgeHookOptions{URL: url})
}

// decisionOf reads the decision a PermissionRequest hook printed.
func decisionOf(t *testing.T, output string) map[string]any {
	t.Helper()
	var printed struct {
		HookSpecificOutput struct {
			HookEventName string         `json:"hookEventName"`
			Decision      map[string]any `json:"decision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(output), &printed); err != nil {
		t.Fatalf("hook printed %q: %v", output, err)
	}
	if printed.HookSpecificOutput.HookEventName != "PermissionRequest" {
		t.Fatalf("hook printed %q, want a PermissionRequest answer", output)
	}
	return printed.HookSpecificOutput.Decision
}

func permissionPayload(tool string, input map[string]any) map[string]any {
	return map[string]any{
		"hook_event_name": "PermissionRequest",
		"session_id":      "s-1",
		"tool_name":       tool,
		"tool_input":      input,
	}
}

func colorQuestion() map[string]any {
	return map[string]any{"questions": []any{map[string]any{
		"question": "Which color?", "header": "Color", "multiSelect": false,
		"options": []any{map[string]any{"label": "Red"}, map[string]any{"label": "Blue"}},
	}}}
}

func TestBridgeHookAllowsWhatLegatoAllows(t *testing.T) {
	fake := newFakeBridge(t, map[string]any{"decision": "allow"})

	output := runBridgeHook(t, fake.url(), permissionPayload("Bash", map[string]any{"command": "git push"}))

	if decision := decisionOf(t, output); !reflect.DeepEqual(decision, map[string]any{"behavior": "allow"}) {
		t.Fatalf("decision = %v", decision)
	}
	if len(fake.asked) != 1 || fake.asked[0]["tool"] != "Bash" || fake.asked[0]["requestId"] == "" {
		t.Fatalf("asked = %v", fake.asked)
	}
	if input, _ := fake.asked[0]["input"].(map[string]any); input["command"] != "git push" {
		t.Fatalf("input asked about = %v", fake.asked[0]["input"])
	}
}

// Claude Code reads a question's answers from the tool's own input, where
// its dialog would have put them.
func TestBridgeHookAnswersAQuestionInsideTheToolsInput(t *testing.T) {
	fake := newFakeBridge(t, map[string]any{"decision": "allow", "answers": map[string]any{"Which color?": "Blue"}})

	output := runBridgeHook(t, fake.url(), permissionPayload("AskUserQuestion", colorQuestion()))

	decision := decisionOf(t, output)
	updated, _ := decision["updatedInput"].(map[string]any)
	if decision["behavior"] != "allow" || !reflect.DeepEqual(updated["answers"], map[string]any{"Which color?": "Blue"}) {
		t.Fatalf("decision = %v", decision)
	}
	if questions, _ := updated["questions"].([]any); len(questions) != 1 {
		t.Fatalf("the questions were not handed back: %v", updated)
	}
}

func TestBridgeHookAsksAgainUnderOneRequestWhileLegatoWaits(t *testing.T) {
	fake := newFakeBridge(t, map[string]any{"decision": "wait"}, map[string]any{"decision": "wait"}, map[string]any{"decision": "allow"})

	output := runBridgeHook(t, fake.url(), permissionPayload("Bash", map[string]any{"command": "git push"}))

	if decision := decisionOf(t, output); decision["behavior"] != "allow" {
		t.Fatalf("decision = %v", decision)
	}
	if len(fake.asked) != 3 {
		t.Fatalf("asked %d times, want 3", len(fake.asked))
	}
	for _, asked := range fake.asked[1:] {
		if asked["requestId"] != fake.asked[0]["requestId"] {
			t.Fatalf("request IDs differ: %v", fake.asked)
		}
	}
}

func TestBridgeHookDenialReadsAsLegatoWordedIt(t *testing.T) {
	cases := map[string]struct {
		answer map[string]any
		want   string
	}{
		"message":  {map[string]any{"decision": "deny", "feedback": "later", "message": "The user dismissed these questions."}, "The user dismissed these questions."},
		"feedback": {map[string]any{"decision": "deny", "feedback": "never force push"}, "never force push"},
		"neither":  {map[string]any{"decision": "deny"}, "Denied in Legato."},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fake := newFakeBridge(t, tc.answer)

			decision := decisionOf(t, runBridgeHook(t, fake.url(), permissionPayload("Bash", map[string]any{"command": "git push --force"})))

			if !reflect.DeepEqual(decision, map[string]any{"behavior": "deny", "message": tc.want}) {
				t.Fatalf("decision = %v", decision)
			}
		})
	}
}

// Claude Code accepts a plan only with its input handed back.
func TestBridgeHookApprovesAPlanWithTheEditModeChosenInLegato(t *testing.T) {
	acceptEdits := []any{map[string]any{"type": "setMode", "mode": "acceptEdits", "destination": "session"}}
	fake := newFakeBridge(t, map[string]any{"decision": "allow", "returnInput": true, "updatedPermissions": acceptEdits})
	plan := map[string]any{"plan": "# Add a comment", "planFilePath": "/home/me/.claude/plans/p.md"}

	decision := decisionOf(t, runBridgeHook(t, fake.url(), permissionPayload("ExitPlanMode", plan)))

	want := map[string]any{"behavior": "allow", "updatedInput": plan, "updatedPermissions": acceptEdits}
	if !reflect.DeepEqual(decision, want) {
		t.Fatalf("decision = %v, want %v", decision, want)
	}
}

func TestBridgeHookPassesTheEnginesSuggestionsOn(t *testing.T) {
	suggestions := []any{map[string]any{"type": "addRules", "behavior": "allow", "destination": "localSettings",
		"rules": []any{map[string]any{"toolName": "Bash", "ruleContent": "npm run:*"}}}}
	fake := newFakeBridge(t, map[string]any{"decision": "allow", "updatedPermissions": suggestions})
	payload := permissionPayload("Bash", map[string]any{"command": "npm run build"})
	payload["permission_suggestions"] = suggestions

	decision := decisionOf(t, runBridgeHook(t, fake.url(), payload))

	if !reflect.DeepEqual(fake.asked[0]["suggestions"], suggestions) {
		t.Fatalf("suggestions asked with = %v", fake.asked[0]["suggestions"])
	}
	if !reflect.DeepEqual(decision, map[string]any{"behavior": "allow", "updatedPermissions": suggestions}) {
		t.Fatalf("decision = %v", decision)
	}
}

// A long argument reaches the panel cut short; the decision still hands back
// the tool's whole input.
func TestBridgeHookCutsLongArgumentsForThePanel(t *testing.T) {
	fake := newFakeBridge(t, map[string]any{"decision": "ask"})
	long := strings.Repeat("x", 30_000)

	runBridgeHook(t, fake.url(), permissionPayload("Write", map[string]any{"content": long}))

	input, _ := fake.asked[0]["input"].(map[string]any)
	content, _ := input["content"].(string)
	if len(content) >= len(long) || !strings.HasSuffix(content, "… (truncated)") {
		t.Fatalf("content reached the bridge at %d characters", len(content))
	}
}

// Whenever Legato has no decision, the hook prints nothing: the terminal's
// dialog alone decides, and a hook never fails the tool call.
func TestBridgeHookLeavesTheDialogAloneWithoutADecision(t *testing.T) {
	ask := newFakeBridge(t, map[string]any{"decision": "ask"})
	broken := newFakeBridge(t)
	broken.status = http.StatusInternalServerError
	gone := newFakeBridge(t)
	gone.server.Close()

	cases := map[string]string{
		"ask":         ask.url(),
		"error":       broken.url(),
		"unreachable": gone.url(),
		"no bridge":   "",
	}
	for name, url := range cases {
		t.Run(name, func(t *testing.T) {
			if output := runBridgeHook(t, url, permissionPayload("Bash", map[string]any{"command": "ls"})); output != "" {
				t.Fatalf("hook printed %q", output)
			}
		})
	}
	if output := cli.ClaudeBridgeHook(context.Background(), strings.NewReader("not json"), cli.ClaudeBridgeHookOptions{URL: ask.url()}); output != "" {
		t.Fatalf("hook printed %q for a payload it cannot read", output)
	}
}

func TestBridgeHookReportsAFailedRequestWithoutTheProviderEndpoint(t *testing.T) {
	fake := newFakeBridge(t)

	output := runBridgeHook(t, fake.url(), map[string]any{
		"hook_event_name":        "StopFailure",
		"error":                  "unknown",
		"last_assistant_message": "API Error: 400 provider-canary at https://provider.test/nonsecret-endpoint-canary",
	})

	if output != "" {
		t.Fatalf("hook printed %q", output)
	}
	want := []map[string]any{{"type": "request_error", "error": "API Error: 400 provider-canary at [provider endpoint]"}}
	if !reflect.DeepEqual(fake.events, want) {
		t.Fatalf("events = %v", fake.events)
	}
}

func TestBridgeHookReportsTheErrorWhenNothingWasSaid(t *testing.T) {
	fake := newFakeBridge(t)

	runBridgeHook(t, fake.url(), map[string]any{"hook_event_name": "StopFailure", "error": "rate_limit"})

	if len(fake.events) != 1 || fake.events[0]["error"] != "rate_limit" {
		t.Fatalf("events = %v", fake.events)
	}
}

func TestBridgeHookIgnoresOtherEvents(t *testing.T) {
	fake := newFakeBridge(t)

	if output := runBridgeHook(t, fake.url(), map[string]any{"hook_event_name": "Stop"}); output != "" {
		t.Fatalf("hook printed %q", output)
	}
	if len(fake.paths) != 0 {
		t.Fatalf("the bridge was called: %v", fake.paths)
	}
}

// The seam that broke in LEG-684: an AskUserQuestion reaches the panel
// through the hook, and the answer given there settles Claude Code's dialog.
func TestBridgeHookPutsAQuestionToTheRealBridgeAndReturnsItsAnswer(t *testing.T) {
	bridge := claudebridge.New(claudebridge.Options{TaskID: "task-1", Token: "secret", DecisionKey: "test-host-key", Commands: io.Discard})
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(func() {
		bridge.Close()
		server.Close()
	})
	base := server.URL + "/secret"
	postJSON(t, base+"/mod/events", map[string]any{"events": []any{
		map[string]any{"type": "hello", "sessionId": "s-1", "cwd": "/work/repo"},
		map[string]any{"type": "turn_start", "turnId": "turn-1"},
	}})
	control, _ := url.Parse(base)
	control.User = url.UserPassword("legato", "test-host-key")
	events := subscribeBridge(t, control.String()+"/v1/sessions/task-1/events")

	output := make(chan string, 1)
	go func() {
		output <- runBridgeHook(t, base, permissionPayload("AskUserQuestion", colorQuestion()))
	}()

	var request map[string]any
	for request == nil || request["type"] != "question_request" {
		select {
		case request = <-events:
		case <-time.After(5 * time.Second):
			t.Fatal("no question_request reached the panel")
		}
	}
	questions, _ := request["questions"].([]any)
	first, _ := questions[0].(map[string]any)
	requestID, _ := request["requestId"].(string)
	postJSON(t, control.String()+"/v1/sessions/task-1/questions/"+requestID, map[string]any{
		"answers": map[string]any{first["id"].(string): []string{"Blue"}},
	})

	select {
	case printed := <-output:
		decision := decisionOf(t, printed)
		updated, _ := decision["updatedInput"].(map[string]any)
		if decision["behavior"] != "allow" || !reflect.DeepEqual(updated["answers"], map[string]any{"Which color?": "Blue"}) {
			t.Fatalf("decision = %v", decision)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the hook did not return after the answer")
	}
}

func postJSON(t *testing.T, url string, body any) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(url, "application/json", strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST %s = %d %s", url, resp.StatusCode, payload)
	}
}

func subscribeBridge(t *testing.T, url string) <-chan map[string]any {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan map[string]any, 64)
	go func() {
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
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
