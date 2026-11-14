package chimera

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// NormalizeQuestionRequest supplies the bridge protocol's question identities
// for Chimera's id-less questions. Both snapshots and SSE use the same order.
// Leave bridge questions and unknown fields unchanged.
func NormalizeQuestionRequest(raw json.RawMessage) json.RawMessage {
	var request map[string]json.RawMessage
	if json.Unmarshal(raw, &request) != nil {
		return raw
	}
	var questions []map[string]json.RawMessage
	if json.Unmarshal(request["questions"], &questions) != nil {
		return raw
	}
	for i, question := range questions {
		if question == nil {
			return raw
		}
		if _, exists := question["id"]; !exists {
			question["id"], _ = json.Marshal(fmt.Sprintf("q%d", i+1))
			question["multiSelect"] = json.RawMessage("false")
			question["allowOther"] = json.RawMessage("true")
		}
	}
	request["questions"], _ = json.Marshal(questions)
	normalized, _ := json.Marshal(request)
	return normalized
}

func normalizePendingQuestions(raw json.RawMessage) json.RawMessage {
	var requests []json.RawMessage
	if json.Unmarshal(raw, &requests) != nil || requests == nil {
		return raw
	}
	for i := range requests {
		requests[i] = NormalizeQuestionRequest(requests[i])
	}
	normalized, _ := json.Marshal(requests)
	return normalized
}

// AnswerChimeraQuestion translates the panel's keyed answers to Chimera's
// question-text resolution. Claude Code and Codex bridges use AnswerQuestion.
func (c *Client) AnswerChimeraQuestion(ctx context.Context, sessionID, requestID string, answer QuestionAnswer) error {
	body := map[string]any{"status": "cancelled"}
	if !answer.Dismissed {
		snapshot, err := c.GetSession(ctx, sessionID)
		if err != nil {
			return err
		}
		var requests []struct {
			RequestID string `json:"requestId"`
			Questions []struct {
				ID       string `json:"id"`
				Question string `json:"question"`
				Options  []struct {
					Label string `json:"label"`
				} `json:"options"`
			} `json:"questions"`
		}
		if err := json.Unmarshal(snapshot.PendingQuestions, &requests); err != nil {
			return fmt.Errorf("read pending questions: %w", err)
		}
		type resolvedAnswer struct {
			Question string `json:"question"`
			Answer   string `json:"answer"`
			Custom   bool   `json:"custom"`
		}
		var resolved []resolvedAnswer
		for _, request := range requests {
			if request.RequestID != requestID {
				continue
			}
			for _, question := range request.Questions {
				chosen := answer.Answers[question.ID]
				if len(chosen) != 1 || chosen[0] == "" {
					return fmt.Errorf("question %s requires one answer", question.ID)
				}
				custom := true
				for _, option := range question.Options {
					if chosen[0] == option.Label {
						custom = false
						break
					}
				}
				resolved = append(resolved, resolvedAnswer{Question: question.Question, Answer: chosen[0], Custom: custom})
			}
			if len(answer.Answers) != len(request.Questions) {
				return fmt.Errorf("answers do not match the request's questions")
			}
		}
		if len(resolved) == 0 {
			return fmt.Errorf("no pending question request %q", requestID)
		}
		body = map[string]any{"status": "answered", "answers": resolved}
	}
	encoded, _ := json.Marshal(body)
	path := "/v1/sessions/" + url.PathEscape(sessionID) + "/questions/" + url.PathEscape(requestID)
	resp, err := c.do(ctx, http.MethodPost, path, encoded)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("answerQuestion: %s: %s", resp.Status, detail)
	}
	return nil
}
