package claudebridge

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// colorAndSizes is an AskUserQuestion call as Claude Code makes it: one
// single-choice question and one that takes several answers.
func colorAndSizes() map[string]any {
	option := func(label, description string) map[string]any {
		return map[string]any{"label": label, "description": description}
	}
	return map[string]any{"questions": []any{
		map[string]any{
			"question": "Which color?", "header": "Color", "multiSelect": false,
			"options": []any{option("Red", "Warm"), option("Blue", "Cool")},
		},
		map[string]any{
			"question": "Which sizes?", "header": "Sizes", "multiSelect": true,
			"options": []any{option("Small", ""), option("Medium", ""), option("Large", "")},
		},
	}}
}

type questionSnapshot struct {
	Status             string `json:"status"`
	PendingPermissions []struct {
		RequestID string `json:"requestId"`
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
}

func (h *harness) questionSnapshot() questionSnapshot {
	h.t.Helper()
	var snap questionSnapshot
	if code := h.getJSON("/v1/sessions/task-1", &snap); code != http.StatusOK {
		h.t.Fatalf("GET snapshot = %d", code)
	}
	return snap
}

func TestQuestionWaitsForTheAnswersGivenInLegato(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil), map[string]any{"type": "turn_start", "turnId": "turn-1"})
	events := h.subscribe("")

	answer := h.askPermission("permission-1", "AskUserQuestion", colorAndSizes())

	request := nextEvent(t, events)
	if request["type"] != "question_request" || request["requestId"] != "permission-1" {
		t.Fatalf("request = %v", request)
	}
	snap := h.questionSnapshot()
	if snap.Status != "waiting_for_input" || len(snap.PendingPermissions) != 0 {
		t.Fatalf("status %q, pending permissions %+v", snap.Status, snap.PendingPermissions)
	}
	if len(snap.PendingQuestions) != 1 || snap.PendingQuestions[0].RequestID != "permission-1" || len(snap.PendingQuestions[0].Questions) != 2 {
		t.Fatalf("pending questions = %+v", snap.PendingQuestions)
	}
	color, sizes := snap.PendingQuestions[0].Questions[0], snap.PendingQuestions[0].Questions[1]
	if color.Header != "Color" || color.Question != "Which color?" || color.MultiSelect || !color.AllowOther {
		t.Fatalf("first question = %+v", color)
	}
	if len(color.Options) != 2 || color.Options[1].Label != "Blue" || color.Options[1].Description != "Cool" {
		t.Fatalf("first question's options = %+v", color.Options)
	}
	if !sizes.MultiSelect || len(sizes.Options) != 3 || color.ID == "" || color.ID == sizes.ID {
		t.Fatalf("questions = %+v", snap.PendingQuestions[0].Questions)
	}
	if asked, _ := request["questions"].([]any); len(asked) != 2 {
		t.Fatalf("the event carries %v, want both questions", request["questions"])
	}

	code, body := h.post("/v1/sessions/task-1/questions/permission-1", map[string]any{
		"answers": map[string]any{color.ID: []string{"Blue"}, sizes.ID: []string{"Small", "Medium"}},
	})
	if code != http.StatusNoContent {
		t.Fatalf("answer status = %d %s", code, body)
	}

	// Claude Code reads answers by the question's own words, several joined.
	decided := nextAnswer(t, answer)
	want := map[string]any{"Which color?": "Blue", "Which sizes?": "Small, Medium"}
	if decided["decision"] != "allow" || !reflect.DeepEqual(decided["answers"], want) {
		t.Fatalf("answer to the mod = %v", decided)
	}
	resolved := nextEvent(t, events)
	if resolved["type"] != "question_resolved" || resolved["requestId"] != "permission-1" {
		t.Fatalf("resolved = %v", resolved)
	}
	if answers, _ := resolved["answers"].(map[string]any); !reflect.DeepEqual(answers[sizes.ID], []any{"Small", "Medium"}) {
		t.Fatalf("resolved answers = %v", resolved["answers"])
	}
	snap = h.questionSnapshot()
	if len(snap.PendingQuestions) != 0 || snap.Status != "running" {
		t.Fatalf("after the answer: status %q, pending %+v", snap.Status, snap.PendingQuestions)
	}
}

func TestDismissedQuestionTellsTheModelNobodyAnswered(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil), map[string]any{"type": "turn_start", "turnId": "turn-1"})
	events := h.subscribe("")
	answer := h.askPermission("permission-1", "AskUserQuestion", colorAndSizes())
	nextEvent(t, events) // question_request

	if code, body := h.post("/v1/sessions/task-1/questions/permission-1", map[string]any{"dismissed": true}); code != http.StatusNoContent {
		t.Fatalf("dismiss status = %d %s", code, body)
	}

	decided := nextAnswer(t, answer)
	if message, _ := decided["message"].(string); decided["decision"] != "deny" || !strings.Contains(message, "dismissed") {
		t.Fatalf("answer to the mod = %v", decided)
	}
	if resolved := nextEvent(t, events); resolved["type"] != "question_resolved" || resolved["dismissed"] != true {
		t.Fatalf("resolved = %v", resolved)
	}
	if pending := h.questionSnapshot().PendingQuestions; len(pending) != 0 {
		t.Fatalf("pending = %+v", pending)
	}
}

func TestAnswersThatDoNotFitTheQuestionsLeaveThemOpen(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil))
	events := h.subscribe("")
	h.askPermission("permission-1", "AskUserQuestion", colorAndSizes())
	nextEvent(t, events) // question_request

	for name, answers := range map[string]map[string]any{
		"a question left unanswered":        {"q1": []string{"Blue"}},
		"a blank answer":                    {"q1": []string{"  "}, "q2": []string{"Small"}},
		"two answers to a single choice":    {"q1": []string{"Red", "Blue"}, "q2": []string{"Small"}},
		"an answer to a question not asked": {"q1": []string{"Blue"}, "q2": []string{"Small"}, "q9": []string{"x"}},
	} {
		if code, _ := h.post("/v1/sessions/task-1/questions/permission-1", map[string]any{"answers": answers}); code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", name, code)
		}
	}
	// A question is answered, never allowed or denied.
	if code, _ := h.post("/v1/sessions/task-1/permissions/permission-1", map[string]any{"decision": "allow"}); code != http.StatusNotFound {
		t.Fatalf("allow on a question: status = %d, want 404", code)
	}
	if code, _ := h.post("/v1/sessions/task-1/questions/nope", map[string]any{"dismissed": true}); code != http.StatusNotFound {
		t.Fatalf("unknown question: status = %d, want 404", code)
	}
	if pending := h.questionSnapshot().PendingQuestions; len(pending) != 1 {
		t.Fatalf("pending = %+v, want the question still open", pending)
	}
}

func TestQuestionAnsweredAtTheTerminalLeavesThePanel(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil), map[string]any{"type": "turn_start", "turnId": "turn-1"})
	h.mod(assistantRow("row-a", toolUse("toolu_1", "AskUserQuestion", colorAndSizes())))
	events := h.subscribe("")
	answer := h.askPermission("permission-1", "AskUserQuestion", colorAndSizes())
	nextEvent(t, events) // question_request

	// Claude Code's own dialog stays open; a result for the call means the
	// person answered there.
	h.mod(toolResultRow("row-r1", "toolu_1", "User has answered your questions", false))

	if event := nextEvent(t, events); event["type"] != "question_timeout" || event["requestId"] != "permission-1" {
		t.Fatalf("event = %v", event)
	}
	if decided := nextAnswer(t, answer); decided["decision"] != "ask" {
		t.Fatalf("answer = %v", decided)
	}
	if snap := h.questionSnapshot(); len(snap.PendingQuestions) != 0 || snap.Status != "running" {
		t.Fatalf("status %q pending %+v", snap.Status, snap.PendingQuestions)
	}
}

func TestQuestionAndPlanCallsSayWhatTheyAreAbout(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil), map[string]any{"type": "turn_start", "turnId": "turn-1"})

	h.mod(assistantRow("row-a",
		toolUse("toolu_1", "AskUserQuestion", colorAndSizes()),
		toolUse("toolu_2", "ExitPlanMode", map[string]any{"plan": "# Add a comment\n\n- Edit README.md", "planFilePath": "/p.md"}),
	))

	calls := h.snapshot().ToolCalls
	if len(calls) != 2 {
		t.Fatalf("tool calls = %+v", calls)
	}
	if got := calls[0].Display.Summary; got != "Which color? (+1 more)" {
		t.Fatalf("question summary = %q", got)
	}
	if got := calls[1].Display.Summary; got != "Add a comment" {
		t.Fatalf("plan summary = %q", got)
	}
}
