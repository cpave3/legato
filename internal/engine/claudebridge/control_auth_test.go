package claudebridge

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"
)

func TestAgentCanRequestPermissionButCannotDecideIt(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil), map[string]any{"type": "turn_start", "turnId": "turn-1"})
	events := h.subscribe("")
	answer := h.askPermission("auth-request", "Bash", map[string]any{"command": "printf approved"})
	if event := nextEvent(t, events); event["type"] != "permission_request" {
		t.Fatalf("event = %v", event)
	}
	public, err := url.Parse(h.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	public.User = nil
	for _, key := range []string{"", "wrong", "previous-generation"} {
		req, err := http.NewRequest(http.MethodPost, public.String()+"/v1/sessions/task-1/permissions/auth-request", bytes.NewBufferString(`{"decision":"allow"}`))
		if err != nil {
			t.Fatal(err)
		}
		if key != "" {
			req.SetBasicAuth("legato", key)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("unauthenticated decision = %d; want 401", resp.StatusCode)
		}
		if pending := h.snapshot().PendingPermissions; len(pending) != 1 {
			t.Fatalf("pending = %v", pending)
		}
		select {
		case a := <-answer:
			t.Fatalf("agent answered its own request: %v", a)
		default:
		}
	}
	if code, body := h.post("/v1/sessions/task-1/permissions/auth-request", map[string]any{"decision": "allow"}); code != http.StatusNoContent {
		t.Fatalf("host decision = %d %s", code, body)
	}
	if got := nextAnswer(t, answer); got["decision"] != "allow" {
		t.Fatalf("mod answer = %v", got)
	}
}

func TestNativeControlRoutesFailClosedBeforeParsing(t *testing.T) {
	h := newHarness(t)
	public, _ := url.Parse(h.server.URL)
	public.User = nil
	for _, route := range []string{"messages", "interrupt", "compact", "permissions/r", "questions/r", "model", "mode", "paths"} {
		t.Run(route, func(t *testing.T) {
			resp, err := http.Post(public.String()+"/v1/sessions/task-1/"+route, "application/json", bytes.NewBufferString("not JSON"))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status=%d", resp.StatusCode)
			}
		})
	}
	for _, route := range []string{"/mod/events", "/mod/permissions"} {
		resp, err := http.Post(public.String()+route, "application/json", bytes.NewBufferString("not JSON"))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("keyless opener status=%d", resp.StatusCode)
		}
	}
	resp, err := http.Get(public.String() + "/v1/sessions/task-1/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("keyless viewer=%d", resp.StatusCode)
	}
	var body any
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1024)).Decode(&body)
}

func TestAgentCannotAnswerAQuestionOrApproveAPlan(t *testing.T) {
	for _, tool := range []string{"AskUserQuestion", "ExitPlanMode"} {
		t.Run(tool, func(t *testing.T) {
			h := newHarness(t)
			h.mod(hello(nil), map[string]any{"type": "turn_start", "turnId": "turn"})
			events := h.subscribe("")
			input := colorAndSizes()
			route := "questions"
			decision := map[string]any{"dismissed": true}
			if tool == "ExitPlanMode" {
				input = map[string]any{"plan": "# Harmless test plan"}
				route = "permissions"
				decision = map[string]any{"decision": "allow", "option": "accept_edits"}
			}
			answer := h.askPermission("protected", tool, input)
			nextEvent(t, events)
			body, _ := json.Marshal(decision)
			resp, err := http.Post(h.publicURL()+"/v1/sessions/task-1/"+route+"/protected", "application/json", bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("self decision=%d", resp.StatusCode)
			}
			if tool == "AskUserQuestion" {
				if len(h.questionSnapshot().PendingQuestions) != 1 {
					t.Fatal("question disappeared")
				}
			} else if len(h.snapshot().PendingPermissions) != 1 {
				t.Fatal("plan disappeared")
			}
			select {
			case decided := <-answer:
				t.Fatalf("unauthorized answer=%v", decided)
			default:
			}
			if code, body := h.post("/v1/sessions/task-1/"+route+"/protected", decision); code != http.StatusNoContent {
				t.Fatalf("host decision=%d %s", code, body)
			}
			decided := nextAnswer(t, answer)
			if tool == "AskUserQuestion" && decided["decision"] != "deny" {
				t.Fatalf("dismissal=%v", decided)
			}
			if tool == "ExitPlanMode" && (decided["returnInput"] != true || decided["decision"] != "allow") {
				t.Fatalf("plan answer=%v", decided)
			}
		})
	}
}
