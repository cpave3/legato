package claudebridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// question is one thing the agent asks the person, in the native protocol's
// own shape: the panel draws it without knowing which agent or tool asked.
type question struct {
	// ID names the question in the answers sent back.
	ID          string           `json:"id"`
	Header      string           `json:"header,omitempty"`
	Question    string           `json:"question"`
	Options     []questionOption `json:"options"`
	MultiSelect bool             `json:"multiSelect"`
	// AllowOther lets the person type an answer of their own.
	AllowOther bool `json:"allowOther"`
}

type questionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Preview     string `json:"preview,omitempty"`
}

const askUserQuestionTool = "AskUserQuestion"

// questionDismissedMessage is what the model reads when the person closes a
// question in Legato without answering it.
const questionDismissedMessage = "The user dismissed these questions from Legato, where they are following this session, without answering them. " +
	"Carry on without the answers, or ask in plain text if you cannot."

var (
	errNoSuchRequest = errors.New("no such pending request")
	errInvalidAnswer = errors.New("invalid answer")
)

// questionsOf reads the questions of an AskUserQuestion call, or nil for any
// other tool. Claude Code's own dialog always offers a typed answer beside
// the options, so every question allows one.
func questionsOf(tool string, input map[string]any) []question {
	if tool != askUserQuestionTool {
		return nil
	}
	encoded, err := json.Marshal(input["questions"])
	if err != nil {
		return nil
	}
	var asked []question
	if json.Unmarshal(encoded, &asked) != nil {
		return nil
	}
	var kept []question
	for _, one := range asked {
		if strings.TrimSpace(one.Question) == "" {
			continue
		}
		one.ID = "q" + strconv.Itoa(len(kept)+1)
		one.AllowOther = true
		if one.Options == nil {
			one.Options = []questionOption{}
		}
		kept = append(kept, one)
	}
	return kept
}

// answersFor checks the answers given in Legato against the questions asked
// and returns them as Claude Code reads them: under each question's own
// words, several choices joined.
func answersFor(questions []question, given map[string][]string) (map[string]string, error) {
	known := make(map[string]bool, len(questions))
	out := make(map[string]string, len(questions))
	for _, one := range questions {
		known[one.ID] = true
		var chosen []string
		for _, answer := range given[one.ID] {
			if answer = strings.TrimSpace(answer); answer != "" {
				chosen = append(chosen, answer)
			}
		}
		switch {
		case len(chosen) == 0:
			return nil, fmt.Errorf("%w: question %s has no answer", errInvalidAnswer, one.ID)
		case len(chosen) > 1 && !one.MultiSelect:
			return nil, fmt.Errorf("%w: question %s takes one answer", errInvalidAnswer, one.ID)
		}
		out[one.Question] = strings.Join(chosen, ", ")
	}
	for id := range given {
		if !known[id] {
			return nil, fmt.Errorf("%w: %s is not one of the questions asked", errInvalidAnswer, id)
		}
	}
	return out, nil
}

// answerQuestion settles an open question with the answers given in Legato.
// It returns them in the form the mod hands to Claude Code.
func (s *session) answerQuestion(requestID string, given map[string][]string) ([]nativeEvent, map[string]string, error) {
	index := s.pendingIndex(requestID)
	if index < 0 || s.pending[index].questions == nil {
		return nil, nil, errNoSuchRequest
	}
	answers, err := answersFor(s.pending[index].questions, given)
	if err != nil {
		return nil, nil, err
	}
	s.pending = append(s.pending[:index], s.pending[index+1:]...)
	return []nativeEvent{{"type": "question_resolved", "requestId": requestID, "answers": given}}, answers, nil
}

// dismissQuestion closes an open question the person chose not to answer.
func (s *session) dismissQuestion(requestID string) ([]nativeEvent, error) {
	index := s.pendingIndex(requestID)
	if index < 0 || s.pending[index].questions == nil {
		return nil, errNoSuchRequest
	}
	s.pending = append(s.pending[:index], s.pending[index+1:]...)
	return []nativeEvent{{"type": "question_resolved", "requestId": requestID, "dismissed": true}}, nil
}
