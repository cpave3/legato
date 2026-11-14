package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// The fixture faithfully copies Chimera's server test sessionWithQuestion:
// cpave3/chimera e6458b4c, packages/server/test/app.test.ts:860-903.
func TestChimeraQuestionChoiceReachesAgent(t *testing.T) {
	testChimeraQuestionAnswer(t, "vitest", false, false)
}

func TestChimeraQuestionFreeTextReachesAgent(t *testing.T) {
	testChimeraQuestionAnswer(t, "use bun test", true, false)
}

func TestChimeraQuestionStoredBeforeUpgradeCanBeAnswered(t *testing.T) {
	testChimeraQuestionAnswer(t, "vitest", false, true)
}

func testChimeraQuestionAnswer(t *testing.T, answer string, custom, storedBeforeUpgrade bool) {
	t.Helper()
	ctx := context.Background()
	fixture, err := os.ReadFile("../../contracts/chimera-question.json")
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan string, 1)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/v1/sessions/session-1":
			fmt.Fprintf(w, `{"id":"session-1","pendingQuestions":[%s]}`, fixture)
		case r.URL.Path == "/v1/sessions/session-1/events":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case r.Method == "POST" && r.URL.Path == "/v1/sessions/session-1/questions/q-1":
			// Chimera's questionResolutionSchema requires status and an answer array.
			var body struct {
				Status  string `json:"status"`
				Answers []struct {
					Question string `json:"question"`
					Answer   string `json:"answer"`
					Custom   bool   `json:"custom"`
				} `json:"answers"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Status != "answered" || len(body.Answers) != 1 {
				http.Error(w, "bad question resolution", 400)
				return
			}
			got := body.Answers[0]
			if got.Question != "Which test runner?" || got.Answer != answer || got.Custom != custom {
				http.Error(w, "wrong question answer", 400)
				return
			}
			received <- got.Answer
			w.WriteHeader(204)
		default:
			http.NotFound(w, r)
		}
	}))
	defer endpoint.Close()
	approvals, db, _ := newApprovalFixture(t)
	createTask(t, db, "task-1")
	sessionID := "session-1"
	if err := db.InsertAgentSession(ctx, store.AgentSession{TaskID: "task-1", TmuxSession: "legato-task-1", AgentKind: "chimera", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateChimeraDiscovery(ctx, "task-1", sessionID, endpoint.URL); err != nil {
		t.Fatal(err)
	}
	adapter := NewChimeraInteractionAdapter(db, nil)
	if storedBeforeUpgrade {
		var request questionRequest
		if err := json.Unmarshal(fixture, &request); err != nil {
			t.Fatal(err)
		}
		_, _, err := approvals.Submit(ctx, ApprovalInput{
			Source: ApprovalSourceAgentQuestion, SourceKey: "/session-1/q-1", TaskID: "task-1", Title: "Which test runner?",
			Details: agentRequestDetails{RequestID: request.RequestID, AgentKind: "chimera", Questions: request.Questions},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	watcher := NewApprovalWatcher(approvals, adapter, ApprovalWatcherOptions{Sessions: db.ListAgentSessions})
	defer watcher.Stop()
	watcher.Reconcile(ctx)
	pending := (watcherFixture{approvals: approvals}).pendingSoon(t, 1)[0]
	var details struct {
		Questions []struct {
			ID string `json:"id"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(pending.Details, &details); err != nil {
		t.Fatal(err)
	}
	if len(details.Questions) != 1 || details.Questions[0].ID != "q1" {
		t.Fatalf("question identity missing: %s", pending.Details)
	}
	decided, err := approvals.Decide(ctx, pending.ID, ApprovalDecision{Decision: ApprovalAnswer, By: "web", Answers: map[string][]string{"q1": {answer}}})
	if err != nil {
		t.Fatal(err)
	}
	if decided.Status != store.ApprovalAnswered {
		t.Fatalf("status = %s", decided.Status)
	}
	select {
	case <-received:
	default:
		t.Fatal("answer never reached Chimera")
	}
}
