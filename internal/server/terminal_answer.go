package server

import (
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode"

	"github.com/cpave3/legato/internal/engine/prompt"
)

const (
	// MsgAnswerTerminalPrompt answers the prompt an agent's terminal shows
	// with one of the answers Legato read off it (client → server).
	MsgAnswerTerminalPrompt = "answer_terminal_prompt"
	// MsgTerminalPromptAnswered says the keys went in, with what the screen
	// shows after them (server → client).
	MsgTerminalPromptAnswered = "terminal_prompt_answered"
)

// terminalAnswerSettle is how long the terminal is given to redraw after an
// answer before its screen is read again.
var terminalAnswerSettle = 400 * time.Millisecond

// answerablePrompts are the prompt types whose actions answer a question.
var answerablePrompts = map[prompt.PromptType]bool{
	prompt.Menu:         true,
	prompt.ToolApproval: true,
	prompt.PlanApproval: true,
	prompt.Question:     true,
}

// handleAnswerTerminalPrompt answers a prompt in an agent's terminal, for a
// prompt that never reached Legato as a request of its own. Unlike
// send_keys it serves native agents too, so it presses only keys it has
// just read off the screen as an answer: the screen is read again, it must
// still show the prompt the answer was chosen from (Prompt), and the keys
// must be one of its actions. A row that takes text (Content) gets its
// text typed once the cursor is on it. The tmux delivery guard still applies.
func (s *Server) handleAnswerTerminalPrompt(client *wsClient, msg WSMessage) {
	refuse := func(code, message string, state *prompt.PromptState) {
		client.send(WSMessage{Type: MsgError, AgentID: msg.AgentID, RequestID: msg.RequestID, Code: code, Error: message, Prompt: state})
	}
	if s.tmux == nil {
		refuse("unavailable", "tmux not available", nil)
		return
	}
	if code, message := checkTerminalAnswer(msg); code != "" {
		refuse(code, message, nil)
		return
	}
	text := msg.Content

	sessionName := "legato-" + msg.AgentID
	output, err := s.tmux.Capture(sessionName)
	if err != nil {
		refuse("capture_failed", "could not read the terminal: "+err.Error(), nil)
		return
	}
	state := s.streams.detectPromptState(msg.AgentID, output).State
	chosen, code, message := chooseTerminalAnswer(state, msg)
	if chosen == nil {
		refuse(code, message, &state)
		return
	}

	s.streams.clearDetectorWaiting(msg.AgentID)
	if err := s.typeTerminalAnswer(sessionName, *chosen, text); err != nil {
		refuse("send_failed", "the keys did not reach the terminal: "+err.Error(), nil)
		return
	}

	time.Sleep(terminalAnswerSettle)
	after := prompt.PromptState{Type: prompt.FreeText}
	if output, err := s.tmux.Capture(sessionName); err == nil {
		after = s.streams.detectPromptState(msg.AgentID, output).State
	}
	client.send(WSMessage{Type: MsgTerminalPromptAnswered, AgentID: msg.AgentID, RequestID: msg.RequestID, Prompt: &after})
}

// checkTerminalAnswer refuses an answer that is malformed before the screen
// is read: it names the agent, the request, the keys and the prompt, and its
// text is one line.
func checkTerminalAnswer(msg WSMessage) (code, message string) {
	if msg.AgentID == "" || msg.RequestID == "" || msg.Keys == "" || msg.Prompt == nil {
		return "invalid", "agent_id, request_id, keys and the prompt answered are required"
	}
	if strings.IndexFunc(msg.Content, unicode.IsControl) >= 0 {
		return "invalid", "the answer must be one line of text"
	}
	return "", ""
}

// chooseTerminalAnswer finds the action the answer chose on the screen read
// now. Keys are relative to the cursor, so they mean the answer chosen only
// on the screen it was chosen from: the prompt must be unchanged. Text goes
// only to a row that takes it, and such a row needs some.
func chooseTerminalAnswer(state prompt.PromptState, msg WSMessage) (chosen *prompt.Action, code, message string) {
	if answerablePrompts[state.Type] && reflect.DeepEqual(state, *msg.Prompt) {
		for index := range state.Actions {
			if state.Actions[index].Keys == msg.Keys {
				chosen = &state.Actions[index]
				break
			}
		}
	}
	switch {
	case chosen == nil:
		return nil, "prompt_changed", "the terminal changed before the answer was sent; check it and choose again"
	case chosen.TakesText && strings.TrimSpace(msg.Content) == "":
		return nil, "text_required", "type an answer for " + chosen.Label
	case !chosen.TakesText && msg.Content != "":
		return nil, "invalid", chosen.Label + " takes no text"
	}
	return chosen, "", ""
}

// promptAnswerer is the tmux manager's guarded way to answer a prompt on
// screen. Plain keys are refused for a native agent; this is not, while the
// tool itself is in front.
type promptAnswerer interface {
	AnswerPrompt(name string, keys []string, text string, submit bool) error
}

// typeTerminalAnswer presses an action's keys. For a row that takes text,
// the cursor moves to it, the text is typed, and Enter answers with it only
// where Enter does not untick it again.
func (s *Server) typeTerminalAnswer(sessionName string, action prompt.Action, text string) error {
	answerer, ok := s.tmux.(promptAnswerer)
	if !ok {
		return fmt.Errorf("this terminal cannot be answered from Legato")
	}
	keys := strings.Fields(action.Keys)
	if !action.TakesText {
		return answerer.AnswerPrompt(sessionName, keys, "", false)
	}
	if len(keys) > 0 && keys[len(keys)-1] == "Enter" {
		keys = keys[:len(keys)-1]
	}
	return answerer.AnswerPrompt(sessionName, keys, text, action.TextSubmits)
}
