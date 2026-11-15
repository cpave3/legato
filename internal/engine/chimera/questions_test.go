package chimera

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
)

// This output fixture is also consumed by the web interaction tests, so the
// watcher, native panel and approval tray agree on the identity sent back.
func TestChimeraQuestionsHaveTheSamePanelContractInSnapshotAndStream(t *testing.T) {
	wire, err := os.ReadFile("../../../contracts/chimera-question.json")
	if err != nil {
		t.Fatal(err)
	}
	panel, err := os.ReadFile("../../../contracts/chimera-question-panel.json")
	if err != nil {
		t.Fatal(err)
	}
	var want any
	if err := json.Unmarshal(panel, &want); err != nil {
		t.Fatal(err)
	}
	check := func(raw json.RawMessage) {
		t.Helper()
		var got map[string]any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		delete(got, "type")
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("question = %s, want %s", raw, panel)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/sessions/s/events" {
			var event map[string]any
			_ = json.Unmarshal(wire, &event)
			event["type"] = "question_request"
			encoded, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", encoded)
		} else {
			fmt.Fprintf(w, `{"pendingQuestions":[%s]}`, wire)
		}
	}))
	defer server.Close()
	client := NewClient(server.URL)
	snap, err := client.GetSession(context.Background(), "s")
	if err != nil {
		t.Fatal(err)
	}
	var requests []json.RawMessage
	if err := json.Unmarshal(snap.PendingQuestions, &requests); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 {
		t.Fatalf("requests = %s", snap.PendingQuestions)
	}
	check(requests[0])
	count := 0
	if err := client.Subscribe(context.Background(), "s", "", func(event EventEnvelope) { count++; check(event.Data) }); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("events = %d", count)
	}
}

func TestChimeraQuestionDismissalReachesAgent(t *testing.T) {
	received := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/sessions/s/questions/r" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(body, map[string]any{"status": "cancelled"}) {
			t.Errorf("resolution = %v", body)
		}
		received = true
		w.WriteHeader(204)
	}))
	defer server.Close()
	if err := NewClient(server.URL).AnswerChimeraQuestion(context.Background(), "s", "r", QuestionAnswer{Dismissed: true}); err != nil {
		t.Fatal(err)
	}
	if !received {
		t.Fatal("dismissal never reached Chimera")
	}
}
