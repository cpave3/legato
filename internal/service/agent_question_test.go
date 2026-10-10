package service

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestQuestionAnswerReachesABridgedClaudeAgent(t *testing.T) {
	agent := newBridgedClaudeAgent(t, "TASK-42")
	// A native viewer is watching, so the bridge holds the mod's request.
	ctx, stopWatching := context.WithCancel(context.Background())
	defer stopWatching()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, agent.endpoint+"/v1/sessions/TASK-42/events", nil)
	watching, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer watching.Body.Close()
	events := bufio.NewScanner(watching.Body)

	answer := make(chan map[string]string, 1)
	go func() {
		resp, err := http.Post(agent.endpoint+"/mod/permissions", "application/json", strings.NewReader(
			`{"requestId":"permission-1","tool":"AskUserQuestion","input":{"questions":[`+
				`{"question":"Which color?","header":"Color","multiSelect":false,"options":[{"label":"Red"},{"label":"Blue"}]}]}}`))
		if err != nil {
			answer <- map[string]string{"error": err.Error()}
			return
		}
		defer resp.Body.Close()
		var decided struct {
			Decision string            `json:"decision"`
			Answers  map[string]string `json:"answers"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&decided)
		if decided.Answers == nil {
			decided.Answers = map[string]string{}
		}
		decided.Answers["decision"] = decided.Decision
		answer <- decided.Answers
	}()
	// Wait until the question is open before answering it.
	for events.Scan() {
		if strings.Contains(events.Text(), `"question_request"`) {
			break
		}
	}

	err = agent.svc.AnswerQuestion(context.Background(), "TASK-42", "permission-1", QuestionAnswer{
		Answers: map[string][]string{"q1": {"Blue"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-answer:
		if got["decision"] != "allow" || got["Which color?"] != "Blue" {
			t.Fatalf("answer to the mod = %v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the mod's request was never answered")
	}
}

func TestAnsweringAQuestionOfATerminalAgentIsRefused(t *testing.T) {
	adapter := &SendKeysAdapter{}

	err := adapter.AnswerQuestion(context.Background(), "TASK-42", "question-1", QuestionAnswer{Dismissed: true})

	if err == nil {
		t.Fatal("a terminal-backed agent has no structured questions to answer")
	}
}
