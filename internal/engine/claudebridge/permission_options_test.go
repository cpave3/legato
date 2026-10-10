package claudebridge

import (
	"bytes"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// ask is the mod holding a permission request open, with everything the
// engine told it about the request.
func (h *harness) ask(body map[string]any) <-chan map[string]any {
	h.t.Helper()
	answer := make(chan map[string]any, 1)
	go func() {
		defer close(answer)
		encoded, _ := json.Marshal(body)
		resp, err := http.Post(h.server.URL+"/mod/permissions", "application/json", bytes.NewReader(encoded))
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

func optionsOf(t *testing.T, event map[string]any) []map[string]any {
	t.Helper()
	listed, _ := event["options"].([]any)
	out := make([]map[string]any, 0, len(listed))
	for _, one := range listed {
		option, _ := one.(map[string]any)
		out = append(out, option)
	}
	return out
}

func TestPlanApprovalShowsThePlanAndHowEditsWillBeHandled(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil), map[string]any{"type": "turn_start", "turnId": "turn-1"})
	events := h.subscribe("")
	plan := "# Add a comment\n\n- Edit README.md\n- Check the diff"

	answer := h.ask(map[string]any{
		"requestId": "permission-1", "tool": "ExitPlanMode",
		"input": map[string]any{"plan": plan, "planFilePath": "/home/me/.claude/plans/add-a-comment.md"},
	})

	request := nextEvent(t, events)
	if request["type"] != "permission_request" || request["tool"] != "ExitPlanMode" || request["plan"] != plan {
		t.Fatalf("request = %v", request)
	}
	options := optionsOf(t, request)
	if len(options) != 2 || options[0]["id"] != "accept_edits" || options[1]["id"] != "manual" {
		t.Fatalf("options = %v", options)
	}
	var snap struct {
		PendingPermissions []struct {
			Plan    string `json:"plan"`
			Options []struct {
				ID    string `json:"id"`
				Label string `json:"label"`
			} `json:"options"`
		} `json:"pendingPermissions"`
	}
	h.getJSON("/v1/sessions/task-1", &snap)
	if len(snap.PendingPermissions) != 1 || snap.PendingPermissions[0].Plan != plan || len(snap.PendingPermissions[0].Options) != 2 || snap.PendingPermissions[0].Options[0].Label == "" {
		t.Fatalf("pending = %+v", snap.PendingPermissions)
	}

	if code, body := h.post("/v1/sessions/task-1/permissions/permission-1", map[string]any{"decision": "allow", "option": "accept_edits"}); code != http.StatusNoContent {
		t.Fatalf("resolve status = %d %s", code, body)
	}

	// Claude Code accepts a plan only with its input handed back, and takes
	// the mode edits will run in as a permission update.
	decided := nextAnswer(t, answer)
	wantMode := []any{map[string]any{"type": "setMode", "mode": "acceptEdits", "destination": "session"}}
	if decided["decision"] != "allow" || decided["returnInput"] != true || !reflect.DeepEqual(decided["updatedPermissions"], wantMode) {
		t.Fatalf("answer to the mod = %v", decided)
	}
	if resolved := nextEvent(t, events); resolved["type"] != "permission_resolved" || resolved["decision"] != "allow" || resolved["option"] != "accept_edits" {
		t.Fatalf("resolved = %v", resolved)
	}
}

func TestPlanApprovedWithoutAChoiceKeepsEditsManual(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
	events := h.subscribe("")
	answer := h.ask(map[string]any{"requestId": "permission-1", "tool": "ExitPlanMode", "input": map[string]any{"plan": "- one"}})
	nextEvent(t, events) // permission_request

	h.post("/v1/sessions/task-1/permissions/permission-1", map[string]any{"decision": "allow"})

	decided := nextAnswer(t, answer)
	wantMode := []any{map[string]any{"type": "setMode", "mode": "default", "destination": "session"}}
	if decided["returnInput"] != true || !reflect.DeepEqual(decided["updatedPermissions"], wantMode) {
		t.Fatalf("answer to the mod = %v", decided)
	}
}

func TestToolPermissionOffersClaudeCodesOwnDontAskAgain(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
	events := h.subscribe("")
	suggestions := []any{
		map[string]any{
			"type": "addRules", "behavior": "allow", "destination": "localSettings",
			"rules": []any{map[string]any{"toolName": "Bash", "ruleContent": "npm run:*"}},
		},
	}
	open := func(id string) <-chan map[string]any {
		return h.ask(map[string]any{
			"requestId": id, "tool": "Bash", "input": map[string]any{"command": "npm run build"}, "suggestions": suggestions,
		})
	}

	always := open("permission-1")
	options := optionsOf(t, nextEvent(t, events))
	if len(options) != 2 || options[0]["id"] != "once" || options[0]["label"] != "Allow once" || options[1]["id"] != "always" {
		t.Fatalf("options = %v", options)
	}
	if label := options[1]["label"]; label != "Allow, and don't ask again for Bash(npm run:*) in this project" {
		t.Fatalf("label = %q", label)
	}

	// An option the request never offered changes nothing.
	if code, _ := h.post("/v1/sessions/task-1/permissions/permission-1", map[string]any{"decision": "allow", "option": "forever"}); code != http.StatusBadRequest {
		t.Fatalf("unknown option: status = %d, want 400", code)
	}
	h.post("/v1/sessions/task-1/permissions/permission-1", map[string]any{"decision": "allow", "option": "always"})
	decided := nextAnswer(t, always)
	if decided["decision"] != "allow" || !reflect.DeepEqual(decided["updatedPermissions"], suggestions) || decided["returnInput"] != nil {
		t.Fatalf("answer to the mod = %v", decided)
	}
	nextEvent(t, events) // permission_resolved

	once := open("permission-2")
	nextEvent(t, events) // permission_request
	h.post("/v1/sessions/task-1/permissions/permission-2", map[string]any{"decision": "allow", "option": "once"})
	if decided := nextAnswer(t, once); decided["decision"] != "allow" || decided["updatedPermissions"] != nil {
		t.Fatalf("answer to the mod = %v", decided)
	}
}

func TestDontAskAgainSaysWhatItWillAllowAndForHowLong(t *testing.T) {
	rule := func(tool, content string) map[string]any {
		return map[string]any{"toolName": tool, "ruleContent": content}
	}
	for name, tc := range map[string]struct {
		suggestions []any
		want        string
	}{
		"a folder and edits, this session": {
			suggestions: []any{
				map[string]any{"type": "addDirectories", "directories": []any{"/tmp/out"}, "destination": "session"},
				map[string]any{"type": "setMode", "mode": "acceptEdits", "destination": "session"},
			},
			want: "Allow, and always allow access to /tmp/out and accept edits for this session",
		},
		"a whole tool, everywhere": {
			suggestions: []any{
				map[string]any{"type": "addRules", "behavior": "allow", "destination": "userSettings", "rules": []any{rule("WebFetch", "")}},
			},
			want: "Allow, and don't ask again for WebFetch in every project",
		},
		"a change this build cannot put into words": {
			suggestions: []any{map[string]any{"type": "somethingNew", "destination": "session"}},
			want:        "Allow, and don't ask again for this session",
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.mod(hello(nil))
			events := h.subscribe("")
			h.ask(map[string]any{"requestId": "permission-1", "tool": "Bash", "input": map[string]any{"command": "ls"}, "suggestions": tc.suggestions})

			options := optionsOf(t, nextEvent(t, events))
			if len(options) != 2 || options[1]["label"] != tc.want {
				t.Fatalf("options = %v, want the second labelled %q", options, tc.want)
			}
		})
	}
}

func TestRequestWithNothingToRememberTakesAPlainAllow(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
	events := h.subscribe("")
	h.ask(map[string]any{"requestId": "permission-1", "tool": "Bash", "input": map[string]any{"command": "ls"}})

	request := nextEvent(t, events)
	if _, listed := request["options"]; listed {
		t.Fatalf("request = %v, want no options", request)
	}
	if _, isPlan := request["plan"]; isPlan {
		t.Fatalf("request = %v, want no plan", request)
	}
}

func TestDenialReadsToTheModelAsThePersonsOwn(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
	events := h.subscribe("")

	withFeedback := h.ask(map[string]any{"requestId": "permission-1", "tool": "Bash", "input": map[string]any{"command": "git push --force"}})
	nextEvent(t, events)
	h.post("/v1/sessions/task-1/permissions/permission-1", map[string]any{"decision": "deny", "feedback": "never force push"})
	message, _ := nextAnswer(t, withFeedback)["message"].(string)
	if !strings.Contains(message, "the user said:\nnever force push") {
		t.Fatalf("message = %q", message)
	}
	nextEvent(t, events)

	bare := h.ask(map[string]any{"requestId": "permission-2", "tool": "Bash", "input": map[string]any{"command": "ls"}})
	nextEvent(t, events)
	h.post("/v1/sessions/task-1/permissions/permission-2", map[string]any{"decision": "deny"})
	message, _ = nextAnswer(t, bare)["message"].(string)
	if !strings.Contains(message, "rejected it from Legato") || strings.Contains(message, "the user said") {
		t.Fatalf("message = %q", message)
	}
}
